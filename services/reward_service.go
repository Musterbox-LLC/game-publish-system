// services/reward_service.go
package services

import (
	"errors"
	"game-publish-system/models"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RewardService struct {
	DB *gorm.DB
}

func NewRewardService(db *gorm.DB) *RewardService {
	return &RewardService{DB: db}
}

// --- Admin Handlers ---

// CreateReward creates a new reward (Admin only)
func (s *RewardService) CreateReward(c *fiber.Ctx) error {
	var req struct {
		Title       string                `json:"title" validate:"required"`
		Type        models.RewardType     `json:"type" validate:"required,oneof=cash item"`
		Category    models.RewardCategory `json:"category" validate:"required,oneof=bonus bounty referral tournament_prize achievement milestone other"`
		ImageURL    string                `json:"image_url"`
		Emoji       string                `json:"emoji"`
		Excerpt     string                `json:"excerpt"`
		Amount      *float64              `json:"amount"`
		ItemDetails string                `json:"item_details"`
		ExpiryDate  *time.Time            `json:"expiry_date"`
		UserID      string                `json:"user_id" validate:"omitempty,uuid"`
		Level       int                   `json:"level"`
		Status      models.RewardStatus   `json:"status" validate:"required,oneof=draft published archived"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Basic validation
	if req.Type == models.RewardTypeCash && req.Amount == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Amount is required for cash rewards"})
	}
	if req.Type == models.RewardTypeItem && req.ItemDetails == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Item details are required for item rewards"})
	}

	reward := &models.Reward{
		ID:          uuid.NewString(),
		Title:       req.Title,
		Type:        req.Type,
		Category:    req.Category,
		ImageURL:    req.ImageURL,
		Emoji:       req.Emoji,
		Excerpt:     req.Excerpt,
		Amount:      0, // Initialize
		ItemDetails: req.ItemDetails,
		ExpiryDate:  req.ExpiryDate,
		Claimed:     false,      // New rewards are unclaimed by default
		UserID:      req.UserID, // Can be empty initially
		Level:       req.Level,
		Status:      req.Status,
		Viewed:      false, // New rewards are unviewed by default
	}

	if req.Amount != nil {
		reward.Amount = *req.Amount
	}

	if err := s.DB.Create(reward).Error; err != nil {
		log.Printf("DB Error creating reward: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create reward"})
	}

	return c.Status(fiber.StatusCreated).JSON(reward)
}

// UpdateReward updates an existing reward (Admin only)
func (s *RewardService) UpdateReward(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid reward ID"})
	}

	var existingReward models.Reward
	if err := s.DB.First(&existingReward, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Reward not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	var req struct {
		Title       *string              `json:"title"`
		Type        *models.RewardType   `json:"type"`
		ImageURL    *string              `json:"image_url"`
		Emoji       *string              `json:"emoji"`
		Excerpt     *string              `json:"excerpt"`
		Amount      *float64             `json:"amount"`
		ItemDetails *string              `json:"item_details"`
		ExpiryDate  *time.Time           `json:"expiry_date"`
		UserID      *string              `json:"user_id"`
		Level       *int                 `json:"level"`
		Status      *models.RewardStatus `json:"status"`
		Viewed      *bool                `json:"viewed"` // Add viewed flag for updates if needed
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Apply updates if provided
	if req.Title != nil {
		existingReward.Title = *req.Title
	}
	if req.Type != nil {
		existingReward.Type = *req.Type
		// Validate amount/details based on new type
		if *req.Type == models.RewardTypeCash && req.Amount == nil && existingReward.Amount == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Amount is required for cash rewards"})
		}
		if *req.Type == models.RewardTypeItem && req.ItemDetails == nil && existingReward.ItemDetails == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Item details are required for item rewards"})
		}
	}
	if req.ImageURL != nil {
		existingReward.ImageURL = *req.ImageURL
	}
	if req.Emoji != nil {
		existingReward.Emoji = *req.Emoji
	}
	if req.Excerpt != nil {
		existingReward.Excerpt = *req.Excerpt
	}
	if req.Amount != nil {
		existingReward.Amount = *req.Amount
	}
	if req.ItemDetails != nil {
		existingReward.ItemDetails = *req.ItemDetails
	}
	if req.ExpiryDate != nil {
		existingReward.ExpiryDate = req.ExpiryDate
	}
	if req.UserID != nil {
		existingReward.UserID = *req.UserID
	}
	if req.Level != nil {
		existingReward.Level = *req.Level
	}
	if req.Status != nil {
		existingReward.Status = *req.Status
	}
	// Update viewed flag if provided
	if req.Viewed != nil {
		existingReward.Viewed = *req.Viewed
	}

	if err := s.DB.Save(&existingReward).Error; err != nil {
		log.Printf("DB Error updating reward: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update reward"})
	}

	return c.JSON(existingReward)
}

// DeleteReward deletes a reward (Admin only)
func (s *RewardService) DeleteReward(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid reward ID"})
	}

	var reward models.Reward
	if err := s.DB.First(&reward, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Reward not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	// Use Delete to trigger soft delete if DeletedAt field exists
	if err := s.DB.Delete(&reward).Error; err != nil {
		log.Printf("DB Error deleting reward: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete reward"})
	}

	return c.JSON(fiber.Map{"message": "Reward deleted successfully"})
}

// --- User Handlers ---

// GetUserRewards fetches rewards for the *authenticated* user based on filters
func (s *RewardService) GetUserRewards(c *fiber.Ctx) error {
	// Retrieve the user ID from the context set by the middleware
	userID := c.Locals("user_id").(string)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "User ID not found in context"})
	}

	// Query parameters for filtering
	limitStr := c.Query("limit")     // e.g., limit=10
	claimedStr := c.Query("claimed") // e.g., claimed=all (default), claimed=true, claimed=false
	statusStr := c.Query("status")   // e.g., status=published (default), status=any

	var limit *int
	if limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err != nil || l <= 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid limit parameter"})
		}
		limit = &l
	}

	var claimedFilter *bool
	switch strings.ToLower(claimedStr) {
	case "true":
		claimed := true
		claimedFilter = &claimed
	case "false":
		claimed := false
		claimedFilter = &claimed
		// Default ("all" or not provided) means no filter on claimed status
	}

	var statusFilter *models.RewardStatus
	switch strings.ToLower(statusStr) {
	case "any":
		// No filter on status
	case "published":
		fallthrough
	case "draft":
		fallthrough
	case "archived":
		status := models.RewardStatus(strings.ToLower(statusStr))
		statusFilter = &status
	default:
		// Default to published
		publishedStatus := models.RewardStatusPublished
		statusFilter = &publishedStatus
	}

	// Query for rewards belonging to the authenticated user
	query := s.DB.Where("user_id = ?", userID)

	if claimedFilter != nil {
		query = query.Where("claimed = ?", *claimedFilter)
	}

	if statusFilter != nil {
		query = query.Where("status = ?", *statusFilter)
	}

	var rewards []models.Reward
	dbQuery := query.Order("created_at DESC") // Order by creation date, newest first

	if limit != nil {
		dbQuery = dbQuery.Limit(*limit)
	}

	if err := dbQuery.Find(&rewards).Error; err != nil {
		log.Printf("DB Error fetching user rewards: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch rewards"})
	}

	return c.JSON(rewards)
}

// GetUserRewardCountsEndpoint returns the total count and count of unviewed rewards for the authenticated user.
// This is the endpoint you can poll every 30 seconds.
func (s *RewardService) GetUserRewardCountsEndpoint(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	now := time.Now()

	baseQuery := s.DB.Model(&models.Reward{}).
		Where("user_id = ?", userID).
		Where("status = ?", models.RewardStatusPublished).
		Where("(expiry_date IS NULL OR expiry_date >= ?)", now)

	// Count total (valid) rewards
	var totalCount int64
	if err := baseQuery.Count(&totalCount).Error; err != nil {
		log.Printf("DB Error counting total rewards: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error counting total rewards"})
	}

	// Count unviewed (valid) rewards
	var unviewedCount int64
	if err := baseQuery.
		Where("viewed = ?", false).
		Count(&unviewedCount).Error; err != nil {
		log.Printf("DB Error counting unviewed rewards: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error counting unviewed rewards"})
	}

	// Count unclaimed (valid) rewards
	var unclaimedCount int64
	if err := baseQuery.
		Where("claimed = ?", false). // Assuming your Reward model has a 'claimed' field
		Count(&unclaimedCount).Error; err != nil {
		log.Printf("DB Error counting unclaimed rewards: %v", err)
		return c.Status(fiber.StatusInternalServerError).
			JSON(fiber.Map{"error": "DB error counting unclaimed rewards"})
	}

	return c.JSON(fiber.Map{
		"total_count":      totalCount,
		"unviewed_count":   unviewedCount,
		"unclaimed_count":  unclaimedCount,
	})
}

// ClaimReward handles the claiming of a reward by the user
func (s *RewardService) ClaimReward(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string) 
	rewardID := c.Params("id")

	if _, err := uuid.Parse(rewardID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid reward ID"})
	}

	var reward models.Reward
	if err := s.DB.Where("id = ? AND user_id = ?", rewardID, userID).First(&reward).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Reward not found or not owned by user"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	if reward.Claimed {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Reward already claimed"})
	}

	if reward.Status != models.RewardStatusPublished {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Reward is not available for claiming"})
	}

	if reward.ExpiryDate != nil && reward.ExpiryDate.Before(time.Now()) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Reward has expired"})
	}

	reward.Claimed = true
	if err := s.DB.Save(&reward).Error; err != nil {
		log.Printf("DB Error claiming reward: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to claim reward"})
	}

	return c.JSON(fiber.Map{"message": "Reward claimed successfully", "reward": reward})
}

// --- Admin Handler for Publishing ---

// UpdateRewardStatus allows admin to change the status (e.g., draft -> published)
func (s *RewardService) UpdateRewardStatus(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid reward ID"})
	}

	var req struct {
		Status models.RewardStatus `json:"status" validate:"required,oneof=draft published archived"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	var existingReward models.Reward
	if err := s.DB.First(&existingReward, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Reward not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	existingReward.Status = req.Status

	if err := s.DB.Save(&existingReward).Error; err != nil {
		log.Printf("DB Error updating reward status: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update reward status"})
	}

	return c.JSON(fiber.Map{"message": "Reward status updated successfully", "reward": existingReward})
}

// GetAllRewards fetches all rewards (Admin only, potentially paginated in future)
func (s *RewardService) GetAllRewards(c *fiber.Ctx) error {
	var rewards []models.Reward
	if err := s.DB.Find(&rewards).Error; err != nil {
		log.Printf("DB Error fetching all rewards: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch rewards"})
	}

	return c.JSON(rewards)
}

// MarkRewardAsViewed marks a single reward as viewed (idempotent)
func (s *RewardService) MarkRewardAsViewed(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	rewardID := c.Params("id")

	if _, err := uuid.Parse(rewardID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid reward ID"})
	}

	var reward models.Reward
	if err := s.DB.Where("id = ? AND user_id = ?", rewardID, userID).First(&reward).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Reward not found or not owned"})
		}
		log.Printf("DB error fetching reward: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "DB error"})
	}

	if !reward.Viewed {
		reward.Viewed = true
		if err := s.DB.Save(&reward).Error; err != nil {
			log.Printf("Failed to update viewed status: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to mark as viewed"})
		}
	}

	return c.JSON(fiber.Map{"message": "OK", "reward_id": reward.ID, "viewed": true})
}

// Optional: Mark *all* rewards for the user as viewed
func (s *RewardService) MarkAllRewardsAsViewed(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	result := s.DB.Model(&models.Reward{}).
		Where("user_id = ? AND viewed = ?", userID, false).
		Update("viewed", true)

	if result.Error != nil {
		log.Printf("Bulk mark viewed failed: %v", result.Error)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update rewards"})
	}

	return c.JSON(fiber.Map{
		"message":        "OK",
		"marked_count":   result.RowsAffected,
		"total_unviewed": 0, // if you want pre-count, query first
	})
}