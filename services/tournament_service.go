package services

import (
	"fmt"
	"game-publish-system/models"
	"game-publish-system/utils"
	"log"
	"math"
	"path/filepath"
	"strconv"
	"strings" // Import strings for processing IsFeatured
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

// --- Waiver-related request types ---
type CreateWaiverRequest struct {
	UserID      string     `json:"user_id" validate:"required"`
	Code        string     `json:"code" validate:"required"`
	Amount      float64    `json:"amount" validate:"required,gt=0"`
	Description string     `json:"description,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// UpdateWaiverRequest defines the structure for partial updates
type UpdateWaiverRequest struct {
	Code        *string    `json:"code,omitempty"`
	Amount      *float64   `json:"amount,omitempty"`
	Description *string    `json:"description,omitempty"`
	IsActive    *bool      `json:"is_active,omitempty"` // Crucial for toggling
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type RedeemWaiverRequest struct {
	UserID       string  `json:"user_id" validate:"required,uuid"`
	WaiverCode   string  `json:"waiver_code" validate:"required"`
	AmountToUse  float64 `json:"amount_to_use" validate:"required,gt=0"` // user chooses this
	TournamentID string  `json:"tournament_id" validate:"required,uuid"` // for audit + enforce tournament rules
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
	requirementsStr := c.FormValue("requirements") // This is newline-separated string from frontend
	sponsorName := c.FormValue("sponsor_name")
	isFeaturedStr := c.FormValue("is_featured")
	publishScheduleStr := c.FormValue("publish_schedule") // Expected format: RFC3339

	// CHANGED: Store requirements as the raw newline-separated string
	processedRequirements := requirementsStr // Assign the string directly

	// Process IsFeatured (assuming it's sent as "true"/"false" string from form)
	isFeatured := false
	if strings.ToLower(isFeaturedStr) == "true" {
		isFeatured = true
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
	// --- END NEW ---

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
				ID:    uuid.NewString(), // ✅ Unique ID for each photo
				URL:   url,
				Order: i,
			})
		} else {
			break // stop on first missing
		}
	}

	// --- Create tournament ---
	tournament := &models.Tournament{
		ID:             uuid.NewString(), // ✅ Ensure unique tournament ID
		GameID:         gameID,
		Name:           name,
		Description:    description,
		Rules:          rules,
		Guidelines:     guidelines,
		Genre:          genre,
		MaxSubscribers: maxSubscribers,
		EntryFee:       entryFee,
		MainPhotoURL:   mainPhotoURL,
		StartTime:      startTime,
		EndTime:        endTime,
		// --- NEW: Assign Prize Pool, Requirements, Sponsor Name, Is Featured, Publish Schedule ---
		PrizePool:       prizePool,
		Requirements:    processedRequirements, // Assign the processed string
		SponsorName:     sponsorName,
		IsFeatured:      isFeatured,
		PublishSchedule: publishSchedule,
		// --- END NEW ---
		Status: "draft", // Always start as draft
	}

	// --- Save (with photos) ---
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// Create tournament first (without photos to avoid conflicts)
		if err := tx.Omit("Photos").Create(tournament).Error; err != nil {
			return err
		}

		// Set TournamentID for each photo and create them individually
		for i := range photos {
			photos[i].TournamentID = tournament.ID
			if err := tx.Create(&photos[i]).Error; err != nil {
				return err
			}
		}

		// Update tournament with photos for response
		tournament.Photos = photos
		return nil
	})

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB insert failed"})
	}

	// Preload associations for response
	s.DB.Preload("Photos").First(tournament, "id = ?", tournament.ID)

	return c.Status(201).JSON(tournament)
}

func (s *TournamentService) GetAllTournaments(c *fiber.Ctx) error {
	var tournaments []models.Tournament
	// Only preload Game for list view (not photos/batches to save bandwidth)
	s.DB.Preload("Game").Find(&tournaments)
	return c.JSON(tournaments)
}

// GetAllTournamentsMini returns a minimal list of tournament details, including the associated game.
func (s *TournamentService) GetAllTournamentsMini(c *fiber.Ctx) error {
	var tournaments []models.Tournament

	// Fetch full Tournament models with the associated Game, selecting only the required fields for both.
	// Use the main Tournament model for the query because it defines the GORM relationship.
	err := s.DB.Model(&models.Tournament{}).
		Preload("Game", func(db *gorm.DB) *gorm.DB {
			// Select only the specific fields needed from the Game table
			return db.Select("id, name, main_logo_url") // Add other specific game fields you want to return here if needed
		}).
		Select(`
			id, 
			name, 
			status, 
			start_time, 
			main_photo_url, 
			entry_fee, 
			prize_pool, 
			sponsor_name, 
			is_featured, 
			published_at,
			game_id -- Need game_id to access the preloaded Game
		`).
		Find(&tournaments).Error

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch tournaments"})
	}

	// Manually map the results to MiniTournament structs
	miniTournaments := make([]models.MiniTournament, len(tournaments))
	for i, t := range tournaments {
		miniTournaments[i] = models.MiniTournament{
			ID:           t.ID,
			Name:         t.Name,
			Status:       t.Status,
			StartTime:    t.StartTime,
			MainPhotoURL: t.MainPhotoURL,
			EntryFee:     t.EntryFee,
			PrizePool:    t.PrizePool,
			SponsorName:  t.SponsorName,
			IsFeatured:   t.IsFeatured,
			PublishedAt:  t.PublishedAt,
			// Manually assign the preloaded Game object
			Game: t.Game, // This works because t.Game was preloaded
		}
	}

	return c.JSON(miniTournaments)
}

// SubscribeToTournament adds user to tournament with full payment & waiver tracking
func (s *TournamentService) SubscribeToTournament(c *fiber.Ctx) error {
	type Req struct {
		ExternalUserID string `json:"external_user_id" validate:"required,uuid"` // UUID from Profile Service
		UserName       string `json:"user_name" validate:"required"`
		UserAvatarURL  string `json:"user_avatar_url,omitempty"`

		// --- Optional Waiver ---
		WaiverCode   string  `json:"waiver_code,omitempty"`   // e.g., "WELCOME10"
		WaiverAmount float64 `json:"waiver_amount,omitempty"` // user-selected amount to apply (optional, defaults to remaining)

		// --- Payment Details ---
		PaymentID     string  `json:"payment_id,omitempty"`
		PaymentAmount float64 `json:"payment_amount,omitempty"`
		PaymentStatus string  `json:"payment_status" validate:"oneof=pending paid failed refunded waived"`
		TransactionID string  `json:"transaction_id,omitempty"`
		PaymentMethod string  `json:"payment_method,omitempty"`
	}

	tournamentID := c.Params("id")
	if tournamentID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "tournament_id required in URL"})
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}

	// Validate required
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

	// 🔑 Get or create LOCAL TournamentUser (decoupled from profile service)
	var tUser models.TournamentUser
	if err := s.DB.Where("external_user_id = ?", req.ExternalUserID).
		First(&tUser).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create minimal local user record
			var profilePicURL *string
			if req.UserAvatarURL != "" {
				profilePicURL = &req.UserAvatarURL
			} else {
				profilePicURL = nil
			}

			tUser = models.TournamentUser{
				ID:                uuid.NewString(), // Explicitly generate ID
				ExternalUserID:    req.ExternalUserID,
				Username:          req.UserName,
				ProfilePictureURL: profilePicURL,
				Email:             "", // populate later via webhook if needed
			}
			if err := s.DB.Create(&tUser).Error; err != nil {
				return c.Status(500).JSON(fiber.Map{"error": "failed to create tournament user"})
			}
		} else {
			return c.Status(500).JSON(fiber.Map{"error": "DB error fetching user"})
		}
	}

	// 🔑 WAIVER LOGIC — compute effective fee BEFORE payment validation
	var effectiveEntryFee float64 = tournament.EntryFee
	var waiverToUse *models.UserWaiver // Store waiver details for later update
	var amountToApply float64 = 0.0    // Store the amount to apply from the waiver

	if req.WaiverCode != "" {
		if !tournament.AcceptsWaivers {
			return c.Status(403).JSON(fiber.Map{"error": "this tournament does not accept waivers"})
		}

		// 🔧 Fetch the specific waiver by code and user ID, regardless of current used_amount vs amount
		codeUpper := strings.ToUpper(req.WaiverCode)
		var w models.UserWaiver
		if err := s.DB.Where("user_id = ? AND UPPER(code) = ?", tUser.ID, codeUpper).First(&w).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return c.Status(400).JSON(fiber.Map{"error": "waiver code not found or not owned by user"})
			}
			return c.Status(500).JSON(fiber.Map{"error": "DB error fetching waiver", "details": err.Error()})
		}

		// Validate waiver state (active, not expired)
		if !w.IsActive {
			return c.Status(400).JSON(fiber.Map{"error": "waiver is not active"})
		}
		if w.ExpiresAt != nil && w.ExpiresAt.Before(time.Now()) {
			return c.Status(400).JSON(fiber.Map{"error": "waiver has expired"})
		}

		// Calculate usable amount
		remaining := w.Amount - w.UsedAmount
		if remaining <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "waiver is fully used and has no remaining balance"})
		}

		// Determine how much to apply
		amountToApply = req.WaiverAmount
		if amountToApply <= 0 {
			amountToApply = remaining // Use full remaining amount if not specified
		}
		if amountToApply > remaining {
			amountToApply = remaining // Cap at remaining amount
		}

		waiverToUse = &w // Store the waiver object for the transaction later
		effectiveEntryFee = tournament.EntryFee - amountToApply
		if effectiveEntryFee < 0 {
			effectiveEntryFee = 0 // Ensure effective fee doesn't go negative
		}
		// Log for debugging
		log.Printf("Applying waiver %s: $%.2f (remaining $%.2f), effective fee: $%.2f", w.Code, amountToApply, remaining, effectiveEntryFee)
	}

	// Check already subscribed (by ExternalUserID for consistency)
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

	// 🔐 Payment validation - now based on effectiveEntryFee
	paymentAmount := req.PaymentAmount
	paymentID := req.PaymentID
	transactionID := req.TransactionID
	paymentMethod := req.PaymentMethod
	var paymentAt *time.Time

	switch req.PaymentStatus {
	case "waived":
		// If effective fee is 0 due to waiver, this is valid.
		// If effective fee > 0, "waived" status is invalid.
		if effectiveEntryFee > 0 {
			return c.Status(400).JSON(fiber.Map{"error": "payment_status 'waived' invalid when effective fee > 0"})
		}
		paymentAmount = 0.0
		if paymentID == "" {
			paymentID = "waived-" + uuid.NewString()
		}
	case "paid":
		// Payment amount should match the effective fee
		if paymentAmount <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "payment_amount must be > 0 for 'paid'"})
		}
		if paymentAmount != effectiveEntryFee {
			// This check might be too strict if partial payments are allowed later.
			// For now, assume full effective fee must be paid if status is 'paid'.
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
		// paymentAmount might be set to effective fee, or handled later
		if paymentAmount != effectiveEntryFee {
			// Log or adjust if necessary, depending on business logic for pending payments
			log.Printf("Warning: payment_amount ($%.2f) does not match effective fee ($%.2f) for 'pending'. Using effective fee.", paymentAmount, effectiveEntryFee)
			paymentAmount = effectiveEntryFee // Adjust to effective fee if needed
		}
	}

	// ✅ Create subscription — use LOCAL TournamentUserID + denormalized data
	var subUserAvatarURL *string
	if req.UserAvatarURL != "" {
		subUserAvatarURL = &req.UserAvatarURL
	} else {
		subUserAvatarURL = nil
	}

	sub := models.TournamentSubscription{
		ID:               uuid.NewString(),
		TournamentID:     tournamentID,
		TournamentUserID: tUser.ID,             // ✅ Local FK
		ExternalUserID:   tUser.ExternalUserID, // ✅ Denormalized for audit
		UserName:         tUser.Username,       // ✅ Denormalized (safe copy at join time)
		UserAvatarURL:    subUserAvatarURL,     // ✅ Assign the *string pointer
		JoinedAt:         time.Now(),

		PaymentID:     paymentID,
		PaymentAmount: paymentAmount, // Use the effective amount (could be 0 if fully waived)
		PaymentStatus: req.PaymentStatus,
		TransactionID: transactionID,
		PaymentMethod: paymentMethod,
		PaymentAt:     paymentAt,

		// Waiver details (if applicable)
		WaiverCodeUsed:   req.WaiverCode, // Store the code used
		WaiverAmountUsed: amountToApply,  // Store the amount *applied* from the waiver
		// WaiverIDUsed will be set in the transaction if the waiver is actually used
	}

	// 🔁 Atomic Waiver Update (if applicable) and Subscription Creation
	if waiverToUse != nil && amountToApply > 0 {
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			// Re-fetch the waiver within the transaction with locking
			var wLocked models.UserWaiver
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", waiverToUse.ID).
				First(&wLocked).Error; err != nil {
				return fmt.Errorf("failed to lock waiver for update: %w", err)
			}

			// Double-check remaining balance within the transaction
			remainingInTx := wLocked.Amount - wLocked.UsedAmount
			if remainingInTx < amountToApply {
				// The amount to apply might have exceeded the balance available at transaction start
				// due to concurrent updates. Handle this case.
				return fmt.Errorf("insufficient waiver balance at transaction time (have $%.2f, need $%.2f)", remainingInTx, amountToApply)
			}

			newUsed := wLocked.UsedAmount + amountToApply
			if newUsed > wLocked.Amount {
				return fmt.Errorf("calculated used amount exceeds waiver total")
			}

			// Update the waiver's used amount
			if err := tx.Model(&wLocked).
				Where("id = ?", wLocked.ID).
				Updates(map[string]interface{}{
					"used_amount": newUsed,
				}).Error; err != nil {
				return fmt.Errorf("failed to update waiver used amount: %w", err)
			}

			// Set the waiver ID used in the subscription record
			sub.WaiverIDUsed = wLocked.ID // Use the ID from the locked waiver within the transaction

			// Create the subscription record
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
		// If no waiver was used (or amountToApply was 0), just create the subscription directly
		// outside the transaction to avoid unnecessary locking.
		if err := s.DB.Create(&sub).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to create subscription", "details": err.Error()})
		}
	}

	// ...

	// Return safe response (don't expose internal IDs like TournamentUserID)
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
			"payment_amount":     sub.PaymentAmount, // This reflects the effective fee paid
			"waiver_code_used":   sub.WaiverCodeUsed,
			"waiver_amount_used": sub.WaiverAmountUsed,
		},
	})
}


// In services/tournament_service.go
func (s *TournamentService) SuspendSubscription(c *fiber.Ctx) error {
    tournamentID := c.Params("tournament_id")
    userID := c.Params("user_id") // Or external_user_id depending on your path structure

    type Req struct {
        Reason string `json:"reason"`
    }
    var req Req
    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
    }

    // Find the subscription
    var sub models.TournamentSubscription
    if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID).First(&sub).Error; err != nil {
        if err == gorm.ErrRecordNotFound {
            return c.Status(404).JSON(fiber.Map{"error": "subscription not found"})
        }
        return c.Status(500).JSON(fiber.Map{"error": "DB error"})
    }

    // Update status and reason (add reason field to your model if needed)
    now := time.Now()
    updates := map[string]interface{}{
        "payment_status": "suspended", // Or define a new status like "admin_suspended"
        "suspended_at":   &now, // Add a suspended_at field to your model if needed
        "suspended_reason": req.Reason, // Add a suspended_reason field to your model if needed
    }

    if err := s.DB.Model(&sub).Updates(updates).Error; err != nil {
        return c.Status(500).JSON(fiber.Map{"error": "suspend failed"})
    }

    return c.JSON(fiber.Map{"message": "subscription suspended", "subscription": sub})
}

// In services/tournament_service.go
func (s *TournamentService) RevokeSubscription(c *fiber.Ctx) error {
    tournamentID := c.Params("tournament_id")
    userID := c.Params("user_id") // External user ID from path

    type Req struct {
        Reason string `json:"reason"`
    }
    var req Req
    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
    }

    // Find the subscription
    var sub models.TournamentSubscription
    if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID).First(&sub).Error; err != nil {
        if err == gorm.ErrRecordNotFound {
            return c.Status(404).JSON(fiber.Map{"error": "subscription not found"})
        }
        return c.Status(500).JSON(fiber.Map{"error": "DB error"})
    }

    // Example logic (simplified):
    if sub.PaymentStatus == "paid" {
        
        log.Printf("INFO: Subscription for user %s in tournament %s was paid ($%.2f). Refund logic needed.", userID, tournamentID, sub.PaymentAmount)
        // You might update a field like 'refund_initiated_at' or 'refund_status' here if tracking separately.
    } else if sub.WaiverCodeUsed != "" && sub.WaiverAmountUsed > 0 {
      
        log.Printf("INFO: Subscription for user %s in tournament %s used waiver '%s' ($%.2f). Waiver benefit lost.", userID, tournamentID, sub.WaiverCodeUsed, sub.WaiverAmountUsed)
    }

    // 2. Update subscription status to 'revoked' and add reason
    now := time.Now()
    updates := map[string]interface{}{
        "payment_status": "revoked", // Define a 'revoked' status
        "revoked_at":     &now,      // Add a revoked_at field to your model if needed
        "revoked_reason": req.Reason, // Add a revoked_reason field to your model if needed
    }

    if err := s.DB.Model(&sub).Updates(updates).Error; err != nil {
        return c.Status(500).JSON(fiber.Map{"error": "revoke failed"})
    }

    return c.JSON(fiber.Map{"message": "subscription revoked", "subscription": sub})
}

// getUserAvailableWaivers returns active, unexpired, non-exhausted waivers for a user.
// This function is still useful for other endpoints (like listing available waivers).
func (s *TournamentService) getUserAvailableWaivers(userID string) ([]models.UserWaiver, error) {
	var waivers []models.UserWaiver
	now := time.Now()
	query := s.DB.Where("user_id = ? AND is_active = true AND used_amount < amount", userID)
	// Exclude expired (if ExpiresAt is set)
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
		RefundedBy   string `json:"refunded_by,omitempty"` // admin/user ID
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
	if err := s.DB.Where("tournament_id = ? AND user_id = ?", tournamentID, userID).
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
		"payment_at":     now, // or keep original? We overwrite to reflect refund time
		// Optional: store metadata in a separate table or as JSON string
	}

	if err := s.DB.Model(&sub).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "refund failed"})
	}

	// Re-fetch for response
	s.DB.First(&sub, "id = ?", sub.ID)

	return c.JSON(fiber.Map{
		"message":      "refund processed",
		"subscription": sub,
	})
}

func (s *TournamentService) CreateBatch(c *fiber.Ctx) error {
	type Req struct {
		Name        string `json:"name" validate:"required"`
		Description string `json:"description"`
		Order       int    `json:"order"`
		StartDate   string `json:"start_date"` // RFC3339
		EndDate     string `json:"end_date"`
	}

	tournamentID := c.Params("id")

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
		Order:        req.Order,
		StartDate:    startDate,
		EndDate:      endDate,
	}

	if err := s.DB.Create(batch).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to create batch"})
	}

	return c.Status(201).JSON(batch)
}

func (s *TournamentService) CreateRound(c *fiber.Ctx) error {
	type Req struct {
		Name         string `json:"name" validate:"required"`
		Description  string `json:"description"`
		Order        int    `json:"order"`
		StartDate    string `json:"start_date" validate:"required"`
		EndDate      string `json:"end_date" validate:"required"`
		DurationMins int    `json:"duration_mins"`
		Status       string `json:"status"`
		ScoreType    string `json:"score_type"` // "highest", "sum", etc.
		Attempts     int    `json:"attempts"`
	}

	batchID := c.Params("batch_id")

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	// Fetch batch to get tournament ID
	var batch models.TournamentBatch
	if err := s.DB.First(&batch, "id = ?", batchID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "batch not found"})
	}

	startDate, err := time.Parse(time.RFC3339, req.StartDate)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid start_date"})
	}
	endDate, err := time.Parse(time.RFC3339, req.EndDate)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid end_date"})
	}

	// Validate logic: end ≥ start
	if !endDate.After(startDate) {
		return c.Status(400).JSON(fiber.Map{"error": "end_date must be after start_date"})
	}

	round := &models.TournamentRound{
		ID:           uuid.NewString(),
		BatchID:      batchID,
		TournamentID: batch.TournamentID, // ✅ denormalized
		Name:         req.Name,
		Description:  req.Description,
		Order:        req.Order,
		StartDate:    startDate,
		EndDate:      endDate,
		DurationMins: req.DurationMins,
		Status:       "pending",
		ScoreType:    req.ScoreType,
		Attempts:     req.Attempts,
	}

	if round.ScoreType == "" {
		round.ScoreType = "highest" // default
	}

	if err := s.DB.Create(round).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to create round"})
	}

	return c.Status(201).JSON(round)
}

func (s *TournamentService) GetTournamentByID(c *fiber.Ctx) error {
	id := c.Params("id")

	var tournament models.Tournament
	err := s.DB.Preload("Game").
		Preload("Photos").
		Preload("Batches.Rounds"). // ✅ Nested preload
		Preload("Subscriptions").
		First(&tournament, "id = ?", id).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}

	return c.JSON(tournament)
}

func (s *TournamentService) UpdateTournament(c *fiber.Ctx) error {
	id := c.Params("id")

	// Fetch existing
	var existing models.Tournament
	if err := s.DB.Preload("Photos").First(&existing, "id = ?", id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
	}

	// Parse form
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
	// --- NEW: Parse Prize Pool, Requirements, Sponsor Name, Is Featured, Publish Schedule for Update ---
	prizePool := c.FormValue("prize_pool")
	requirementsStr := c.FormValue("requirements") // This is newline-separated string from frontend
	sponsorName := c.FormValue("sponsor_name")
	isFeaturedStr := c.FormValue("is_featured")
	publishScheduleStr := c.FormValue("publish_schedule") // Expected format: RFC3339

	// CHANGED: Store requirements as the raw newline-separated string
	processedRequirements := requirementsStr // Assign the string directly

	// Process IsFeatured (assuming it's sent as "true"/"false" string from form)
	isFeatured := false
	if strings.ToLower(isFeaturedStr) == "true" {
		isFeatured = true
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
	// --- END NEW ---

	// Validate required
	if gameID == "" || name == "" || startTimeStr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "game_id, name, start_time required"})
	}

	// Parse numbers
	maxSubscribers := 0
	if maxSubStr != "" {
		if n, err := strconv.Atoi(maxSubStr); err != nil || n < 0 { // `n` and `err` defined here
			return c.Status(400).JSON(fiber.Map{"error": "invalid max_subscribers"})
		} else {
			maxSubscribers = n // Use `n` here
		}
	}

	entryFee := 0.0
	if entryFeeStr != "" {
		if f, err := strconv.ParseFloat(entryFeeStr, 64); err != nil || f < 0 { // `f` and `err` defined here
			return c.Status(400).JSON(fiber.Map{"error": "invalid entry_fee"})
		} else {
			entryFee = f // Use `f` here
		}
	}

	// Parse times
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid start_time"})
	}

	var endTime time.Time
	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid end_time"})
		}
	}

	// Verify game exists
	if err := s.DB.First(&models.Game{}, "id = ?", gameID).Error; err != nil { // `err` shadowed again
		return c.Status(400).JSON(fiber.Map{"error": "game_id not found"})
	}

	// --- Handle photo updates (replace all) ---
	var newPhotos []models.TournamentPhoto

	// Main photo
	if mainPhoto, err := c.FormFile("main_photo"); err == nil && mainPhoto.Size > 0 { // `mainPhoto` and `err` new
		ext := filepath.Ext(mainPhoto.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		key := "tournaments/main/" + uuid.NewString() + ext
		url, err := utils.UploadFileToR2(mainPhoto, key) // `url` and `err` new
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "main photo upload failed"})
		}
		existing.MainPhotoURL = url
	}

	// Secondary photos (replace all)
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("secondary_photos[%d]", i)
		if photo, err := c.FormFile(key); err == nil && photo.Size > 0 { // `photo` and `err` new
			ext := filepath.Ext(photo.Filename)
			if ext == "" {
				ext = ".jpg"
			}
			photoKey := "tournaments/photos/" + uuid.NewString() + ext
			url, err := utils.UploadFileToR2(photo, photoKey) // `url` and `err` new
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("photo %d upload failed", i+1)})
			}
			newPhotos = append(newPhotos, models.TournamentPhoto{
				ID:           uuid.NewString(),
				TournamentID: id,
				URL:          url,
				Order:        i,
			})
		}
	}

	// --- Update scalar fields ---
	existing.GameID = gameID
	existing.Name = name
	existing.Description = description
	existing.Rules = rules
	existing.Guidelines = guidelines
	existing.Genre = genre
	existing.MaxSubscribers = maxSubscribers
	existing.EntryFee = entryFee
	existing.StartTime = startTime
	existing.EndTime = endTime
	// --- NEW: Update Prize Pool, Requirements, Sponsor Name, Is Featured, Publish Schedule ---
	existing.PrizePool = prizePool
	existing.Requirements = processedRequirements // Assign the processed string
	existing.SponsorName = sponsorName
	existing.IsFeatured = isFeatured
	existing.PublishSchedule = publishSchedule
	// --- END NEW ---

	// --- Transaction: replace photos, save tournament ---
	txErr := s.DB.Transaction(func(tx *gorm.DB) error { // Use different variable name for transaction error
		// Delete old photos
		if err := tx.Where("tournament_id = ?", id).Delete(&models.TournamentPhoto{}).Error; err != nil {
			return err
		}
		// Insert new ones
		if len(newPhotos) > 0 {
			if err := tx.Create(&newPhotos).Error; err != nil {
				return err
			}
		}
		existing.Photos = newPhotos

		// Save tournament
		return tx.Save(&existing).Error
	})

	if txErr != nil { // Use the transaction error variable
		return c.Status(500).JSON(fiber.Map{"error": "update failed"})
	}

	// Reload with associations
	s.DB.Preload("Photos").Preload("Batches.Rounds").First(&existing, "id = ?", id)
	return c.JSON(existing)
}

func (s *TournamentService) DeleteTournament(c *fiber.Ctx) error {
	id := c.Params("id")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Delete in correct order to respect foreign key constraints:
		// 1. TournamentRound references TournamentBatch, so delete rounds first
		if err := tx.Where("tournament_id = ?", id).Delete(&models.TournamentRound{}).Error; err != nil {
			return err
		}

		// 2. Now safe to delete batches
		if err := tx.Where("tournament_id = ?", id).Delete(&models.TournamentBatch{}).Error; err != nil {
			return err
		}

		// 3. Delete other dependent tables
		if err := tx.Where("tournament_id = ?", id).Delete(&models.TournamentPhoto{}).Error; err != nil {
			return err
		}

		if err := tx.Where("tournament_id = ?", id).Delete(&models.TournamentSubscription{}).Error; err != nil {
			return err
		}

		if err := tx.Where("tournament_id = ?", id).Delete(&models.LeaderboardEntry{}).Error; err != nil {
			return err
		}

		// 4. Finally delete the tournament itself
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
		Status string `json:"status" validate:"oneof=draft published active completed cancelled publish unpublish"` // Include 'publish' and 'unpublish' actions
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	// Treat 'publish' and 'unpublish' as special actions
	var updates map[string]interface{}
	switch req.Status {
	case "publish":
		// Fetch the tournament to check schedule
		var tournament models.Tournament
		if err := s.DB.First(&tournament, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
			}
			return c.Status(500).JSON(fiber.Map{"error": "DB error"})
		}

		// Determine final status and published time based on schedule
		finalStatus := "published"
		publishedAt := time.Now()
		if tournament.PublishSchedule != nil {
			if tournament.PublishSchedule.Before(time.Now()) {
				finalStatus = "active"
				publishedAt = *tournament.PublishSchedule // Use the scheduled time if it was in the past
			} else {
				// Scheduled time is in the future, keep as 'published' for now
				finalStatus = "published"
				publishedAt = *tournament.PublishSchedule
			}
		} else {
			// No schedule, go active immediately
			finalStatus = "active"
		}

		updates = map[string]interface{}{
			"status":       finalStatus,
			"published_at": publishedAt,
		}
	case "unpublish":
		// Revert to draft and clear published_at
		updates = map[string]interface{}{
			"status":       "draft",
			"published_at": nil,
		}
	case "draft", "published", "active", "completed", "cancelled":
		// Direct status update
		updates = map[string]interface{}{
			"status": req.Status,
		}
	default:
		return c.Status(400).JSON(fiber.Map{"error": "invalid status"})
	}

	result := s.DB.Model(&models.Tournament{}).
		Where("id = ?", id).
		Updates(updates) // Use Updates for multiple fields

	if result.Error != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB update failed"})
	}
	if result.RowsAffected == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
	}

	// Return updated tournament
	var updated models.Tournament
	s.DB.First(&updated, "id = ?", id)
	return c.JSON(updated)
}

func (s *TournamentService) UpdateBatch(c *fiber.Ctx) error {
	id := c.Params("id")

	type Req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Order       int    `json:"order"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
	}

	var req Req
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON"})
	}

	var batch models.TournamentBatch
	if err := s.DB.First(&batch, "id = ?", id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "batch not found"})
	}

	// Parse dates
	if req.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, req.StartDate); err == nil {
			batch.StartDate = t
		}
	}
	if req.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, req.EndDate); err == nil {
			batch.EndDate = t
		}
	}

	batch.Name = req.Name
	batch.Description = req.Description
	batch.Order = req.Order

	if err := s.DB.Save(&batch).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "update failed"})
	}
	return c.JSON(batch)
}

func (s *TournamentService) DeleteBatch(c *fiber.Ctx) error {
	id := c.Params("id")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Delete rounds first
		tx.Where("batch_id = ?", id).Delete(&models.TournamentRound{})
		// Then batch
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

func (s *TournamentService) UpdateRound(c *fiber.Ctx) error {
	id := c.Params("id")

	type Req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		Order        int    `json:"order"`
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

	// Parse & validate dates
	if req.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, req.StartDate); err == nil {
			round.StartDate = t
		}
	}
	if req.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, req.EndDate); err == nil {
			round.EndDate = t
		}
	}
	if !round.EndDate.After(round.StartDate) && !round.EndDate.IsZero() {
		return c.Status(400).JSON(fiber.Map{"error": "end_date must be after start_date"})
	}

	round.Name = req.Name
	round.Description = req.Description
	round.Order = req.Order
	round.DurationMins = req.DurationMins
	round.Status = req.Status
	round.ScoreType = req.ScoreType
	round.Attempts = req.Attempts

	if err := s.DB.Save(&round).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "update failed"})
	}
	return c.JSON(round)
}

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

// CreateWaiver issues a new reusable waiver (coupon) for a user.
func (s *TournamentService) CreateWaiver(c *fiber.Ctx) error {
	var req CreateWaiverRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid JSON",
			"details": err.Error(),
		})
	}

	// Validate code
	if req.Code == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "code is required",
		})
	}

	// Normalize code: trim + uppercase
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if len(code) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "code cannot be empty after trimming",
		})
	}
	if len(code) > 64 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "code too long (max 64 characters)",
		})
	}

	// Ensure code uniqueness
	var count int64
	if err := s.DB.Model(&models.UserWaiver{}).
		Where("code = ?", code).
		Count(&count).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "database error checking code uniqueness",
			"details": err.Error(),
		})
	}
	if count > 0 {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "waiver code already exists",
		})
	}

	// Validate user exists in our LOCAL TournamentUser table
	// req.UserID should be the ExternalUserID
	var tournamentUser models.TournamentUser
	if err := s.DB.First(&tournamentUser, "external_user_id = ?", req.UserID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(400).JSON(fiber.Map{"error": "user_id not found in local tournament_users"})
		}
		log.Printf("DB error checking tournament user: %v", err) // Add logging
		return c.Status(500).JSON(fiber.Map{"error": "DB error checking user", "details": err.Error()})
	}

	// Safely extract IssuedByID (e.g., from admin middleware context)
	// This assumes the middleware sets c.Locals("admin_id") to the *TournamentUser.ID* of the admin
	var issuedByTournamentUserID string
	if local := c.Locals("admin_id"); local != nil {
		if id, ok := local.(string); ok && id != "" {
			// Validate that the admin ID is a valid TournamentUser.ID
			var adminTournamentUser models.TournamentUser
			if err := s.DB.First(&adminTournamentUser, "id = ?", id).Error; err != nil {
				log.Printf("Admin ID validation failed: %v", err) // Add logging
				return c.Status(500).JSON(fiber.Map{"error": "Invalid admin context"})
			}
			issuedByTournamentUserID = id
		}
	}

	// Fallback: if no admin context, assume user self-issued (e.g., promo redemption by the user themselves)
	// In this case, IssuedByID would be the ID of the *beneficiary* user.
	if issuedByTournamentUserID == "" {
		// If self-issuance is not allowed, return an error here:
		// return c.Status(403).JSON(fiber.Map{"error": "Admin privileges required to issue waivers"})
		// Otherwise, use the beneficiary's TournamentUser.ID
		issuedByTournamentUserID = tournamentUser.ID // Use the *local* ID of the user receiving the waiver
	}

	// Create waiver
	// Use the *local* TournamentUser.ID for the UserWaiver.UserID
	waiver := &models.UserWaiver{
		ID:          uuid.NewString(),  // Generate new UUID for the waiver
		UserID:      tournamentUser.ID, // ✅ Link to the *local* TournamentUser.ID
		Code:        code,
		Amount:      req.Amount,
		UsedAmount:  0.0,
		Description: req.Description,
		IsActive:    true,
		CreatedAt:   time.Now(),
		ExpiresAt:   req.ExpiresAt,
		IssuedByID:  issuedByTournamentUserID, // ✅ Store the *local* TournamentUser.ID of the issuer
	}

	if err := s.DB.Create(waiver).Error; err != nil {
		log.Printf("DB error creating waiver: %v", err) // Add logging
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "failed to create waiver",
			"details": err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(waiver)
}

// RedeemWaiver allows a user to claim part of a waiver (e.g., before payment).
// Returns the effective amount applied and updated waiver.
func (s *TournamentService) RedeemWaiver(c *fiber.Ctx) error {
	var req RedeemWaiverRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}

	// Fetch tournament to check waiver eligibility
	var tournament models.Tournament
	if err := s.DB.Select("id, accepts_waivers").First(&tournament, "id = ?", req.TournamentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "tournament not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error fetching tournament"})
	}

	if !tournament.AcceptsWaivers {
		return c.Status(403).JSON(fiber.Map{"error": "this tournament does not accept waivers"})
	}

	// Fetch waiver (case-insensitive code match)
	var waiver models.UserWaiver
	codeUpper := strings.ToUpper(req.WaiverCode)
	if err := s.DB.Where("user_id = ? AND UPPER(code) = ? AND is_active = true", req.UserID, codeUpper).
		First(&waiver).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "waiver not found or not owned by user"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}

	// Check expiration
	if waiver.ExpiresAt != nil && waiver.ExpiresAt.Before(time.Now()) {
		return c.Status(400).JSON(fiber.Map{"error": "waiver has expired"})
	}

	// Check remaining balance
	remaining := waiver.Amount - waiver.UsedAmount
	if remaining <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "waiver is fully used"})
	}

	// Clamp amount to remaining & to requested
	amountToApply := math.Min(req.AmountToUse, remaining)

	// 🔁 Atomic update
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// Re-fetch for concurrency safety (optional: use SELECT FOR UPDATE)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", waiver.ID).
			First(&waiver).Error; err != nil {
			return err
		}

		newUsed := waiver.UsedAmount + amountToApply
		if newUsed > waiver.Amount {
			return fmt.Errorf("overspent waiver")
		}

		if err := tx.Model(&waiver).
			Where("id = ?", waiver.ID).
			Updates(map[string]interface{}{
				"used_amount": newUsed,
			}).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to redeem waiver", "details": err.Error()})
	}

	// Refresh waiver
	s.DB.First(&waiver, "id = ?", waiver.ID)

	return c.JSON(fiber.Map{
		"message":        "waiver redeemed successfully",
		"waiver_code":    waiver.Code,
		"amount_applied": amountToApply,
		"remaining":      waiver.Amount - waiver.UsedAmount,
		"waiver":         waiver,
	})
}

// GetUserAvailableWaiversEndpoint returns all active, unexpired, non-exhausted waivers for a user.
// GET /users/:user_id/waivers/available
func (s *TournamentService) GetUserAvailableWaiversEndpoint(c *fiber.Ctx) error {
	userID := c.Params("user_id")
	if userID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "user_id is required in path"})
	}

	waivers, err := s.getUserAvailableWaivers(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error":   "failed to fetch available waivers",
			"details": err.Error(),
		})
	}

	// Format response
	type WaiverSummary struct {
		ID          string     `json:"id"`
		Code        string     `json:"code"`
		Amount      float64    `json:"amount"`
		UsedAmount  float64    `json:"used_amount"`
		Remaining   float64    `json:"remaining"`
		Description string     `json:"description,omitempty"`
		ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	}

	response := make([]WaiverSummary, len(waivers))
	for i, w := range waivers {
		response[i] = WaiverSummary{
			ID:          w.ID,
			Code:        w.Code,
			Amount:      w.Amount,
			UsedAmount:  w.UsedAmount,
			Remaining:   w.Amount - w.UsedAmount,
			Description: w.Description,
			ExpiresAt:   w.ExpiresAt,
		}
	}

	hasWaiver := len(response) > 0
	totalAvailable := 0.0
	for _, w := range response {
		totalAvailable += w.Remaining
	}

	return c.JSON(fiber.Map{
		"has_waiver":      hasWaiver,
		"total_available": totalAvailable,
		"count":           len(response),
		"waivers":         response,
	})
}

func (s *TournamentService) GetAllWaivers(c *fiber.Ctx) error {
	var waivers []models.UserWaiver
	if err := s.DB.Find(&waivers).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}
	return c.JSON(waivers)
}

func (s *TournamentService) UpdateWaiver(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id required"})
	}

	var req UpdateWaiverRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}

	// Prepare updates map, only include fields that are present in the request
	updates := make(map[string]interface{})
	if req.Code != nil {
		code := strings.ToUpper(strings.TrimSpace(*req.Code))
		if code == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "code cannot be empty"})
		}
		// Enforce uniqueness (excluding self)
		var count int64
		s.DB.Model(&models.UserWaiver{}).Where("code = ? AND id != ?", code, id).Count(&count)
		if count > 0 {
			return c.Status(409).JSON(fiber.Map{"error": "code already in use"})
		}
		updates["code"] = code
	}
	if req.Amount != nil {
		if *req.Amount <= 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "amount must be > 0"})
		}
		updates["amount"] = *req.Amount
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.IsActive != nil { // Handle IsActive toggle
		updates["is_active"] = *req.IsActive
	}
	if req.ExpiresAt != nil {
		updates["expires_at"] = *req.ExpiresAt
	}

	if len(updates) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no fields to update"})
	}

	// Perform the update
	if err := s.DB.Model(&models.UserWaiver{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "update failed", "details": err.Error()})
	}

	// Fetch the updated waiver to return
	var updated models.UserWaiver
	if err := s.DB.First(&updated, "id = ?", id).Error; err != nil {
		// This should ideally not happen if the update succeeded, but handle it
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch updated waiver", "details": err.Error()})
	}

	return c.JSON(updated)
}

func (s *TournamentService) DeleteWaiver(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "id required"})
	}

	result := s.DB.Delete(&models.UserWaiver{}, "id = ?", id)
	if result.Error != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}
	if result.RowsAffected == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "waiver not found"})
	}
	return c.JSON(fiber.Map{"message": "waiver deleted"})
}
