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
	requirementsStr := c.FormValue("requirements") // This is newline-separated string from frontend
	sponsorName := c.FormValue("sponsor_name")
	isFeaturedStr := c.FormValue("is_featured")
	publishScheduleStr := c.FormValue("publish_schedule") // Expected format: RFC3339
	acceptsWaiversStr := c.FormValue("accepts_waivers")

	// CHANGED: Store requirements as the raw newline-separated string
	processedRequirements := requirementsStr // Assign the string directly

	// Process IsFeatured (assuming it's sent as "true"/"false" string from form)
	isFeatured := false
	if strings.ToLower(isFeaturedStr) == "true" {
		isFeatured = true
	}

	// Process AcceptsWaivers
	acceptsWaivers := true // Default to true
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
				ID:           uuid.NewString(), // ✅ Unique ID for each photo
				URL:          url,
				SortOrder:    i, // ✅ Now correctly uses SortOrder
				TournamentID: "", // Will be set in transaction
			})
		} else {
			break // stop on first missing
		}
	}

	// --- Create tournament ---
	tournament := &models.Tournament{
		ID:              uuid.NewString(), // ✅ Ensure unique tournament ID
		GameID:          gameID,
		Name:            name,
		Description:     description,
		Rules:           rules,
		Guidelines:      guidelines,
		Genre:           genre,
		GenreTags:       c.FormValue("genre_tags"), // Added GenreTags
		MaxSubscribers:  maxSubscribers,
		EntryFee:        entryFee,
		MainPhotoURL:    mainPhotoURL,
		StartTime:       startTime,
		EndTime:         endTime,
		AcceptsWaivers:  acceptsWaivers, // Added AcceptsWaivers
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

	// Preload associations for response — ✅ FIX: ORDER BY "sort_order"
	s.DB.Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions").First(tournament, "id = ?", tournament.ID)
	return c.Status(201).JSON(tournament)
}

func (s *TournamentService) GetAllTournaments(c *fiber.Ctx) error {
	var tournaments []models.Tournament
	// Preload associations — ✅ FIX: ORDER BY "sort_order"
	err := s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions").
		Find(&tournaments).Error
	if err != nil {
		// Log the error for debugging
		log.Printf("ERROR fetching tournaments with preloads: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch tournaments"})
	}
	return c.JSON(tournaments)
}

// GetAllTournamentsMini returns a minimal list of tournament details, including the associated game.
func (s *TournamentService) GetAllTournamentsMini(c *fiber.Ctx) error {
	type TournamentMini struct {
		ID              string     `json:"id"`
		Name            string     `json:"name"`
		Status          string     `json:"status"`
		StartTime       time.Time  `json:"start_time"`
		EndTime         time.Time  `json:"end_time"` // Added this field
		MainPhotoURL    string     `json:"main_photo_url"`
		EntryFee        float64    `json:"entry_fee"`
		PrizePool       string     `json:"prize_pool"`
		SponsorName     string     `json:"sponsor_name"`
		IsFeatured      bool       `json:"is_featured"`
		PublishedAt     *time.Time `json:"published_at,omitempty"`
		GameID          string     `json:"game_id"`
		GameName        string     `json:"game_name"`
		GameLogoURL     string     `json:"game_logo_url,omitempty"`
		MaxSubscribers  int        `json:"max_subscribers"`
		SubscribersCount int64     `json:"subscribers_count"`
		Genre           string     `json:"genre,omitempty"`
		Description     string     `json:"description,omitempty"`
		CreatedAt       time.Time  `json:"created_at"`
		UpdatedAt       time.Time  `json:"updated_at"`
		// Add other fields that might be needed
		Requirements    string     `json:"requirements,omitempty"`
		Rules           string     `json:"rules,omitempty"`
		Guidelines      string     `json:"guidelines,omitempty"`
		AcceptsWaivers  bool       `json:"accepts_waivers"`
		PublishSchedule *time.Time `json:"publish_schedule,omitempty"`
	}
	var tournaments []TournamentMini
	// Updated query to include end_time and other missing fields
	// ⚠️ Note: This query does *not* require ordering by sort_order — it's just a list by created_at.
	// But if you want to sort by sort_order of batches/rounds later, that's in full fetch, not mini.
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
		if err := s.DB.Where("user_id = ? AND UPPER(code) = ?", req.ExternalUserID, codeUpper).First(&w).Error; err != nil { // ✅ Changed: user_id is ExternalUserID
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
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, req.ExternalUserID). // ✅ Changed: use ExternalUserID
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

	// ✅ Create subscription — use ExternalUserID directly
	var subUserAvatarURL *string
	if req.UserAvatarURL != "" {
		subUserAvatarURL = &req.UserAvatarURL
	} else {
		subUserAvatarURL = nil
	}
	sub := models.TournamentSubscription{
		ID:               uuid.NewString(),
		TournamentID:     tournamentID,
		// Removed: TournamentUserID
		ExternalUserID: req.ExternalUserID, // ✅ Use ExternalUserID as the primary key
		UserName:       req.UserName,       // ✅ Denormalized (safe copy at join time)
		UserAvatarURL:  subUserAvatarURL,   // ✅ Assign the *string pointer
		JoinedAt:       time.Now(),
		PaymentID:      paymentID,
		PaymentAmount:  paymentAmount, // Use the effective amount (could be 0 if fully waived)
		PaymentStatus:  req.PaymentStatus,
		TransactionID:  transactionID,
		PaymentMethod:  paymentMethod,
		PaymentAt:      paymentAt,
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
					"is_redeemed": true, // Mark as redeemed when used
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
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID).First(&sub).Error; err != nil { // ✅ Changed: use ExternalUserID
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "subscription not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "DB error"})
	}
	// Update status and reason (add reason field to your model if needed)
	now := time.Now()
	updates := map[string]interface{}{
		"payment_status":   "suspended", // Or define a new status like "admin_suspended"
		"suspended_at":     &now,        // Add a suspended_at field to your model if needed
		"suspended_reason": req.Reason,  // Add a suspended_reason field to your model if needed
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
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID).First(&sub).Error; err != nil { // ✅ Changed: use ExternalUserID
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
		"payment_status": "revoked",  // Define a 'revoked' status
		"revoked_at":     &now,       // Add a revoked_at field to your model if needed
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
	query := s.DB.Where("user_id = ? AND is_active = true AND used_amount < amount", userID) // ✅ Changed: user_id is ExternalUserID
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
	if err := s.DB.Where("tournament_id = ? AND external_user_id = ?", tournamentID, userID). // ✅ Changed: use ExternalUserID
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
	// Removed: Preload("TournamentUser") as it no longer exists
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
		SortOrder   int    `json:"sort_order"` // ✅ Use SortOrder
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
		SortOrder:    req.SortOrder, // ✅ Correct field
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
		SortOrder    int    `json:"sort_order"` // ✅ Use SortOrder
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
		SortOrder:    req.SortOrder, // ✅ Correct field
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

// GetTournamentByID retrieves a tournament by ID along with its related data.
func (s *TournamentService) GetTournamentByID(c *fiber.Ctx) error {
	id := c.Params("id")
	var tournament models.Tournament

	// Preload all necessary associations to build the complete tournament object.
	// This includes Game, Photos ordered by sort_order, Batches ordered by sort_order,
	// and the Rounds within each Batch, also ordered by sort_order.
	err := s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			// ✅ FIX: Order by "sort_order" (quoted as it's a reserved keyword in PostgreSQL)
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			// ✅ FIX: Order by "sort_order"
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Rounds", func(db *gorm.DB) *gorm.DB {
			// ✅ FIX: Order Rounds by "sort_order" within each Batch
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions", func(db *gorm.DB) *gorm.DB {
			// Order subscriptions by join time, for example
			return db.Order("joined_at DESC")
		}).
		// Removed: Preload("Subscriptions.TournamentUser") as TournamentUser model was removed
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
		availableSlots = -1 // unlimited
	}

	// Set the calculated fields on the tournament object
	tournament.SubscribersCount = subsCount
	tournament.ActiveSubscribersCount = activeSubsCount
	tournament.AvailableSlots = availableSlots

	// Return the fully populated tournament object
	return c.JSON(tournament)
}

// UpdateTournament handles updating an existing tournament by ID.
func (s *TournamentService) UpdateTournament(c *fiber.Ctx) error {
	id := c.Params("id")
	var existingTournament models.Tournament
	if err := s.DB.Preload("Photos").First(&existingTournament, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tournament not found"})
		}
		log.Printf("DB Error fetching tournament %s: %v", id, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}
	// --- START: Validate and Parse start_time ---
	startTimeStr := c.FormValue("start_time")
	log.Printf("DEBUG: Received start_time string: '%s'", startTimeStr) // Add logging
	if startTimeStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "start_time is required and cannot be empty",
		})
	}
	// Attempt to parse the received string as RFC3339
	parsedStartTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		log.Printf("ERROR: Failed to parse start_time '%s': %v", startTimeStr, err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid start_time format. Must be RFC3339 (e.g., 2024-01-01T15:04:05Z)",
		})
	}
	// --- END: Validate and Parse start_time ---
	// --- START: Validate and Parse end_time (if provided) ---
	endTimeStr := c.FormValue("end_time")
	var parsedEndTime *time.Time // Use pointer to handle optional field
	if endTimeStr != "" {
		parsedET, err := time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			log.Printf("ERROR: Failed to parse end_time '%s': %v", endTimeStr, err)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid end_time format. Must be RFC3339 (e.g., 2024-01-01T15:04:05Z)",
			})
		}
		parsedEndTime = &parsedET // Assign the address of the parsed time
	}
	// --- END: Validate and Parse end_time ---
	// --- START: Validate and Parse publish_schedule (if provided) ---
	publishScheduleStr := c.FormValue("publish_schedule")
	var parsedPublishSchedule *time.Time
	if publishScheduleStr != "" {
		parsedPS, err := time.Parse(time.RFC3339, publishScheduleStr)
		if err != nil {
			log.Printf("ERROR: Failed to parse publish_schedule '%s': %v", publishScheduleStr, err)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid publish_schedule format. Must be RFC3339 (e.g., 2024-01-01T15:04:05Z)",
			})
		}
		parsedPublishSchedule = &parsedPS
	}
	// --- END: Validate and Parse publish_schedule ---
	// --- START: Prepare updates map ---
	updates := map[string]interface{}{
		"start_time":  parsedStartTime, // Use the parsed time object
		"name":        strings.TrimSpace(c.FormValue("name")),
		"genre":       strings.TrimSpace(c.FormValue("genre")),
		"genre_tags":  strings.TrimSpace(c.FormValue("genre_tags")), // Added GenreTags
		"description": c.FormValue("description"),
		"rules":       c.FormValue("rules"),
		"guidelines":  c.FormValue("guidelines"),
		"max_subscribers": func() int {
			if v := c.FormValue("max_subscribers"); v != "" {
				if val, err := strconv.Atoi(v); err == nil {
					return val
				}
			}
			return 0 // Default or handle error if needed
		}(),
		"entry_fee": func() float64 {
			if v := c.FormValue("entry_fee"); v != "" {
				if val, err := strconv.ParseFloat(v, 64); err == nil {
					return val
				}
			}
			return 0.0 // Default or handle error if needed
		}(),
		"prize_pool":      c.FormValue("prize_pool"),
		"requirements":    c.FormValue("requirements"),
		"sponsor_name":    c.FormValue("sponsor_name"),
		"is_featured":     c.FormValue("is_featured") == "true", // Handle boolean conversion
		"accepts_waivers": c.FormValue("accepts_waivers") == "true", // Handle boolean conversion for AcceptsWaivers
		"status":          c.FormValue("status"),                   // Consider validation if changing status here
	}
	// Conditionally add end_time and publish_schedule to updates if they were provided
	if parsedEndTime != nil {
		updates["end_time"] = *parsedEndTime
	}
	if parsedPublishSchedule != nil {
		updates["publish_schedule"] = *parsedPublishSchedule
	}

	var newPhotos []models.TournamentPhoto
	// 1. Handle Main Photo Replacement
	if mainPhotoFile, err := c.FormFile("main_photo"); err == nil && mainPhotoFile.Size > 0 {
		// Delete old main photo from storage if it existed
		if existingTournament.MainPhotoURL != "" {
			// Optionally delete from R2 here
		}
		ext := filepath.Ext(mainPhotoFile.Filename)
		if ext == "" {
			ext = ".jpg" // Default extension
		}
		key := fmt.Sprintf("tournaments/main/%s%s", uuid.NewString(), ext)
		url, err := utils.UploadFileToR2(mainPhotoFile, key)
		if err != nil {
			log.Printf("ERROR: Failed to upload new main photo for tournament %s: %v", id, err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to upload main photo"})
		}
		updates["main_photo_url"] = url // Update the main photo URL in the DB
	}

	// 2. Handle Secondary Photos Replacement
	for i := 0; ; i++ { // Loop through potential secondary photos
		key := fmt.Sprintf("photos[%d]", i)
		if photoFile, err := c.FormFile(key); err == nil && photoFile.Size > 0 {
			ext := filepath.Ext(photoFile.Filename)
			if ext == "" {
				ext = ".jpg"
			}
			photoKey := fmt.Sprintf("tournaments/secondary/%s%s", uuid.NewString(), ext)
			url, err := utils.UploadFileToR2(photoFile, photoKey)
			if err != nil {
				log.Printf("ERROR: Failed to upload secondary photo %d for tournament %s: %v", i, id, err)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Failed to upload secondary photo %d", i)})
			}
			newPhoto := models.TournamentPhoto{
				ID:           uuid.NewString(), // Generate new ID for new photos
				URL:          url,
				SortOrder:    i, // ✅ Now correctly uses SortOrder
				TournamentID: existingTournament.ID, // Link to the tournament
			}
			newPhotos = append(newPhotos, newPhoto)
		} else {
			// No more files found for this index, break the loop
			break
		}
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// Step 1: Update the main tournament record with non-photo fields
		if err := tx.Model(&existingTournament).Updates(updates).Error; err != nil {
			log.Printf("ERROR: Failed to update tournament %s: %v", id, err)
			return err
		}
		// Step 2: Handle Photo Updates (same as before)
		// 2a. Delete existing secondary photos from database and storage (optional: add soft-delete logic)
		var existingSecondaryPhotos []models.TournamentPhoto
		if err := tx.Where("tournament_id = ?", existingTournament.ID).Find(&existingSecondaryPhotos).Error; err != nil {
			log.Printf("ERROR: Failed to fetch existing secondary photos for deletion: %v", err)
			return err
		}
		for _, oldPhoto := range existingSecondaryPhotos {
			// Optional: Delete file from R2 here if needed
			// utils.DeleteFileFromR2(oldPhoto.URL)
			if err := tx.Delete(&oldPhoto).Error; err != nil {
				return err
			}
		}
		// 2b. Create new secondary photos in the database
		for _, photo := range newPhotos {
			if err := tx.Create(&photo).Error; err != nil {
				log.Printf("ERROR: Failed to create new secondary photo for tournament %s: %v", id, err)
				return err
			}
		}
		existingTournament.StartTime = parsedStartTime
		if parsedEndTime != nil {
			existingTournament.EndTime = *parsedEndTime
		} else {
			existingTournament.EndTime = time.Time{}
		}
		existingTournament.PublishSchedule = parsedPublishSchedule
		existingTournament.Photos = newPhotos
		return nil
	})
	if err != nil {
		log.Printf("ERROR: Transaction failed for updating tournament %s: %v", id, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update tournament"})
	}
	// --- END: Perform Atomic Update Transaction ---
	// Fetch the fully updated tournament with photos for the response — ✅ FIX: ORDER BY "sort_order"
	if err := s.DB.
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Game").
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions"). // Removed: Preload("Subscriptions.TournamentUser")
		First(&existingTournament, "id = ?", id).Error; err != nil {
		// This should ideally not happen if the transaction succeeded
		log.Printf("ERROR: Could not refetch updated tournament %s: %v", id, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve updated tournament"})
	}
	return c.JSON(existingTournament)
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
	// Return updated tournament — ✅ FIX: ORDER BY "sort_order"
	var updated models.Tournament
	s.DB.
		Preload("Game").
		Preload("Photos", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Batches.Rounds", func(db *gorm.DB) *gorm.DB {
			return db.Order("\"sort_order\" ASC")
		}).
		Preload("Subscriptions"). // Removed: Preload("Subscriptions.TournamentUser")
		First(&updated, "id = ?", id)
	return c.JSON(updated)
}

func (s *TournamentService) UpdateBatch(c *fiber.Ctx) error {
	id := c.Params("id")
	type Req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"` // ✅ Use SortOrder
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
	batch.SortOrder = req.SortOrder // ✅ Correct field
	if err := s.DB.Save(&batch).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "update failed"})
	}
	// Preload rounds for the response — ✅ FIX: ORDER BY "sort_order"
	s.DB.Preload("Rounds", func(db *gorm.DB) *gorm.DB {
		return db.Order("\"sort_order\" ASC")
	}).First(&batch, "id = ?", id)
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
		SortOrder    int    `json:"sort_order"` // ✅ Use SortOrder
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
	round.SortOrder = req.SortOrder // ✅ Correct field
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
	// Removed: Preload("TournamentUser") as it no longer exists
	if err := s.DB.Where("tournament_id = ?", tournamentID).
		Order("joined_at DESC").
		Find(&subs).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch subscribers"})
	}
	return c.JSON(subs)
}