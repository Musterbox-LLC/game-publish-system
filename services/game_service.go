package services

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"game-publish-system/models"
	"game-publish-system/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GameService struct {
	DB *gorm.DB
}

func NewGameService(db *gorm.DB) *GameService {
	return &GameService{DB: db}
}

// MinimalGame struct for lightweight listing
type MinimalGame struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	MainLogoURL   string  `json:"main_logo_url"`
	Platform      string  `json:"platform"`
	AverageRating float64 `json:"average_rating"` // ✅ Add to minimal

	PlayLink      string `json:"play_link,omitempty"`
	PlayStoreURL  string `json:"play_store_url,omitempty"`
	AppStoreURL   string `json:"app_store_url,omitempty"`
	PCDownloadURL string `json:"pc_download_url,omitempty"`
}

// UploadGame creates a new **draft** game with core file, media, and platform links.
func (s *GameService) UploadGame(c *fiber.Ctx) error {
	gameFile, err := c.FormFile("game_file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "game_file is required"})
	}

	if gameFile.Size > 5*1024*1024*1024 { // 5GB
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file too large (max 5GB)"})
	}

	// ✅ Save game file LOCALLY only — NOT to R2 (large files should not go to R2 CDN bucket)
	gameExt := filepath.Ext(gameFile.Filename)
	if gameExt == "" {
		gameExt = ".bin"
	}
	localGameFilename := uuid.New().String() + gameExt
	localGamePath := utils.GetUploadPath("games/" + localGameFilename) // e.g., "uploads/games/abc123.zip"

	if err := utils.SaveFile(gameFile, localGamePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "failed to save game file locally"})
	}

	// 1. Create the base game record
	game := &models.Game{
		ID:               uuid.NewString(),
		Name:             c.FormValue("name"),
		ShortDescription: c.FormValue("short_description"),
		LongDescription:  c.FormValue("long_description"),
		GameType:         c.FormValue("game_type"),
		Platform:         c.FormValue("platform"),
		AgeRating:        c.FormValue("age_rating"),
		FileURL:          "/" + localGamePath,      // ✅ Local path (for admin/internal use, e.g., Web3GL extraction)
		PlayLink:         c.FormValue("play_link"), // ✅ User-facing playable/download link (external or later-generated)

		// Platform-specific links
		PlayStoreURL:  c.FormValue("play_store_url"),
		AppStoreURL:   c.FormValue("app_store_url"),
		PCDownloadURL: c.FormValue("pc_download_url"),

		// Web3GL auto-detection
		IsWeb3GL: strings.HasSuffix(strings.ToLower(gameFile.Filename), ".zip") &&
			strings.ToLower(c.FormValue("game_type")) == "webgl",

		Status: "draft",
	}

	// 2. ✅ Handle Main Logo upload → R2 (small, public asset)
	if logoFile, err := c.FormFile("main_logo"); err == nil && logoFile.Size > 0 {
		logoExt := filepath.Ext(logoFile.Filename)
		if logoExt == "" {
			logoExt = ".png"
		}
		logoKey := "logos/" + uuid.NewString() + logoExt
		logoURL, err := utils.UploadFileToR2(logoFile, logoKey)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).
				JSON(fiber.Map{"error": "failed to upload main logo to R2"})
		}
		game.MainLogoURL = logoURL // ✅ Public R2 URL stored
	}

	// 3. Begin transaction
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// Save main game
		if err := tx.Create(game).Error; err != nil {
			return err
		}

		// 4. ✅ Handle Screenshot uploads → R2 (small, public assets)
		var screenshots []models.GameScreenshot
		for i := 0; ; i++ {
			key := "screenshots[" + strconv.Itoa(i) + "]"
			file, err := c.FormFile(key)
			if err != nil {
				break
			}

			screenshotExt := filepath.Ext(file.Filename)
			if screenshotExt == "" {
				screenshotExt = ".jpg"
			}
			screenshotKey := "screenshots/" + uuid.NewString() + screenshotExt
			screenshotURL, err := utils.UploadFileToR2(file, screenshotKey)
			if err != nil {
				return fmt.Errorf("failed to upload screenshot %d to R2: %w", i, err)
			}

			screenshots = append(screenshots, models.GameScreenshot{
				ID:     uuid.NewString(),
				GameID: game.ID,
				URL:    screenshotURL, // ✅ Public R2 URL
				Order:  i,
			})
		}

		// 5. Handle Video Links JSON (no upload — just metadata)
		var videoLinks []models.GameVideo
		if rawVideoLinks := c.FormValue("video_links"); rawVideoLinks != "" {
			if err := json.Unmarshal([]byte(rawVideoLinks), &videoLinks); err != nil {
				return fmt.Errorf("invalid video_links JSON: %v", err)
			}
			for i := range videoLinks {
				videoLinks[i].ID = uuid.NewString()
				videoLinks[i].GameID = game.ID
				videoLinks[i].Order = i
			}
		}

		// 6. Save associated records
		if len(screenshots) > 0 {
			if err := tx.Create(&screenshots).Error; err != nil {
				return fmt.Errorf("failed to save screenshots: %v", err)
			}
		}
		if len(videoLinks) > 0 {
			if err := tx.Create(&videoLinks).Error; err != nil {
				return fmt.Errorf("failed to save video links: %v", err)
			}
		}

		// Reload with associations
		if err := tx.Preload("Screenshots").Preload("VideoLinks").First(game, "id = ?", game.ID).Error; err != nil {
			return fmt.Errorf("failed to reload game with media: %v", err)
		}

		return nil
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(game)
}

// GetAllGames returns all games with media
func (s *GameService) GetAllGames(c *fiber.Ctx) error {
	var games []models.Game
	// ✅ GORM automatically adds `WHERE deleted_at IS NULL`
	if err := s.DB.Preload("Screenshots").Preload("VideoLinks").Preload("Reviews").Find(&games).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to fetch games"})
	}
	return c.JSON(games)
}

// GetGameByID returns a single game with media and reviews
func (s *GameService) GetGameByID(c *fiber.Ctx) error {
	id := c.Params("id")

	var game models.Game
	if err := s.DB.Preload("Screenshots").Preload("VideoLinks").Preload("Reviews").First(&game, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "game not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}
	return c.JSON(game)
}

// GetMinimalGames returns lightweight game list (published only) with average ratings
// GetMinimalGames returns lightweight game list (published only) with average ratings and platform links
func (s *GameService) GetMinimalGames(c *fiber.Ctx) error {
	fmt.Println("GetMinimalGames called")

	var games []models.Game
	if err := s.DB.Select(`
		id, 
		name, 
		main_logo_url, 
		platform, 
		average_rating,
		play_link,
		play_store_url,
		app_store_url,
		pc_download_url
	`).
		Where("status = ?", "published").
		Find(&games).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to fetch games"})
	}

	var minimalGames []MinimalGame
	for _, game := range games {
		minimalGames = append(minimalGames, MinimalGame{
			ID:            game.ID,
			Name:          game.Name,
			MainLogoURL:   game.MainLogoURL,
			Platform:      game.Platform,
			AverageRating: game.AverageRating,

			// ✅ Populate platform links
			PlayLink:      game.PlayLink,
			PlayStoreURL:  game.PlayStoreURL,
			AppStoreURL:   game.AppStoreURL,
			PCDownloadURL: game.PCDownloadURL,
		})
	}

	fmt.Printf("Returning %d minimal games\n", len(minimalGames))
	return c.JSON(minimalGames)
}


// UpdateGame allows full editing
func (s *GameService) UpdateGame(c *fiber.Ctx) error {
	id := c.Params("id")

	var game models.Game
	if err := s.DB.Preload("Screenshots").Preload("VideoLinks").First(&game, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "game not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	// Update scalar fields
	game.Name = c.FormValue("name")
	game.ShortDescription = c.FormValue("short_description")
	game.LongDescription = c.FormValue("long_description")
	game.GameType = c.FormValue("game_type")
	game.Platform = c.FormValue("platform")
	game.AgeRating = c.FormValue("age_rating")
	game.PlayLink = c.FormValue("play_link")
	game.PlayStoreURL = c.FormValue("play_store_url")
	game.AppStoreURL = c.FormValue("app_store_url")
	game.PCDownloadURL = c.FormValue("pc_download_url")

	// 🖼️ Main Logo (optional replacement)
	if logoFile, err := c.FormFile("main_logo"); err == nil && logoFile.Size > 0 {
		logoExt := filepath.Ext(logoFile.Filename)
		if logoExt == "" {
			logoExt = ".png"
		}
		logoKey := "logos/" + uuid.NewString() + logoExt
		logoURL, err := utils.UploadFileToR2(logoFile, logoKey)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).
				JSON(fiber.Map{"error": "failed to upload updated logo to R2"})
		}

		// Optional: delete old logo from R2 if desired (add later)
		game.MainLogoURL = logoURL
	}

	// 🖼️ Screenshots (replace all)
	var newScreenshots []models.GameScreenshot
	for i := 0; ; i++ {
		key := "screenshots[" + strconv.Itoa(i) + "]"
		file, err := c.FormFile(key)
		if err != nil {
			break
		}

		screenshotExt := filepath.Ext(file.Filename)
		if screenshotExt == "" {
			screenshotExt = ".jpg"
		}
		screenshotKey := "screenshots/" + uuid.NewString() + screenshotExt
		screenshotURL, err := utils.UploadFileToR2(file, screenshotKey)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).
				JSON(fiber.Map{"error": fmt.Sprintf("failed to upload screenshot %s to R2", key)})
		}

		newScreenshots = append(newScreenshots, models.GameScreenshot{
			ID:     uuid.NewString(),
			GameID: game.ID,
			URL:    screenshotURL,
			Order:  i,
		})
	}

	// 📺 Video Links (JSON array)
	if raw := c.FormValue("video_links"); raw != "" {
		var videos []models.GameVideo
		if err := json.Unmarshal([]byte(raw), &videos); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid video_links JSON"})
		}
		for i := range videos {
			videos[i].ID = uuid.NewString()
			videos[i].GameID = game.ID
			videos[i].Order = i
		}
		game.VideoLinks = videos
	}

	// 🎛️ Publishing control
	status := c.FormValue("status")
	switch status {
	case "draft", "published":
		game.Status = status
		game.PublishAt = nil
	case "scheduled":
		if tsStr := c.FormValue("publish_at"); tsStr != "" {
			publishAt, err := time.Parse(time.RFC3339, tsStr)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "invalid publish_at — use RFC3339 (e.g., 2025-12-31T23:00:00Z)",
				})
			}
			game.PublishAt = &publishAt
			game.Status = "scheduled"
		} else {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "publish_at required for scheduled status"})
		}
	default:
		if status != "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid status (use: draft, scheduled, published)"})
		}
	}

	// 🧹 Transaction: delete old media, insert new
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// Replace screenshots
		if err := tx.Where("game_id = ?", game.ID).Delete(&models.GameScreenshot{}).Error; err != nil {
			return err
		}
		if len(newScreenshots) > 0 {
			if err := tx.Create(&newScreenshots).Error; err != nil {
				return err
			}
		}
		game.Screenshots = newScreenshots

		// Replace videos
		if err := tx.Where("game_id = ?", game.ID).Delete(&models.GameVideo{}).Error; err != nil {
			return err
		}
		if len(game.VideoLinks) > 0 {
			if err := tx.Create(&game.VideoLinks).Error; err != nil {
				return err
			}
		}

		// Save game
		return tx.Save(&game).Error
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "update transaction failed"})
	}

	return c.JSON(game)
}

// DeleteGame hard-deletes a game and its media
func (s *GameService) DeleteGame(c *fiber.Ctx) error {
	id := c.Params("id")

	var game models.Game
	if err := s.DB.First(&game, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "game not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	// 🔔 Optional: Block deletion if referenced by active tournaments (safer)
	var tournamentCount int64
	s.DB.Model(&models.Tournament{}).Where("game_id = ? AND deleted_at IS NULL", id).Count(&tournamentCount)
	if tournamentCount > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "cannot delete game: still referenced by active tournaments",
			"tournament_count": tournamentCount,
		})
	}

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// ✅ Soft-delete is automatic for Game via `DeletedAt`
		// But we still want to *hard-delete* dependent media (since they have no standalone value)
		if err := tx.Where("game_id = ?", id).Delete(&models.GameScreenshot{}).Error; err != nil {
			return err
		}
		if err := tx.Where("game_id = ?", id).Delete(&models.GameVideo{}).Error; err != nil {
			return err
		}
		if err := tx.Where("game_id = ?", id).Delete(&models.Review{}).Error; err != nil {
			return err
		}

		// ✅ This now does SOFT-DELETE (sets deleted_at)
		return tx.Delete(&game).Error
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete game"})
	}

	return c.JSON(fiber.Map{
		"message": "game soft-deleted successfully",
		"id":      id,
	})
}


// UndeleteGame restores a soft-deleted game
func (s *GameService) UndeleteGame(c *fiber.Ctx) error {
	id := c.Params("id")

	var game models.Game
	// 🔍 Must use Unscoped() to find deleted record
	if err := s.DB.Unscoped().First(&game, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "game not found (even soft-deleted)"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	if game.DeletedAt.Time.IsZero() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "game is not deleted"})
	}

	if err := s.DB.Model(&game).Update("deleted_at", nil).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to restore game"})
	}

	return c.JSON(fiber.Map{
		"message": "game restored",
		"id":      game.ID,
	})
}

// ===== Web3GL Processing =====
// Note: Web3GL extraction still uses local temp dirs (safe, since temp)
// Game ZIP is stored locally (FileURL), so we use that path directly.

// ProcessWebGL processes a .zip game file from local storage, extracts, hosts locally, and sets playable link
func (s *GameService) ProcessWebGL(c *fiber.Ctx) error {
	id := c.Params("id")

	var game models.Game
	if err := s.DB.First(&game, "id = ?", id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "game not found"})
	}

	if game.FileURL == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no game file uploaded"})
	}

	// ✅ Use local FileURL (e.g., "/uploads/games/abc123.zip")
	localZipPath := "." + game.FileURL // e.g., "./uploads/games/abc123.zip"

	// Verify file exists locally
	if _, err := os.Stat(localZipPath); os.IsNotExist(err) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "game file not found locally"})
	}

	if !strings.HasSuffix(strings.ToLower(localZipPath), ".zip") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file must be a .zip for Web3GL processing"})
	}

	// Create temp dir
	tmpDir := filepath.Join(os.TempDir(), "web3gl-"+uuid.NewString())
	if err := os.MkdirAll(tmpDir, os.ModePerm); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "temp dir creation failed"})
	}
	defer os.RemoveAll(tmpDir)

	// Copy local zip to temp for extraction
	tempZipPath := filepath.Join(tmpDir, "game.zip")
	src, err := os.Open(localZipPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to open game zip: " + err.Error()})
	}
	defer src.Close()

	dst, err := os.Create(tempZipPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create temp zip: " + err.Error()})
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to copy zip to temp: " + err.Error()})
	}

	// Unzip
	if err := s.unzip(tempZipPath, tmpDir); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "unzip failed: " + err.Error()})
	}

	// Find entry point
	entry, err := s.findEntryPoint(tmpDir)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Host locally (same as before)
	hostedBase := filepath.Join("public", "web3gl", game.ID)
	if err := os.MkdirAll(hostedBase, os.ModePerm); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "host directory creation failed"})
	}

	if err := s.copyDir(tmpDir, hostedBase); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "copy to host dir failed: " + err.Error()})
	}

	hostedURL := "/web3gl/" + game.ID + "/" + entry

	// Save to DB
	game.IsWeb3GL = true
	game.Web3GLHostedURL = hostedURL
	if game.PlayLink == "" {
		game.PlayLink = hostedURL
	}

	if err := s.DB.Save(&game).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database update failed"})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"hosted_url": hostedURL,
		"play_link":  game.PlayLink,
		"message":    "Web3GL game processed and hosted successfully. Ready for admin verification.",
	})
}

// unzip extracts src zip to dest dir with zip-slip protection
func (s *GameService) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// ✅ Security: prevent path traversal (Zip Slip)
		path := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, os.ModePerm); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// findEntryPoint searches for common WebGL entry files (case-insensitive)
func (s *GameService) findEntryPoint(root string) (string, error) {
	candidates := []string{"index.html", "index.htm", "main.html", "game.html", "play.html", "index.js"}
	var found string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		lowerName := strings.ToLower(info.Name())
		for _, cand := range candidates {
			if lowerName == cand {
				rel, _ := filepath.Rel(root, path)
				found = filepath.ToSlash(rel) // Normalize to forward slashes for URLs
				return filepath.SkipDir
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("no entry point found. Expected files: index.html, main.html, etc")
	}
	return found, nil
}

// copyDir recursively copies src → dst
func (s *GameService) copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source is not a directory: %s", src)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := s.copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := s.copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile copies a single file
func (s *GameService) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// ===== Review Methods =====

// CreateReview creates a new review for a game
func (s *GameService) CreateReview(c *fiber.Ctx) error {
	gameID := c.Params("id")

	var input struct {
		UserID        string `json:"user_id"`
		UserName      string `json:"user_name"`
		UserAvatarURL string `json:"user_avatar_url"`
		Rating        int    `json:"rating" validate:"required,min=1,max=5"`
		Comment       string `json:"comment"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Validate rating
	if input.Rating < 1 || input.Rating > 5 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Rating must be between 1 and 5"})
	}

	// Check if game exists
	var game models.Game
	if err := s.DB.First(&game, "id = ?", gameID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "game not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	// Create review
	review := &models.Review{
		ID:            uuid.NewString(),
		GameID:        gameID,
		UserID:        input.UserID,
		UserName:      input.UserName,
		UserAvatarURL: input.UserAvatarURL,
		Rating:        input.Rating,
		Comment:       input.Comment,
	}

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(review).Error; err != nil {
			return err
		}

		// Recalculate average rating
		var avgRating float64
		err := tx.Model(&models.Review{}).Where("game_id = ?", gameID).Select("AVG(rating)").Scan(&avgRating).Error
		if err != nil {
			return err
		}

		// Update game's average rating
		return tx.Model(&game).Update("average_rating", avgRating).Error
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create review"})
	}

	return c.Status(fiber.StatusCreated).JSON(review)
}

// GetReviewsByGame returns all reviews for a specific game
func (s *GameService) GetReviewsByGame(c *fiber.Ctx) error {
	gameID := c.Params("id")

	var reviews []models.Review
	if err := s.DB.Where("game_id = ?", gameID).Order("created_at DESC").Find(&reviews).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch reviews"})
	}

	return c.JSON(reviews)
}

// UpdateReview updates an existing review
func (s *GameService) UpdateReview(c *fiber.Ctx) error {
	reviewID := c.Params("review_id")

	var input struct {
		Rating  int    `json:"rating" validate:"min=1,max=5"`
		Comment string `json:"comment"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Validate rating
	if input.Rating != 0 && (input.Rating < 1 || input.Rating > 5) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Rating must be between 1 and 5"})
	}

	var review models.Review
	if err := s.DB.First(&review, "id = ?", reviewID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "review not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	// Update fields if provided
	if input.Rating != 0 {
		review.Rating = input.Rating
	}
	if input.Comment != "" {
		review.Comment = input.Comment
	}

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&review).Error; err != nil {
			return err
		}

		// Recalculate average rating for the game
		var avgRating float64
		err := tx.Model(&models.Review{}).Where("game_id = ?", review.GameID).Select("AVG(rating)").Scan(&avgRating).Error
		if err != nil {
			return err
		}

		// Update game's average rating
		return tx.Model(&models.Game{}).Where("id = ?", review.GameID).Update("average_rating", avgRating).Error
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update review"})
	}

	return c.JSON(review)
}

// DeleteReview deletes a review and recalculates average rating
func (s *GameService) DeleteReview(c *fiber.Ctx) error {
	reviewID := c.Params("review_id")

	var review models.Review
	if err := s.DB.First(&review, "id = ?", reviewID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "review not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	gameID := review.GameID

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&review).Error; err != nil {
			return err
		}

		// Recalculate average rating for the game
		var avgRating float64
		err := tx.Model(&models.Review{}).Where("game_id = ?", gameID).Select("AVG(rating)").Scan(&avgRating).Error
		if err != nil {
			return err
		}

		// Update game's average rating
		return tx.Model(&models.Game{}).Where("id = ?", gameID).Update("average_rating", avgRating).Error
	})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete review"})
	}

	return c.JSON(fiber.Map{"message": "review deleted successfully"})
}

// GetUserReviewStatus checks if a user has reviewed a specific game
// This function already exists in your provided code.
func (s *GameService) GetUserReviewStatus(c *fiber.Ctx) error {
	gameID := c.Params("id")
	userID := c.Query("user_id") // Get user ID from query param

	if userID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user_id is required"})
	}

	var review models.Review
	if err := s.DB.Where("game_id = ? AND user_id = ?", gameID, userID).First(&review).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// User has not reviewed this game
			return c.JSON(fiber.Map{
				"has_reviewed": false,
				"review":       nil,
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to check review status"})
	}

	// User has reviewed this game
	return c.JSON(fiber.Map{
		"has_reviewed": true,
		"review":       review,
	})
}


// SetGameFeatured marks a game as featured or not featured.
func (s *GameService) SetGameFeatured(c *fiber.Ctx) error {
	id := c.Params("id")
	featureAction := c.Params("action") // Expect "feature" or "unfeature"

	var game models.Game
	if err := s.DB.First(&game, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "game not found"})
		}
		log.Printf("DB Error fetching game: %v", err) // Log error for debugging
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	isFeatured := false
	if featureAction == "feature" {
		isFeatured = true
	} else if featureAction != "unfeature" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid action, use 'feature' or 'unfeature'"})
	}

	game.IsFeatured = isFeatured

	if err := s.DB.Save(&game).Error; err != nil {
		log.Printf("DB Error updating game featured status: %v", err) // Log error for debugging
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to update game"})
	}

	actionMessage := "featured"
	if !isFeatured {
		actionMessage = "unfeatured"
	}

	return c.JSON(fiber.Map{
		"message": fmt.Sprintf("game %s successfully", actionMessage),
		"game":    game,
	})
}

// GetFeaturedGames returns all games that are marked as featured.
func (s *GameService) GetFeaturedGames(c *fiber.Ctx) error {
	var games []models.Game
	if err := s.DB.Preload("Screenshots").Preload("VideoLinks").Where("status = ? AND is_featured = ?", "published", true).Find(&games).Error; err != nil {
		log.Printf("DB Error fetching featured games: %v", err) // Log error for debugging
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to fetch featured games"})
	}
	return c.JSON(games)
}

// GetFeaturedGamesMinimal returns a lightweight list of featured games.
func (s *GameService) GetFeaturedGamesMinimal(c *fiber.Ctx) error {
	var games []models.Game
	if err := s.DB.Select(`
		id, 
		name, 
		main_logo_url, 
		platform, 
		average_rating,
		play_link,
		play_store_url,
		app_store_url,
		pc_download_url,
		is_featured
	`).
		Where("status = ? AND is_featured = ?", "published", true).
		Find(&games).Error; err != nil {
		log.Printf("DB Error fetching featured games minimal: %v", err) // Log error for debugging
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to fetch featured games"})
	}

	var minimalGames []MinimalGame
	for _, game := range games {
		minimalGames = append(minimalGames, MinimalGame{
			ID:            game.ID,
			Name:          game.Name,
			MainLogoURL:   game.MainLogoURL,
			Platform:      game.Platform,
			AverageRating: game.AverageRating,
			PlayLink:      game.PlayLink,
			PlayStoreURL:  game.PlayStoreURL,
			AppStoreURL:   game.AppStoreURL,
			PCDownloadURL: game.PCDownloadURL,
		})
	}

	return c.JSON(minimalGames)
}
