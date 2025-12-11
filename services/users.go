// services/tournament.go
package services

import (
	"game-publish-system/models"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// SearchUsers searches for users within the local TournamentUser table.
func (s *TournamentService) SearchUsers(c *fiber.Ctx) error {
	query := c.Query("q", "")
	limitStr := c.Query("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 50
	}

	var users []models.TournamentUser
	db := s.DB.Model(&models.TournamentUser{}).Limit(limit)

	// Apply search filter if query is provided
	if query != "" {
		searchTerm := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
		db = db.Where(
			"LOWER(username) LIKE ? OR LOWER(email) LIKE ?",
			searchTerm, searchTerm,
		)
	}

	// Execute the query
	if err := db.Find(&users).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "search failed", "details": err.Error()})
	}

	// Define a minimal struct for the response to avoid exposing internal fields
	// and prioritize the ExternalUserID.
	type UserSummary struct {
		ID       string `json:"id"`                // Internal TournamentUser ID (UUID) - Optional, might be useful internally
		ExternalUserID string `json:"external_user_id"` // The original user ID from the profile service - This is the key identifier for external consumers
		Username string `json:"username"`
		Email    string `json:"email"`
		// Add other fields you want to expose if needed, e.g., ProfilePictureURL
	}

	// Map the DB results to the response struct
	res := make([]UserSummary, len(users))
	for i, u := range users {
		res[i] = UserSummary{
			ID:       u.ID,                 // The local UUID primary key
			ExternalUserID: u.ExternalUserID, // ✅ The original profile service UUID - This is what the client gets as the main identifier
			Username: u.Username,
			Email:    u.Email,
		}
	}

	return c.JSON(res)
}