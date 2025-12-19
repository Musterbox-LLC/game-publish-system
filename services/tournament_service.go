package services

import (
	"errors"
	"fmt"
	"game-publish-system/models"
	"game-publish-system/utils"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TournamentService struct {
	DB *gorm.DB
}

func NewTournamentService(db *gorm.DB) *TournamentService {
	return &TournamentService{DB: db}
}

func (s *TournamentService) CreateTournament(c *fiber.Ctx) error {
	// --- Parse form values ---
	gameID := c.FormValue("game_id")
	name := c.FormValue("name")
	description := c.FormValue("description")
	rules := c.FormValue("rules")
	guidelines := c.FormValue("guidelines")
	genre := c.FormValue("genre")
	maxSubStr := c.FormValue("max_subscribers")
	entryFeeStr := c.FormValue("entry_fee")
	startTimeStr := c.FormValue("start_time")
	endTimeStr := c.FormValue("end_time")

	// --- NEW: Parse Prize Pool, Requirements, Sponsor Name, Is Featured, Publish Schedule ---
	prizePool := c.FormValue("prize_pool")
	requirementsStr := c.FormValue("requirements")
	sponsorName := c.FormValue("sponsor_name")
	isFeaturedStr := c.FormValue("is_featured")
	publishScheduleStr := c.FormValue("publish_schedule")
	acceptsWaiversStr := c.FormValue("accepts_waivers")

	// CHANGED: Store requirements as the raw newline-separated string
	processedRequirements := requirementsStr

	// Process IsFeatured
	isFeatured := false
	if strings.ToLower(isFeaturedStr) == "true" {
		isFeatured = true
	}

	// Process AcceptsWaivers
	acceptsWaivers := true
	if strings.ToLower(acceptsWaiversStr) == "false" {
		acceptsWaivers = false
	}

	// Process PublishSchedule
	var publishSchedule *time.Time
	if publishScheduleStr != "" {
		scheduledTime, err := time.Parse(time.RFC3339, publishScheduleStr)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid publish_schedule (use RFC3339)"})
		}
		publishSchedule = &scheduledTime
	}

	// --- Validation ---
	if gameID == "" || name == "" || startTimeStr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "game_id, name, and start_time are required"})
	}

	maxSubscribers := 0
	if maxSubStr != "" {
		if n, err := strconv.Atoi(maxSubStr); err == nil && n >= 0 {
			maxSubscribers = n
		} else {
			return c.Status(400).JSON(fiber.Map{"error": "max_subscribers must be a non-negative integer"})
		}
	}

	entryFee := 0.0
	if entryFeeStr != "" {
		if f, err := strconv.ParseFloat(entryFeeStr, 64); err == nil && f >= 0 {
			entryFee = f
		} else {
			return c.Status(400).JSON(fiber.Map{"error": "entry_fee must be a non-negative number"})
		}
	}

	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid start_time (use RFC3339)"})
	}

	var endTime time.Time
	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid end_time (use RFC3339)"})
		}
	}

	// --- Check game exists ---
	var game models.Game
	if err := s.DB.First(&game, "id = ?", gameID).Error; err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "game_id not found"})
	}

	// --- Handle main photo → R2 ---
	var mainPhotoURL string
	if mainPhoto, err := c.FormFile("main_photo"); err == nil && mainPhoto.Size > 0 {
		ext := filepath.Ext(mainPhoto.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		key := "tournaments/main/" + uuid.NewString() + ext
		url, err := utils.UploadFileToR2(mainPhoto, key)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to upload main photo"})
		}
		mainPhotoURL = url
	}

	// --- Handle secondary photos (up to 5) ---
	var photos []models.TournamentPhoto
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("secondary_photos[%d]", i)
		if photo, err := c.FormFile(key); err == nil && photo.Size > 0 {
			ext := filepath.Ext(photo.Filename)
			if ext == "" {
				ext = ".jpg"
			}
			photoKey := "tournaments/photos/" + uuid.NewString() + ext
			url, err := utils.UploadFileToR2(photo, photoKey)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to upload photo %d", i+1)})
			}
			photos = append(photos, models.TournamentPhoto{
				ID:           uuid.NewString(),
				URL:          url,
				SortOrder:    i,
				TournamentID: "",
			})
		} else {
			break
		}
	}

	// --- Create tournament ---
	tournament := &models.Tournament{
		ID:             uuid.NewString(),
		GameID:         gameID,
		Name:           name,
		Description:    description,
		Rules:          rules,
		Guidelines:     guidelines,
		Genre:          genre,
		GenreTags:      c.FormValue("genre_tags"),
		MaxSubscribers: maxSubscribers,
		EntryFee:       entryFee,
		MainPhotoURL:   mainPhotoURL,
		StartTime:      startTime,
		EndTime:        endTime,
		AcceptsWaivers: acceptsWaivers,
		PrizePool:       prizePool,
		Requirements:    processedRequirements,
		SponsorName:     sponsorName,
		IsFeatured:      isFeatured,
		PublishSchedule: publishSchedule,
		Status: "draft",
	}

	// --- Save (with photos) ---
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Photos").Create(tournament).Error; err != nil {
			return err
		}
		for i := range photos {
			photos[i].TournamentID = tournament.ID
			if err := tx.Create(&photos[i]).Error; err != nil {
				return err
			}
		}
		tournament.Photos = photos
		return nil
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB insert failed"})
	}

	// Preload associations for response
	err = s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions").
		First(tournament, "id = ?", tournament.ID).Error
	if err != nil {
		log.Printf("ERROR fetching newly created tournament %s: %v", tournament.ID, err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch created tournament"})
	}

	return c.Status(201).JSON(tournament)
}

// UpdateTournament handles updating an existing tournament by ID.
func (s *TournamentService) UpdateTournament(c *fiber.Ctx) error {
	id := c.Params("id")
	var existingTournament models.Tournament

	if err := s.DB.First(&existingTournament, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tournament not found"})
		}
		log.Printf("DB Error fetching tournament %s: %v", id, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	// --- Validate and Parse start_time ---
	startTimeStr := c.FormValue("start_time")
	if startTimeStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "start_time is required and cannot be empty",
		})
	}
	parsedStartTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid start_time format. Must be RFC3339 (e.g., 2024-01-01T15:04:05Z)",
		})
	}

	// --- Validate and Parse end_time ---
	endTimeStr := c.FormValue("end_time")
	var parsedEndTime *time.Time
	if endTimeStr != "" {
		parsedET, err := time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid end_time format. Must be RFC3339 (e.g., 2024-01-01T15:04:05Z)",
			})
		}
		parsedEndTime = &parsedET
	}

	// --- Validate and Parse publish_schedule ---
	publishScheduleStr := c.FormValue("publish_schedule")
	var parsedPublishSchedule *time.Time
	if publishScheduleStr != "" {
		parsedPS, err := time.Parse(time.RFC3339, publishScheduleStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid publish_schedule format. Must be RFC3339 (e.g., 2024-01-01T15:04:05Z)",
			})
		}
		parsedPublishSchedule = &parsedPS
	}

	// --- Prepare updates map ---
	updates := map[string]interface{}{
		"start_time": parsedStartTime,
		"name":        strings.TrimSpace(c.FormValue("name")),
		"genre":       strings.TrimSpace(c.FormValue("genre")),
		"genre_tags":  strings.TrimSpace(c.FormValue("genre_tags")),
		"description": c.FormValue("description"),
		"rules":       c.FormValue("rules"),
		"guidelines":  c.FormValue("guidelines"),
		"max_subscribers": func() int {
			if v := c.FormValue("max_subscribers"); v != "" {
				if val, err := strconv.Atoi(v); err == nil {
					return val
				}
			}
			return 0
		}(),
		"entry_fee": func() float64 {
			if v := c.FormValue("entry_fee"); v != "" {
				if val, err := strconv.ParseFloat(v, 64); err == nil {
					return val
				}
			}
			return 0.0
		}(),
		"prize_pool":      c.FormValue("prize_pool"),
		"requirements":    c.FormValue("requirements"),
		"sponsor_name":    c.FormValue("sponsor_name"),
		"is_featured":     c.FormValue("is_featured") == "true",
		"accepts_waivers": c.FormValue("accepts_waivers") == "true",
		"status":          c.FormValue("status"),
	}

	if parsedEndTime != nil {
		updates["end_time"] = *parsedEndTime
	}
	if parsedPublishSchedule != nil {
		updates["publish_schedule"] = *parsedPublishSchedule
	}

	var newPhotos []models.TournamentPhoto

	// 1. Handle Main Photo Replacement
	if mainPhotoFile, err := c.FormFile("main_photo"); err == nil && mainPhotoFile.Size > 0 {
		ext := filepath.Ext(mainPhotoFile.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		key := fmt.Sprintf("tournaments/main/%s%s", uuid.NewString(), ext)
		url, err := utils.UploadFileToR2(mainPhotoFile, key)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to upload main photo"})
		}
		updates["main_photo_url"] = url
	}

	// 2. Handle Secondary Photos Replacement
	for i := 0; ; i++ {
		key := fmt.Sprintf("photos[%d]", i)
		if photoFile, err := c.FormFile(key); err == nil && photoFile.Size > 0 {
			ext := filepath.Ext(photoFile.Filename)
			if ext == "" {
				ext = ".jpg"
			}
			photoKey := fmt.Sprintf("tournaments/secondary/%s%s", uuid.NewString(), ext)
			url, err := utils.UploadFileToR2(photoFile, photoKey)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to upload secondary photo %d", i)})
			}
			newPhoto := models.TournamentPhoto{
				ID:           uuid.NewString(),
				URL:          url,
				SortOrder:    i,
				TournamentID: existingTournament.ID,
			}
			newPhotos = append(newPhotos, newPhoto)
		} else {
			break
		}
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// Step 1: Update the main tournament record
		if err := tx.Model(&existingTournament).Updates(updates).Error; err != nil {
			return err
		}

		// Step 2: Handle Photo Updates
		var existingSecondaryPhotos []models.TournamentPhoto
		if err := tx.Where("tournament_id = ?", existingTournament.ID).Find(&existingSecondaryPhotos).Error; err != nil {
			return err
		}

		for _, oldPhoto := range existingSecondaryPhotos {
			if err := tx.Delete(&oldPhoto).Error; err != nil {
				return err
			}
		}

		for _, photo := range newPhotos {
			if err := tx.Create(&photo).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		log.Printf("ERROR: Transaction failed for updating tournament %s: %v", id, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update tournament"})
	}

	// Fetch the *fully updated* tournament with ALL associations
	if err := s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions", func(db *gorm.DB) *gorm.DB {
			return db.Order("joined_at DESC")
		}).
		First(&existingTournament, "id = ?", id).Error; err != nil {
		log.Printf("ERROR: Could not refetch updated tournament %s: %v", id, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve updated tournament"})
	}

	return c.JSON(existingTournament)
}

func (s *TournamentService) GetAllTournaments(c *fiber.Ctx) error {
	var tournaments []models.Tournament

	err := s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions").
		Find(&tournaments).Error
	if err != nil {
		log.Printf("ERROR fetching tournaments with preloads: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch tournaments"})
	}

	return c.JSON(tournaments)
}

// GetAllTournamentsMini returns a minimal list of tournament details
func (s *TournamentService) GetAllTournamentsMini(c *fiber.Ctx) error {
	type TournamentMini struct {
		ID               string     `json:"id"`
		Name             string     `json:"name"`
		Status           string     `json:"status"`
		StartTime        time.Time  `json:"start_time"`
		EndTime          time.Time  `json:"end_time"`
		MainPhotoURL     string     `json:"main_photo_url"`
		EntryFee         float64    `json:"entry_fee"`
		PrizePool        string     `json:"prize_pool"`
		SponsorName      string     `json:"sponsor_name"`
		IsFeatured       bool       `json:"is_featured"`
		PublishedAt      *time.Time `json:"published_at,omitempty"`
		GameID           string     `json:"game_id"`
		GameName         string     `json:"game_name"`
		GameLogoURL      string     `json:"game_logo_url,omitempty"`
		MaxSubscribers   int        `json:"max_subscribers"`
		SubscribersCount int64      `json:"subscribers_count"`
		Genre            string     `json:"genre,omitempty"`
		Description      string     `json:"description,omitempty"`
		CreatedAt        time.Time  `json:"created_at"`
		UpdatedAt        time.Time  `json:"updated_at"`
		Requirements    string     `json:"requirements,omitempty"`
		Rules           string     `json:"rules,omitempty"`
		Guidelines      string     `json:"guidelines,omitempty"`
		AcceptsWaivers  bool       `json:"accepts_waivers"`
		PublishSchedule *time.Time `json:"publish_schedule,omitempty"`
	}

	var tournaments []TournamentMini

	query := `
        SELECT 
            t.id,
            t.name,
            t.status,
            t.start_time,
            t.end_time,
            t.main_photo_url,
            t.entry_fee,
            t.prize_pool,
            t.sponsor_name,
            t.is_featured,
            t.published_at,
            t.game_id,
            t.genre,
            t.description,
            t.requirements,
            t.rules,
            t.guidelines,
            t.max_subscribers,
            t.accepts_waivers,
            t.publish_schedule,
            t.created_at,
            t.updated_at,
            g.name as game_name,
            g.main_logo_url as game_logo_url,
            COUNT(ts.id) as subscribers_count
        FROM tournaments t
        LEFT JOIN games g ON t.game_id = g.id
        LEFT JOIN tournament_subscriptions ts ON t.id = ts.tournament_id
        GROUP BY t.id, g.id
        ORDER BY t.created_at DESC
    `

	err := s.DB.Raw(query).Scan(&tournaments).Error
	if err != nil {
		log.Printf("ERROR fetching mini tournaments: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch tournaments"})
	}

	return c.JSON(tournaments)
}

// SubscribeToTournament adds user to tournament with full payment & waiver tracking
func (s *TournamentService) SubscribeToTournament(c *fiber.Ctx) error {
	type Req struct {
		ExternalUserID string  `json:"external_user_id" validate:"required,uuid"`
		UserName       string  `json:"user_name" validate:"required"`
		UserAvatarURL  string  `json:"user_avatar_url,omitempty"`
		WaiverCode     string  `json:"waiver_code,omitempty"`
		WaiverAmount   float64 `json:"waiver_amount,omitempty"`
		PaymentID      string  `json:"payment_id,omitempty"`
		PaymentAmount  float64 `json:"payment_amount,omitempty"`
		PaymentStatus  string  `json:"payment_status" validate:"oneof=pending paid failed refunded waived"`
		TransactionID  string  `json:"transaction_id,omitempty"`
		PaymentMethod  string  `json:"payment_method,omitempty"`
	}

	tournamentID := c.Params("id")
	if tournamentID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "tournament_id required in URL"})
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}

	if req.ExternalUserID == "" || req.UserName == "" {
		return c.Status(400).JSON(fiber.Map{"error": "external_user_id and user_name are required"})
	}

	if req.PaymentStatus == "" {
		return c.Status(400).JSON(fiber.Map{"error": "payment_status is required"})
	}

	// Fetch tournament
	var tournament models.Tournament
	if err := s.DB.First(&tournament, "id = ?", tournamentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error fetching tournament"})
	}

	// 🔑 WAIVER LOGIC
	var effectiveEntryFee float64 = tournament.EntryFee
	var waiverToUse *models.UserWaiver
	var amountToApply float64 = 0.0

	if req.WaiverCode != "" {
		if !tournament.AcceptsWaivers {
			return c.Status(403).JSON(fiber.Map{"error": "this tournament does not accept waivers"})
		}

		codeUpper := strings.ToUpper(req.WaiverCode)
		var w models.UserWaiver
		if err := s.DB.Where("user_id = ? AND UPPER(code) = ?", req.ExternalUserID, codeUpper).First(&w).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return c.Status(400).JSON(fiber.Map{"error": "waiver code not found or not owned by user"})
			}
			return c.Status(500).JSON(fiber.Map{"error": "DB error fetching waiver", "details": err.Error()})
		}

		if !w.IsActive {
			return c.Status(400).JSON(fiber.Map{"error": "waiver is not active"})
		}
		if w.ExpiresAt != nil && w.ExpiresAt.Before(time.Now()) {
			return c.Status(400).JSON(fiber.Map{"error": "waiver has expired"})
		}

		remaining := w.Amount - w.UsedAmount
		if remaining <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "waiver is fully used and has no remaining balance"})
		}

		amountToApply = req.WaiverAmount
		if amountToApply <= 0 {
			amountToApply = remaining
		}
		if amountToApply > remaining {
			amountToApply = remaining
		}

		waiverToUse = &w
		effectiveEntryFee = tournament.EntryFee - amountToApply
		if effectiveEntryFee < 0 {
			effectiveEntryFee = 0
		}

		log.Printf("Applying waiver %s: $%.2f (remaining $%.2f), effective fee: $%.2f", w.Code, amountToApply, remaining, effectiveEntryFee)
	}

	// Check already subscribed
	var existingSub models.TournamentSubscription
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, req.ExternalUserID).
		First(&existingSub).Error; err == nil {
		return c.Status(409).JSON(fiber.Map{
			"error":        "user already subscribed",
			"subscription": existingSub,
		})
	}

	// Enforce max subscribers
	if tournament.MaxSubscribers > 0 {
		var count int64
		s.DB.Model(&models.TournamentSubscription{}).
			Where("tournament_id = ?", tournamentID).
			Count(&count)
		if int(count) >= tournament.MaxSubscribers {
			return c.Status(403).JSON(fiber.Map{"error": "tournament is full"})
		}
	}

	// 🔐 Payment validation
	paymentAmount := req.PaymentAmount
	paymentID := req.PaymentID
	transactionID := req.TransactionID
	paymentMethod := req.PaymentMethod
	var paymentAt *time.Time

	switch req.PaymentStatus {
	case "waived":
		if effectiveEntryFee > 0 {
			return c.Status(400).JSON(fiber.Map{"error": "payment_status 'waived' invalid when effective fee > 0"})
		}
		paymentAmount = 0.0
		if paymentID == "" {
			paymentID = "waived-" + uuid.NewString()
		}
	case "paid":
		if paymentAmount <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "payment_amount must be > 0 for 'paid'"})
		}
		if paymentAmount != effectiveEntryFee {
			return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("payment_amount ($%.2f) must match effective fee ($%.2f) for 'paid'", paymentAmount, effectiveEntryFee)})
		}
		if paymentID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "payment_id required for 'paid'"})
		}
		now := time.Now()
		paymentAt = &now
	case "refunded", "failed":
		if paymentID == "" {
			return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("payment_id required for '%s'", req.PaymentStatus)})
		}
		if req.PaymentStatus == "refunded" && paymentAmount <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "payment_amount must be > 0 for 'refunded'"})
		}
		now := time.Now()
		paymentAt = &now
	case "pending":
		if effectiveEntryFee > 0 && paymentID == "" {
			paymentID = "pending-" + uuid.NewString()
		}
		if paymentAmount != effectiveEntryFee {
			log.Printf("Warning: payment_amount ($%.2f) does not match effective fee ($%.2f) for 'pending'. Using effective fee.", paymentAmount, effectiveEntryFee)
			paymentAmount = effectiveEntryFee
		}
	}

	// ✅ Create subscription
	var subUserAvatarURL *string
	if req.UserAvatarURL != "" {
		subUserAvatarURL = &req.UserAvatarURL
	}

	sub := models.TournamentSubscription{
		ID:              uuid.NewString(),
		TournamentID:    tournamentID,
		ExternalUserID:  req.ExternalUserID,
		UserName:        req.UserName,
		UserAvatarURL:   subUserAvatarURL,
		JoinedAt:        time.Now(),
		PaymentID:       paymentID,
		PaymentAmount:   paymentAmount,
		PaymentStatus:   req.PaymentStatus,
		TransactionID:   transactionID,
		PaymentMethod:   paymentMethod,
		PaymentAt:       paymentAt,
		WaiverCodeUsed:  req.WaiverCode,
		WaiverAmountUsed: amountToApply,
	}

	// 🔁 Atomic Waiver Update and Subscription Creation
	if waiverToUse != nil && amountToApply > 0 {
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			var wLocked models.UserWaiver
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", waiverToUse.ID).
				First(&wLocked).Error; err != nil {
				return fmt.Errorf("failed to lock waiver for update: %w", err)
			}

			remainingInTx := wLocked.Amount - wLocked.UsedAmount
			if remainingInTx < amountToApply {
				return fmt.Errorf("insufficient waiver balance at transaction time (have $%.2f, need $%.2f)", remainingInTx, amountToApply)
			}

			newUsed := wLocked.UsedAmount + amountToApply
			if newUsed > wLocked.Amount {
				return fmt.Errorf("calculated used amount exceeds waiver total")
			}

			if err := tx.Model(&wLocked).
				Where("id = ?", wLocked.ID).
				Updates(map[string]interface{}{
					"used_amount": newUsed,
					"is_redeemed": true,
				}).Error; err != nil {
				return fmt.Errorf("failed to update waiver used amount: %w", err)
			}

			sub.WaiverIDUsed = wLocked.ID

			if err := tx.Create(&sub).Error; err != nil {
				return fmt.Errorf("failed to create subscription: %w", err)
			}

			return nil
		})
		if err != nil {
			log.Printf("Transaction failed for subscription with waiver: %v", err)
			return c.Status(500).JSON(fiber.Map{"error": "subscription with waiver failed", "details": err.Error()})
		}
	} else {
		if err := s.DB.Create(&sub).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to create subscription", "details": err.Error()})
		}
	}

	return c.Status(201).JSON(fiber.Map{
		"message": "subscription created successfully",
		"subscription": fiber.Map{
			"id":                 sub.ID,
			"tournament_id":      sub.TournamentID,
			"external_user_id":   sub.ExternalUserID,
			"user_name":          sub.UserName,
			"user_avatar_url":    sub.UserAvatarURL,
			"joined_at":          sub.JoinedAt,
			"payment_status":     sub.PaymentStatus,
			"payment_amount":     sub.PaymentAmount,
			"waiver_code_used":   sub.WaiverCodeUsed,
			"waiver_amount_used": sub.WaiverAmountUsed,
		},
	})
}

// SuspendSubscription suspends a user's subscription
func (s *TournamentService) SuspendSubscription(c *fiber.Ctx) error {
	tournamentID := c.Params("tournament_id")
	userID := c.Params("user_id")

	type Req struct {
		Reason string `json:"reason"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	var sub models.TournamentSubscription
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID).First(&sub).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "subscription not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}

	now := time.Now()
	updates := map[string]interface{}{
		"payment_status":   "suspended",
		"suspended_at":     &now,
		"suspended_reason": req.Reason,
	}

	if err := s.DB.Model(&sub).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "suspend failed"})
	}

	return c.JSON(fiber.Map{"message": "subscription suspended", "subscription": sub})
}

// RevokeSubscription revokes a user's subscription
func (s *TournamentService) RevokeSubscription(c *fiber.Ctx) error {
	tournamentID := c.Params("tournament_id")
	userID := c.Params("user_id")

	type Req struct {
		Reason string `json:"reason"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	var sub models.TournamentSubscription
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID).First(&sub).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "subscription not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}

	if sub.PaymentStatus == "paid" {
		log.Printf("INFO: Subscription for user %s in tournament %s was paid ($%.2f). Refund logic needed.", userID, tournamentID, sub.PaymentAmount)
	} else if sub.WaiverCodeUsed != "" && sub.WaiverAmountUsed > 0 {
		log.Printf("INFO: Subscription for user %s in tournament %s used waiver '%s' ($%.2f). Waiver benefit lost.", userID, tournamentID, sub.WaiverCodeUsed, sub.WaiverAmountUsed)
	}

	now := time.Now()
	updates := map[string]interface{}{
		"payment_status": "revoked",
		"revoked_at":     &now,
		"revoked_reason": req.Reason,
	}

	if err := s.DB.Model(&sub).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "revoke failed"})
	}

	return c.JSON(fiber.Map{"message": "subscription revoked", "subscription": sub})
}

// getUserAvailableWaivers returns active, unexpired, non-exhausted waivers for a user
func (s *TournamentService) getUserAvailableWaivers(userID string) ([]models.UserWaiver, error) {
	var waivers []models.UserWaiver
	now := time.Now()
	query := s.DB.Where("user_id = ? AND is_active = true AND used_amount < amount", userID)
	query = query.Where("expires_at IS NULL OR expires_at > ?", now)
	if err := query.Find(&waivers).Error; err != nil {
		return nil, err
	}
	return waivers, nil
}

// RefundSubscription updates a subscription to 'refunded' status
func (s *TournamentService) RefundSubscription(c *fiber.Ctx) error {
	type Req struct {
		RefundReason string `json:"refund_reason,omitempty"`
		RefundedBy   string `json:"refunded_by,omitempty"`
	}

	tournamentID := c.Params("tournament_id")
	userID := c.Params("user_id")

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	if tournamentID == "" || userID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "tournament_id and user_id are required in URL"})
	}

	var sub models.TournamentSubscription
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID).
		First(&sub).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "subscription not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}

	if sub.PaymentStatus == "refunded" {
		return c.Status(400).JSON(fiber.Map{"error": "already refunded"})
	}

	if sub.PaymentStatus != "paid" {
		return c.Status(400).JSON(fiber.Map{
			"error":   "only 'paid' subscriptions can be refunded",
			"current": sub.PaymentStatus,
		})
	}

	now := time.Now()
	updates := map[string]interface{}{
		"payment_status": "refunded",
		"payment_at":     now,
	}

	if err := s.DB.Model(&sub).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "refund failed"})
	}

	s.DB.First(&sub, "id = ?", sub.ID)

	return c.JSON(fiber.Map{
		"message":      "refund processed",
		"subscription": sub,
	})
}

// CreateBatchWithMatchesAndRounds creates a batch, matches, and rounds in a single atomic operation
func (s *TournamentService) CreateBatchWithMatchesAndRounds(c *fiber.Ctx) error {
	tournamentID := c.Params("id")

	type RoundReq struct {
		Name         string `json:"name" validate:"required"`
		Description  string `json:"description"`
		SortOrder    int    `json:"sort_order"`
		StartDate    string `json:"start_date" validate:"required"`
		EndDate      string `json:"end_date" validate:"required"`
		DurationMins int    `json:"duration_mins"`
		ScoreType    string `json:"score_type"`
		Attempts     int    `json:"attempts"`
	}

	type MatchReq struct {
		Name        string     `json:"name" validate:"required"`
		Description string     `json:"description"`
		SortOrder   int        `json:"sort_order"`
		StartDate   string     `json:"start_date"`
		EndDate     string     `json:"end_date"`
		Rounds      []RoundReq `json:"rounds" validate:"dive"`
		Player1ID   string     `json:"player1_id,omitempty"`
		Player1Name string     `json:"player1_name,omitempty"`
		Player2ID   string     `json:"player2_id,omitempty"`
		Player2Name string     `json:"player2_name,omitempty"`
	}

	type BatchReq struct {
		Name        string     `json:"name" validate:"required"`
		Description string     `json:"description"`
		SortOrder   int        `json:"sort_order"`
		StartDate   string     `json:"start_date"`
		EndDate     string     `json:"end_date"`
		Matches     []MatchReq `json:"matches" validate:"dive"`
	}

	var req BatchReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	// Validate tournament exists
	var tournament models.Tournament
	if err := s.DB.First(&tournament, "id = ?", tournamentID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
	}

	// Parse batch dates
	var batchStartDate, batchEndDate time.Time
	var err error
	if req.StartDate != "" {
		batchStartDate, err = time.Parse(time.RFC3339, req.StartDate)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid batch start_date"})
		}
	}
	if req.EndDate != "" {
		batchEndDate, err = time.Parse(time.RFC3339, req.EndDate)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid batch end_date"})
		}
	}

	// Prepare matches and rounds
	var matches []models.TournamentMatch
	for _, matchReq := range req.Matches {
		// Parse match dates
		var matchStartDate, matchEndDate time.Time
		if matchReq.StartDate != "" {
			matchStartDate, err = time.Parse(time.RFC3339, matchReq.StartDate)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("invalid match start_date for match '%s'", matchReq.Name)})
			}
		}
		if matchReq.EndDate != "" {
			matchEndDate, err = time.Parse(time.RFC3339, matchReq.EndDate)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("invalid match end_date for match '%s'", matchReq.Name)})
			}
		}

		// Prepare rounds for this match
		var rounds []models.TournamentRound
		for _, roundReq := range matchReq.Rounds {
			startDate, err := time.Parse(time.RFC3339, roundReq.StartDate)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("invalid round start_date for round '%s'", roundReq.Name)})
			}
			endDate, err := time.Parse(time.RFC3339, roundReq.EndDate)
			if err != nil {
				return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("invalid round end_date for round '%s'", roundReq.Name)})
			}

			if !endDate.After(startDate) {
				return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("round end_date must be after start_date for round '%s'", roundReq.Name)})
			}

			round := models.TournamentRound{
				ID:           uuid.NewString(),
				Name:         roundReq.Name,
				Description:  roundReq.Description,
				SortOrder:    roundReq.SortOrder,
				StartDate:    startDate,
				EndDate:      endDate,
				DurationMins: roundReq.DurationMins,
				Status:       "pending",
				ScoreType:    roundReq.ScoreType,
				Attempts:     roundReq.Attempts,
			}
			if round.ScoreType == "" {
				round.ScoreType = "highest"
			}
			rounds = append(rounds, round)
		}

		match := models.TournamentMatch{
			ID:          uuid.NewString(),
			Name:        matchReq.Name,
			Description: matchReq.Description,
			SortOrder:   matchReq.SortOrder,
			StartDate:   matchStartDate,
			EndDate:     matchEndDate,
			Status:      "pending",
			Player1ID:   matchReq.Player1ID,
			Player1Name: matchReq.Player1Name,
			Player2ID:   matchReq.Player2ID,
			Player2Name: matchReq.Player2Name,
			Rounds:      rounds,
		}
		matches = append(matches, match)
	}

	// Create batch with matches and rounds in a transaction
	batch := &models.TournamentBatch{
		ID:           uuid.NewString(),
		TournamentID: tournamentID,
		Name:         req.Name,
		Description:  req.Description,
		SortOrder:    req.SortOrder,
		StartDate:    batchStartDate,
		EndDate:      batchEndDate,
		Matches:      matches,
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// Create the batch
		if err := tx.Create(batch).Error; err != nil {
			return err
		}

		// Create matches and their rounds
		for i := range batch.Matches {
			batch.Matches[i].BatchID = batch.ID
			if err := tx.Create(&batch.Matches[i]).Error; err != nil {
				return err
			}

			// Create rounds for this match
			for j := range batch.Matches[i].Rounds {
				batch.Matches[i].Rounds[j].MatchID = batch.Matches[i].ID
				if err := tx.Create(&batch.Matches[i].Rounds[j]).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		log.Printf("ERROR creating batch with matches and rounds for tournament %s: %v", tournamentID, err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to create batch", "details": err.Error()})
	}

	return c.Status(201).JSON(batch)
}


// UpdateBatch updates an existing batch and optionally its associated matches and rounds
func (s *TournamentService) UpdateBatch(c *fiber.Ctx) error {
	id := c.Params("id")

	type RoundUpdateReq struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		SortOrder    int    `json:"sort_order"`
		StartDate    string `json:"start_date"`
		EndDate      string `json:"end_date"`
		DurationMins int    `json:"duration_mins"`
		ScoreType    string `json:"score_type"`
		Attempts     int    `json:"attempts"`
		ToDelete     bool   `json:"to_delete"`
	}

	type MatchUpdateReq struct {
		ID           string           `json:"id"`
		Name         string           `json:"name"`
		Description  string           `json:"description"`
		SortOrder    int              `json:"sort_order"`
		StartDate    string           `json:"start_date"`
		EndDate      string           `json:"end_date"`
		Status       string           `json:"status"`
		Player1ID    string           `json:"player1_id,omitempty"`
		Player1Name  string           `json:"player1_name,omitempty"`
		Player1Score int64            `json:"player1_score,omitempty"`
		Player2ID    string           `json:"player2_id,omitempty"`
		Player2Name  string           `json:"player2_name,omitempty"`
		Player2Score int64            `json:"player2_score,omitempty"`
		WinnerID     string           `json:"winner_id,omitempty"`
		WinnerName   string           `json:"winner_name,omitempty"`
		Rounds       []RoundUpdateReq `json:"rounds,omitempty"`
		ToDelete     bool             `json:"to_delete"`
	}

	type BatchUpdateReq struct {
		Name        string           `json:"name"`
		Description string           `json:"description"`
		SortOrder   int              `json:"sort_order"`
		StartDate   string           `json:"start_date"`
		EndDate     string           `json:"end_date"`
		Matches     []MatchUpdateReq `json:"matches,omitempty"`
	}

	var req BatchUpdateReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	var batch models.TournamentBatch
	if err := s.DB.Preload("Matches").Preload("Matches.Rounds").First(&batch, "id = ?", id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "batch not found"})
	}

	// Update batch fields
	if req.Name != "" {
		batch.Name = req.Name
	}
	if req.Description != "" {
		batch.Description = req.Description
	}
	batch.SortOrder = req.SortOrder

	if req.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, req.StartDate); err == nil {
			batch.StartDate = t
		} else {
			return c.Status(400).JSON(fiber.Map{"error": "invalid start_date"})
		}
	}
	if req.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, req.EndDate); err == nil {
			batch.EndDate = t
		} else {
			return c.Status(400).JSON(fiber.Map{"error": "invalid end_date"})
		}
	}

	// Perform updates in a transaction
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// 1. Update the batch itself
		if err := tx.Save(&batch).Error; err != nil {
			return err
		}

		// Handle matches updates
		for _, matchReq := range req.Matches {
			if matchReq.ToDelete {
				// Delete match and its rounds
				if err := tx.Where("match_id = ?", matchReq.ID).Delete(&models.TournamentRound{}).Error; err != nil {
					return err
				}
				if err := tx.Where("id = ?", matchReq.ID).Delete(&models.TournamentMatch{}).Error; err != nil {
					return err
				}
				continue
			}

			if matchReq.ID == "" {
				// Create new match
				var matchStartDate, matchEndDate time.Time
				var parseErr error
				
				if matchReq.StartDate != "" {
					matchStartDate, parseErr = time.Parse(time.RFC3339, matchReq.StartDate)
					if parseErr != nil {
						return parseErr
					}
				}
				if matchReq.EndDate != "" {
					matchEndDate, parseErr = time.Parse(time.RFC3339, matchReq.EndDate)
					if parseErr != nil {
						return parseErr
					}
				}

				newMatch := models.TournamentMatch{
					ID:           uuid.NewString(),
					BatchID:      batch.ID,
					Name:         matchReq.Name,
					Description:  matchReq.Description,
					SortOrder:    matchReq.SortOrder,
					StartDate:    matchStartDate,
					EndDate:      matchEndDate,
					Status:       matchReq.Status,
					Player1ID:    matchReq.Player1ID,
					Player1Name:  matchReq.Player1Name,
					Player1Score: matchReq.Player1Score,
					Player2ID:    matchReq.Player2ID,
					Player2Name:  matchReq.Player2Name,
					Player2Score: matchReq.Player2Score,
					WinnerID:     matchReq.WinnerID,
					WinnerName:   matchReq.WinnerName,
				}
				if err := tx.Create(&newMatch).Error; err != nil {
					return err
				}
			} else {
				// Update existing match
				var match models.TournamentMatch
				if err := tx.First(&match, "id = ?", matchReq.ID).Error; err != nil {
					return err
				}

				if matchReq.Name != "" {
					match.Name = matchReq.Name
				}
				if matchReq.Description != "" {
					match.Description = matchReq.Description
				}
				match.SortOrder = matchReq.SortOrder
				if matchReq.Status != "" {
					match.Status = matchReq.Status
				}
				match.Player1ID = matchReq.Player1ID
				match.Player1Name = matchReq.Player1Name
				match.Player1Score = matchReq.Player1Score
				match.Player2ID = matchReq.Player2ID
				match.Player2Name = matchReq.Player2Name
				match.Player2Score = matchReq.Player2Score
				match.WinnerID = matchReq.WinnerID
				match.WinnerName = matchReq.WinnerName

				if matchReq.StartDate != "" {
					if t, err := time.Parse(time.RFC3339, matchReq.StartDate); err == nil {
						match.StartDate = t
					}
				}
				if matchReq.EndDate != "" {
					if t, err := time.Parse(time.RFC3339, matchReq.EndDate); err == nil {
						match.EndDate = t
					}
				}

				if err := tx.Save(&match).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		log.Printf("ERROR updating batch %s: %v", id, err)
		return c.Status(500).JSON(fiber.Map{"error": "update failed", "details": err.Error()})
	}

	// Fetch the updated batch with matches and rounds for the response
	s.DB.Preload("Matches", func(db *gorm.DB) *gorm.DB {
		return db.Order("\"sort_order\" ASC")
	}).Preload("Matches.Rounds", func(db *gorm.DB) *gorm.DB {
		return db.Order("\"sort_order\" ASC")
	}).First(&batch, "id = ?", id)

	return c.JSON(batch)
}

// DeleteBatch deletes a batch and all its associated matches and rounds
func (s *TournamentService) DeleteBatch(c *fiber.Ctx) error {
	id := c.Params("id")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Delete rounds first (depend on matches)
		if err := tx.Where("match_id IN (SELECT id FROM tournament_matches WHERE batch_id = ?)", id).
			Delete(&models.TournamentRound{}).Error; err != nil {
			return err
		}
		// Delete matches
		if err := tx.Where("batch_id = ?", id).
			Delete(&models.TournamentMatch{}).Error; err != nil {
			return err
		}
		// Then delete the batch itself
		result := tx.Delete(&models.TournamentBatch{}, "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fiber.NewError(404, "batch not found")
		}
		return nil
	})
}

// CreateMatch creates a new match within a batch
func (s *TournamentService) CreateMatch(c *fiber.Ctx) error {
	batchID := c.Params("batch_id")

	type Req struct {
		Name        string `json:"name" validate:"required"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
		Player1ID   string `json:"player1_id,omitempty"`
		Player1Name string `json:"player1_name,omitempty"`
		Player2ID   string `json:"player2_id,omitempty"`
		Player2Name string `json:"player2_name,omitempty"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	// Fetch batch to verify it exists
	var batch models.TournamentBatch
	if err := s.DB.First(&batch, "id = ?", batchID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "batch not found"})
	}

	// Parse dates
	var startDate, endDate time.Time
	var err error
	if req.StartDate != "" {
		startDate, err = time.Parse(time.RFC3339, req.StartDate)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid start_date"})
		}
	}
	if req.EndDate != "" {
		endDate, err = time.Parse(time.RFC3339, req.EndDate)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid end_date"})
		}
	}

	match := &models.TournamentMatch{
		ID:          uuid.NewString(),
		BatchID:     batchID,
		Name:        req.Name,
		Description: req.Description,
		SortOrder:   req.SortOrder,
		StartDate:   startDate,
		EndDate:     endDate,
		Status:      "pending",
		Player1ID:   req.Player1ID,
		Player1Name: req.Player1Name,
		Player2ID:   req.Player2ID,
		Player2Name: req.Player2Name,
	}

	if err := s.DB.Create(match).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to create match"})
	}

	return c.Status(201).JSON(match)
}

// UpdateMatch updates an existing match
func (s *TournamentService) UpdateMatch(c *fiber.Ctx) error {
	id := c.Params("id")

	type Req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
		Status      string `json:"status"`
		Player1ID   string `json:"player1_id,omitempty"`
		Player1Name string `json:"player1_name,omitempty"`
		Player1Score int64 `json:"player1_score,omitempty"`
		Player2ID   string `json:"player2_id,omitempty"`
		Player2Name string `json:"player2_name,omitempty"`
		Player2Score int64 `json:"player2_score,omitempty"`
		WinnerID    string `json:"winner_id,omitempty"`
		WinnerName  string `json:"winner_name,omitempty"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	var match models.TournamentMatch
	if err := s.DB.First(&match, "id = ?", id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "match not found"})
	}

	// Update fields
	if req.Name != "" {
		match.Name = req.Name
	}
	if req.Description != "" {
		match.Description = req.Description
	}
	match.SortOrder = req.SortOrder

	if req.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, req.StartDate); err == nil {
			match.StartDate = t
		}
	}
	if req.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, req.EndDate); err == nil {
			match.EndDate = t
		}
	}
	if req.Status != "" {
		match.Status = req.Status
	}
	
	match.Player1ID = req.Player1ID
	match.Player1Name = req.Player1Name
	match.Player1Score = req.Player1Score
	match.Player2ID = req.Player2ID
	match.Player2Name = req.Player2Name
	match.Player2Score = req.Player2Score
	match.WinnerID = req.WinnerID
	match.WinnerName = req.WinnerName

	if err := s.DB.Save(&match).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "update failed"})
	}

	return c.JSON(match)
}

// DeleteMatch deletes a match and its associated rounds
func (s *TournamentService) DeleteMatch(c *fiber.Ctx) error {
	id := c.Params("id")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Delete rounds first
		if err := tx.Where("match_id = ?", id).Delete(&models.TournamentRound{}).Error; err != nil {
			return err
		}
		// Then delete the match
		result := tx.Delete(&models.TournamentMatch{}, "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fiber.NewError(404, "match not found")
		}
		return nil
	})
}

// CreateRound creates a new round within a match
func (s *TournamentService) CreateRound(c *fiber.Ctx) error {
	matchID := c.Params("match_id")

	type Req struct {
		Name         string `json:"name" validate:"required"`
		Description  string `json:"description"`
		SortOrder    int    `json:"sort_order"`
		StartDate    string `json:"start_date" validate:"required"`
		EndDate      string `json:"end_date" validate:"required"`
		DurationMins int    `json:"duration_mins"`
		Status       string `json:"status"`
		ScoreType    string `json:"score_type"`
		Attempts     int    `json:"attempts"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	// Fetch match to get batch and tournament IDs
	var match models.TournamentMatch
	if err := s.DB.First(&match, "id = ?", matchID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "match not found"})
	}

	startDate, err := time.Parse(time.RFC3339, req.StartDate)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid start_date"})
	}
	endDate, err := time.Parse(time.RFC3339, req.EndDate)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid end_date"})
	}

	if !endDate.After(startDate) {
		return c.Status(400).JSON(fiber.Map{"error": "end_date must be after start_date"})
	}

	round := &models.TournamentRound{
		ID:           uuid.NewString(),
		MatchID:      matchID,
		Name:         req.Name,
		Description:  req.Description,
		SortOrder:    req.SortOrder,
		StartDate:    startDate,
		EndDate:      endDate,
		DurationMins: req.DurationMins,
		Status:       "pending",
		ScoreType:    req.ScoreType,
		Attempts:     req.Attempts,
	}
	if round.ScoreType == "" {
		round.ScoreType = "highest"
	}

	if err := s.DB.Create(round).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to create round"})
	}

	return c.Status(201).JSON(round)
}

// UpdateRound updates a single round
func (s *TournamentService) UpdateRound(c *fiber.Ctx) error {
	id := c.Params("id")

	type Req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		SortOrder    int    `json:"sort_order"`
		StartDate    string `json:"start_date"`
		EndDate      string `json:"end_date"`
		DurationMins int    `json:"duration_mins"`
		Status       string `json:"status"`
		ScoreType    string `json:"score_type"`
		Attempts     int    `json:"attempts"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	var round models.TournamentRound
	if err := s.DB.First(&round, "id = ?", id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "round not found"})
	}

	// Update fields if provided in the request
	if req.Name != "" {
		round.Name = req.Name
	}
	if req.Description != "" {
		round.Description = req.Description
	}
	round.SortOrder = req.SortOrder

	if req.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, req.StartDate); err == nil {
			round.StartDate = t
		} else {
			return c.Status(400).JSON(fiber.Map{"error": "invalid start_date"})
		}
	}
	if req.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, req.EndDate); err == nil {
			round.EndDate = t
		} else {
			return c.Status(400).JSON(fiber.Map{"error": "invalid end_date"})
		}
	}
	if req.DurationMins != 0 {
		round.DurationMins = req.DurationMins
	}
	if req.Status != "" {
		round.Status = req.Status
	}
	if req.ScoreType != "" {
		round.ScoreType = req.ScoreType
	}
	if req.Attempts != 0 {
		round.Attempts = req.Attempts
	}

	// Validate dates after potential updates
	if !round.EndDate.After(round.StartDate) && !round.EndDate.IsZero() {
		return c.Status(400).JSON(fiber.Map{"error": "end_date must be after start_date"})
	}

	if err := s.DB.Save(&round).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "update failed"})
	}

	return c.JSON(round)
}

// DeleteRound deletes a single round
func (s *TournamentService) DeleteRound(c *fiber.Ctx) error {
	id := c.Params("id")

	result := s.DB.Delete(&models.TournamentRound{}, "id = ?", id)
	if result.Error != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}
	if result.RowsAffected == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "round not found"})
	}

	return c.JSON(fiber.Map{"message": "round deleted"})
}

func (s *TournamentService) GetTournamentByID(c *fiber.Ctx) error {
	id := c.Params("id")
	var tournament models.Tournament

	// Preload all necessary associations with the new hierarchy
	err := s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions", func(db *gorm.DB) *gorm.DB {
			return db.Order("joined_at DESC")
		}).
		First(&tournament, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
		}
		log.Printf("ERROR fetching tournament %s: %v", id, err)
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}

	// Calculate related counts for the tournament
	var subsCount int64
	s.DB.Model(&models.TournamentSubscription{}).
		Where("tournament_id = ?", id).
		Count(&subsCount)

	var activeSubsCount int64
	s.DB.Model(&models.TournamentSubscription{}).
		Where("tournament_id = ? AND payment_status = 'paid'", id).
		Count(&activeSubsCount)

	var leaderboardCount int64
	s.DB.Model(&models.LeaderboardEntry{}).
		Where("tournament_id = ?", id).
		Count(&leaderboardCount)

	// Calculate available slots
	availableSlots := int64(tournament.MaxSubscribers) - subsCount
	if tournament.MaxSubscribers <= 0 {
		availableSlots = -1
	}

	// Set the calculated fields on the tournament object
	tournament.SubscribersCount = subsCount
	tournament.ActiveSubscribersCount = activeSubsCount
	tournament.AvailableSlots = availableSlots

	return c.JSON(tournament)
}

func (s *TournamentService) DeleteTournament(c *fiber.Ctx) error {
	id := c.Params("id")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Delete in correct order (from leaf to root):
		// 1. TournamentRound (depends on Match)
		if err := tx.Where("match_id IN (SELECT id FROM tournament_matches WHERE batch_id IN (SELECT id FROM tournament_batches WHERE tournament_id = ?))", id).
			Delete(&models.TournamentRound{}).Error; err != nil {
			return err
		}
		
		// 2. TournamentMatch (depends on Batch)
		if err := tx.Where("batch_id IN (SELECT id FROM tournament_batches WHERE tournament_id = ?)", id).
			Delete(&models.TournamentMatch{}).Error; err != nil {
			return err
		}
		
		// 3. TournamentBatch
		if err := tx.Where("tournament_id = ?", id).
			Delete(&models.TournamentBatch{}).Error; err != nil {
			return err
		}
		
		// 4. Other dependent tables
		if err := tx.Where("tournament_id = ?", id).
			Delete(&models.TournamentPhoto{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tournament_id = ?", id).
			Delete(&models.TournamentSubscription{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tournament_id = ?", id).
			Delete(&models.LeaderboardEntry{}).Error; err != nil {
			return err
		}
		
		// 5. Finally delete the tournament
		result := tx.Delete(&models.Tournament{}, "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fiber.NewError(404, "tournament not found")
		}
		return nil
	})
}

func (s *TournamentService) UpdateTournamentStatus(c *fiber.Ctx) error {
	id := c.Params("id")

	type Req struct {
		Status string `json:"status" validate:"oneof=draft published active completed cancelled publish unpublish"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	var updates map[string]interface{}
	switch req.Status {
	case "publish":
		var tournament models.Tournament
		if err := s.DB.First(&tournament, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
			}
			return c.Status(500).JSON(fiber.Map{"error": "DB error"})
		}

		finalStatus := "published"
		publishedAt := time.Now()
		if tournament.PublishSchedule != nil {
			if tournament.PublishSchedule.Before(time.Now()) {
				finalStatus = "active"
				publishedAt = *tournament.PublishSchedule
			} else {
				finalStatus = "published"
				publishedAt = *tournament.PublishSchedule
			}
		} else {
			finalStatus = "active"
		}

		updates = map[string]interface{}{
			"status":       finalStatus,
			"published_at": publishedAt,
		}
	case "unpublish":
		updates = map[string]interface{}{
			"status":       "draft",
			"published_at": nil,
		}
	case "draft", "published", "active", "completed", "cancelled":
		updates = map[string]interface{}{
			"status": req.Status,
		}
	default:
		return c.Status(400).JSON(fiber.Map{"error": "invalid status"})
	}

	result := s.DB.Model(&models.Tournament{}).
		Where("id = ?", id).
		Updates(updates)
	if result.Error != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB update failed"})
	}
	if result.RowsAffected == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
	}

	// Return updated tournament
	var updated models.Tournament
	s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Matches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions").
		First(&updated, "id = ?", id)

	return c.JSON(updated)
}

func (s *TournamentService) GetTournamentSubscribers(c *fiber.Ctx) error {
	tournamentID := c.Params("id")
	var subs []models.TournamentSubscription

	if err := s.DB.Where("tournament_id = ?", tournamentID).
		Order("joined_at DESC").
		Find(&subs).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch subscribers"})
	}

	return c.JSON(subs)
}

// CreateBatch creates a new batch within a tournament
func (s *TournamentService) CreateBatch(c *fiber.Ctx) error {
	tournamentID := c.Params("id")

	type Req struct {
		Name        string `json:"name" validate:"required"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	// Validate tournament exists
	if err := s.DB.First(&models.Tournament{}, "id = ?", tournamentID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
	}

	var startDate, endDate time.Time
	var err error
	if req.StartDate != "" {
		startDate, err = time.Parse(time.RFC3339, req.StartDate)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid start_date"})
		}
	}
	if req.EndDate != "" {
		endDate, err = time.Parse(time.RFC3339, req.EndDate)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid end_date"})
		}
	}

	batch := &models.TournamentBatch{
		ID:           uuid.NewString(),
		TournamentID: tournamentID,
		Name:         req.Name,
		Description:  req.Description,
		SortOrder:    req.SortOrder,
		StartDate:    startDate,
		EndDate:      endDate,
	}

	if err := s.DB.Create(batch).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to create batch"})
	}

	return c.Status(201).JSON(batch)
}