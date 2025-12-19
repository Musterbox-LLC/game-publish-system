package services

import (
	"errors"
	"fmt"
	"game-publish-system/models"
	"log"
	"math"
	"strings"
	"time"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// --- Waiver-related request types ---
type CreateWaiverRequest struct {
	UserID        string     `json:"user_id" validate:"required"`
	Code          string     `json:"code" validate:"required"`
	Amount        float64    `json:"amount" validate:"required,gt=0"`
	Description   string     `json:"description,omitempty"`
	Title         string     `json:"title,omitempty"`
	Type          string     `json:"type,omitempty"`
	ImageURL      string     `json:"image_url,omitempty"`
	Emoji         string     `json:"emoji,omitempty"`
	Excerpt       string     `json:"excerpt,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"` // Hard expiry date
	DurationHours int        `json:"duration_hours,omitempty"` // Duration after first use
}

// UpdateWaiverRequest defines the structure for partial updates
type UpdateWaiverRequest struct {
	Code          *string    `json:"code,omitempty"`
	Amount        *float64   `json:"amount,omitempty"`
	Description   *string    `json:"description,omitempty"`
	IsActive      *bool      `json:"is_active,omitempty"` // Crucial for toggling
	ExpiresAt     *time.Time `json:"expires_at,omitempty"` // Hard expiry date
	Title         *string    `json:"title,omitempty"`
	Type          *string    `json:"type,omitempty"`
	ImageURL      *string    `json:"image_url,omitempty"`
	Emoji         *string    `json:"emoji,omitempty"`
	Excerpt       *string    `json:"excerpt,omitempty"`
	DurationHours *int       `json:"duration_hours,omitempty"` // Duration after first use
	// Note: IsRedeemed is typically not updated directly by admin
}

type RedeemWaiverRequest struct {
	UserID       string  `json:"user_id" validate:"required,uuid"`
	WaiverCode   string  `json:"waiver_code" validate:"required"`
	AmountToUse  float64 `json:"amount_to_use" validate:"required,gt=0"` // user chooses this
	TournamentID string  `json:"tournament_id" validate:"required,uuid"` // for audit + enforce tournament rules
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

	// Determine DurationHours (default 168 = 7 days)
	durationHours := 168
	if req.DurationHours > 0 {
		durationHours = req.DurationHours
	}

	// Create waiver
	// Use the *local* TournamentUser.ID for the UserWaiver.UserID
	waiver := &models.UserWaiver{
		ID:            uuid.NewString(),  // Generate new UUID for the waiver
		UserID:        tournamentUser.ID, // ✅ Link to the *local* TournamentUser.ID
		Code:          code,
		Amount:        req.Amount,
		UsedAmount:    0.0,
		Description:   req.Description,
		Title:         req.Title,
		Type:          req.Type,
		ImageURL:      req.ImageURL,
		Emoji:         req.Emoji,
		Excerpt:       req.Excerpt,
		IsActive:      true,
		IsViewed:      false,
		IsRedeemed:    false, // Not redeemed yet
		DurationHours: durationHours,
		CreatedAt:     time.Now(),
		ExpiresAt:     req.ExpiresAt, // Can be nil, set later on first use or as hard expiry
		IssuedByID:    issuedByTournamentUserID, // ✅ Store the *local* TournamentUser.ID of the issuer
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
	// Check hard expiration (original expiry date set during creation)
	if waiver.ExpiresAt != nil && waiver.ExpiresAt.Before(time.Now()) {
		return c.Status(400).JSON(fiber.Map{"error": "waiver has expired (hard expiry)"})
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

		// Determine if this is the first use
		isFirstUse := waiver.UsedAmount == 0 && amountToApply > 0
		updates := map[string]interface{}{
			"used_amount": newUsed,
		}

		// If first use, mark as redeemed and potentially set expiry based on duration
		if isFirstUse {
			updates["is_redeemed"] = true
			// If DurationHours is set and no expiry was set before (or it's still before hard expiry), set expiry from duration
			if waiver.DurationHours > 0 {
				// Calculate potential new expiry from duration
				newExpiryFromDuration := time.Now().Add(time.Duration(waiver.DurationHours) * time.Hour)
				// Check if the original hard expiry exists and is before the new calculated expiry
				if waiver.ExpiresAt == nil || newExpiryFromDuration.Before(*waiver.ExpiresAt) {
					// Use the calculated expiry from duration as it's earlier
					updates["expires_at"] = newExpiryFromDuration
				} else {
					// Keep the original hard expiry as it's still valid and later
					// No need to update ExpiresAt in this case, it's already correct or will be checked later
				}
			}
		}

		if err := tx.Model(&waiver).
			Where("id = ?", waiver.ID).
			Updates(updates).Error; err != nil {
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
	// Add updates for the new fields
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Type != nil {
		updates["type"] = *req.Type
	}
	if req.ImageURL != nil {
		updates["image_url"] = *req.ImageURL
	}
	if req.Emoji != nil {
		updates["emoji"] = *req.Emoji
	}
	if req.Excerpt != nil {
		updates["excerpt"] = *req.Excerpt
	}
	if req.DurationHours != nil {
		if *req.DurationHours < 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "duration_hours must be >= 0"})
		}
		updates["duration_hours"] = *req.DurationHours
	}
	// Note: IsRedeemed is typically not updated directly by admin

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

// GetUserWaiverCountsEndpoint returns the total count, count of unviewed, and count of unclaimed waivers for the authenticated user.
// This is the endpoint you can poll every 30 seconds.
func (s *TournamentService) GetUserWaiverCountsEndpoint(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	now := time.Now()

	// First, get the TournamentUser ID based on the external user ID
	var tournamentUser models.TournamentUser
	if err := s.DB.Where("external_user_id = ?", userID).First(&tournamentUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// User has no waivers
			return c.JSON(fiber.Map{
				"total_count":     0,
				"unviewed_count":  0,
				"unclaimed_count": 0,
			})
		}
		log.Printf("DB Error fetching TournamentUser for counts: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error"})
	}

	baseQuery := s.DB.Model(&models.UserWaiver{}).
		Where("user_id = ?", tournamentUser.ID).
		Where("is_active = ?", true).
		Where("(expires_at IS NULL OR expires_at >= ?)", now)

	// Count total valid waivers
	var totalCount int64
	if err := baseQuery.Count(&totalCount).Error; err != nil {
		log.Printf("DB Error counting total waivers: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error counting total waivers"})
	}

	// Count unviewed valid waivers
	var unviewedCount int64
	if err := baseQuery.
		Where("is_viewed = ?", false).
		Count(&unviewedCount).Error; err != nil {
		log.Printf("DB Error counting unviewed waivers: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error counting unviewed waivers"})
	}

	// Count unclaimed valid waivers
	var unclaimedCount int64
	if err := baseQuery.
		Where("is_claimed = ?", false). // Fixed: Use 'is_claimed' field name
		Count(&unclaimedCount).Error; err != nil {
		log.Printf("DB Error counting unclaimed waivers: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error counting unclaimed waivers"})
	}

	return c.JSON(fiber.Map{
		"total_count":     totalCount,
		"unviewed_count":  unviewedCount,
		"unclaimed_count": unclaimedCount, // Fixed: proper JSON key
	})
}

func (s *TournamentService) MarkAllWaiversAsViewed(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	now := time.Now()

	// Resolve TournamentUser from external user ID
	var tournamentUser models.TournamentUser
	if err := s.DB.
		Where("external_user_id = ?", userID).
		First(&tournamentUser).Error; err != nil {

		if errors.Is(err, gorm.ErrRecordNotFound) {
			// No waivers to mark
			return c.JSON(fiber.Map{
				"message":         "OK",
				"marked_count":    0,
				"total_unviewed":  0,
			})
		}

		log.Printf("DB Error fetching TournamentUser: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error"})
	}

	result := s.DB.Model(&models.UserWaiver{}).
		Where("user_id = ?", tournamentUser.ID).
		Where("is_viewed = ?", false).
		Where("is_active = ?", true).
		Where("(expires_at IS NULL OR expires_at >= ?)", now).
		Update("is_viewed", true)

	if result.Error != nil {
		log.Printf("Bulk mark waivers viewed failed: %v", result.Error)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "Failed to update waivers"})
	}

	return c.JSON(fiber.Map{
		"message":        "OK",
		"marked_count":   result.RowsAffected,
		"total_unviewed": 0,
	})
}



// MarkWaiverAsViewedEndpoint updates the IsViewed flag for a specific waiver owned by the user.
func (s *TournamentService) MarkWaiverAsViewedEndpoint(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string) // Assumes UserContextMiddleware sets this
	waiverID := c.Params("id")             // Waiver ID from URL path
	if waiverID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "waiver_id is required in path"})
	}
	// Get the TournamentUser ID based on the external user ID
	var tournamentUser models.TournamentUser
	if err := s.DB.Where("external_user_id = ?", userID).First(&tournamentUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found in tournament system"})
		}
		log.Printf("DB Error fetching TournamentUser for mark viewed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}
	// Find the specific waiver by ID and ensure it belongs to the user
	var waiver models.UserWaiver
	if err := s.DB.Where("id = ? AND user_id = ?", waiverID, tournamentUser.ID).First(&waiver).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Or return 404 if waiver doesn't exist or doesn't belong to user
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "waiver not found or does not belong to user"})
		}
		log.Printf("DB Error fetching waiver for mark viewed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}
	// Update the IsViewed flag to true
	updates := map[string]interface{}{
		"is_viewed":  true,
		"updated_at": time.Now(), // Optionally update the timestamp as well
	}
	if err := s.DB.Model(&waiver).Where("id = ?", waiverID).Updates(updates).Error; err != nil {
		log.Printf("DB Error updating waiver is_viewed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to mark waiver as viewed"})
	}

	// Optionally, return the updated waiver
	s.DB.First(&waiver, "id = ?", waiverID) // Reload to confirm update
	return c.JSON(fiber.Map{
		"message": "waiver marked as viewed",
		"waiver":  waiver,
	})
}

// MarkWaiverAsRedeemedEndpoint updates the IsRedeemed flag for a specific waiver owned by the user.
// This endpoint allows marking a waiver as fully used/redemeed.
func (s *TournamentService) MarkWaiverAsRedeemedEndpoint(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string) // Assumes UserContextMiddleware sets this
	waiverID := c.Params("id")             // Waiver ID from URL path
	if waiverID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "waiver_id is required in path"})
	}
	// Get the TournamentUser ID based on the external user ID
	var tournamentUser models.TournamentUser
	if err := s.DB.Where("external_user_id = ?", userID).First(&tournamentUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found in tournament system"})
		}
		log.Printf("DB Error fetching TournamentUser for mark redeemed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}
	// Find the specific waiver by ID and ensure it belongs to the user
	var waiver models.UserWaiver
	if err := s.DB.Where("id = ? AND user_id = ?", waiverID, tournamentUser.ID).First(&waiver).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Or return 404 if waiver doesn't exist or doesn't belong to user
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "waiver not found or does not belong to user"})
		}
		log.Printf("DB Error fetching waiver for mark redeemed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}
	// Update the IsRedeemed flag to true
	// Only update if not already fully used
	if waiver.UsedAmount >= waiver.Amount {
		return c.Status(400).JSON(fiber.Map{"error": "waiver is already fully used"})
	}

	updates := map[string]interface{}{
		"is_redeemed": true,
		"used_amount": waiver.Amount, // Mark as fully used
		"updated_at":  time.Now(),
	}
	// If not already set, set expiry based on duration if applicable
	// This handles the case where an admin marks it as fully redeemed without the user ever redeeming it partially
	if waiver.ExpiresAt == nil && waiver.DurationHours > 0 {
		// If it was never redeemed, set expiry from now based on duration
		expiresAt := time.Now().Add(time.Duration(waiver.DurationHours) * time.Hour)
		updates["expires_at"] = expiresAt
	} else if !waiver.IsRedeemed && waiver.DurationHours > 0 { // Check if it's being marked as redeemed for the first time via this endpoint
		// If it was partially used but not marked as redeemed, and duration is set, set expiry from now
		// This is a bit of an edge case if this endpoint is used differently, but covers the "admin marks as fully used now" scenario
		expiresAt := time.Now().Add(time.Duration(waiver.DurationHours) * time.Hour)
		// However, if a hard expiry was set originally, respect that if it's later
		if waiver.ExpiresAt != nil && waiver.ExpiresAt.After(expiresAt) {
			// Keep the original hard expiry if it's still valid and later
			updates["expires_at"] = waiver.ExpiresAt
		} else {
			// Otherwise, use the duration-based expiry
			updates["expires_at"] = expiresAt
		}
	}
	// If the original hard expiry is still valid and later than the duration-based one, it will be respected by the logic above

	if err := s.DB.Model(&waiver).Where("id = ?", waiverID).Updates(updates).Error; err != nil {
		log.Printf("DB Error updating waiver is_redeemed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to mark waiver as redeemed"})
	}

	// Optionally, return the updated waiver
	s.DB.First(&waiver, "id = ?", waiverID) // Reload to confirm update
	return c.JSON(fiber.Map{
		"message": "waiver marked as redeemed",
		"waiver":  waiver,
	})
}

// GetUserWaiversEndpoint fetches all waivers for the authenticated user (from UserContextMiddleware)
// This endpoint already exists but is included here with the updated model fields.
func (s *TournamentService) GetUserWaiversEndpoint(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string) // Assumes UserContextMiddleware sets this
	// First, get the TournamentUser ID based on the external user ID
	var tournamentUser models.TournamentUser
	if err := s.DB.Where("external_user_id = ?", userID).First(&tournamentUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// If the user doesn't exist in TournamentUser table, return empty list or an error
			// Returning empty list is safer here if they just haven't subscribed/interacted yet
			return c.JSON([]models.UserWaiver{})
			// Or return an error: return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found in tournament system"})
		}
		log.Printf("DB Error fetching TournamentUser: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	var waivers []models.UserWaiver
	query := s.DB.Where("user_id = ?", tournamentUser.ID) // Use the local TournamentUser ID

	// Optional: Add status filter based on query params if needed (e.g., ?status=active)
	statusFilter := c.Query("status")
	if statusFilter != "" {
		switch statusFilter {
		case "active":
			// Active: is_active, not expired (against hard expiry), not fully used
			query = query.Where("is_active = ? AND (expires_at IS NULL OR expires_at > ?) AND used_amount < amount AND is_redeemed = ?", true, time.Now(), false)
		case "used", "redeemed":
			// Redeemed: used at least once OR marked as redeemed
			query = query.Where("used_amount > 0 OR is_redeemed = ?", true)
		case "expired":
			// Expired: is_active, but hard expiry date passed
			query = query.Where("is_active = ? AND expires_at IS NOT NULL AND expires_at <= ?", true, time.Now())
		}
	}

	if err := query.Find(&waivers).Error; err != nil {
		log.Printf("DB Error fetching user waivers: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch waivers"})
	}

	return c.JSON(waivers)
}

// MarkWaiverAsClaimedEndpoint updates the IsClaimed flag for a specific waiver owned by the user.
// This endpoint allows marking a waiver as "claimed" (i.e., acknowledged/revealed) without spending its balance.
func (s *TournamentService) MarkWaiverAsClaimedEndpoint(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string) // Assumes UserContextMiddleware sets this
	waiverID := c.Params("id")             // Waiver ID from URL path
	if waiverID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "waiver_id is required in path"})
	}
	// Get the TournamentUser ID based on the external user ID
	var tournamentUser models.TournamentUser
	if err := s.DB.Where("external_user_id = ?", userID).First(&tournamentUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found in tournament system"})
		}
		log.Printf("DB Error fetching TournamentUser for mark claimed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}
	// Find the specific waiver by ID and ensure it belongs to the user
	var waiver models.UserWaiver
	if err := s.DB.Where("id = ? AND user_id = ?", waiverID, tournamentUser.ID).First(&waiver).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "waiver not found or does not belong to user"})
		}
		log.Printf("DB Error fetching waiver for mark claimed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}
	// Only update if not already claimed (or fully redeemed)
	if waiver.IsRedeemed {
		return c.Status(400).JSON(fiber.Map{"error": "waiver is already fully redeemed"})
	}

	updates := map[string]interface{}{
		"is_claimed": true, // New field: mark as claimed
		"updated_at": time.Now(),
	}

	if err := s.DB.Model(&waiver).Where("id = ?", waiverID).Updates(updates).Error; err != nil {
		log.Printf("DB Error updating waiver is_claimed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to mark waiver as claimed"})
	}

	// Optionally, return the updated waiver
	s.DB.First(&waiver, "id = ?", waiverID) // Reload to confirm update
	return c.JSON(fiber.Map{
		"message": "waiver marked as claimed",
		"waiver":  waiver,
	})
}