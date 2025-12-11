// handlers/progression_routes.go
package handlers

import (
	"game-publish-system/middleware"
	"game-publish-system/models"
	"game-publish-system/services"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func SetupProgressionRoutes(app *fiber.App, progressionService *services.ProgressionService, badgeService *services.BadgeService) {
	// 🔐 Secured routes — require user context (userID, roles)
	// Apply UserContextMiddleware to each secured route explicitly
	// This group applies the middleware to all routes under it.
	// The gateway should forward paths like /api/v1/game/s/user/progress -> /user/progress
	securedGroup := app.Group("/", middleware.UserContextMiddleware())

	securedGroup.Get("/user/progress", func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		var prog *models.UserProgress
		var dbProg models.UserProgress

		if err := progressionService.DB.Where("external_user_id = ?", userID).First(&dbProg).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				var createErr error
				prog, createErr = progressionService.EnsureProgressRecord(userID)
				if createErr != nil {
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error": "failed to create progress record",
						"cause": createErr.Error(),
					})
				}
			} else {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "DB error fetching progress",
					"cause": err.Error(),
				})
			}
		} else {
			prog = &dbProg
		}

		// ✅ Compute tournaments won
		var tournamentsWon int64
		if err := progressionService.DB.
			Model(&models.TournamentParticipation{}).
			Where("external_user_id = ? AND final_rank = 1", userID).
			Count(&tournamentsWon).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to count tournament wins",
				"cause": err.Error(),
			})
		}

		// ✅ Compute matches (battles) won
		var matchesWon int64
		// Replace 'models.Match' and 'WinnerID' with your actual model and field names
		if err := progressionService.DB.
			Model(&models.Match{}). // Ensure this model exists
			Where("winner_id = ?", userID). // Ensure this field name is correct
			Count(&matchesWon).Error; err != nil {
			// Log the error but don't fail the whole request if matches table/field is missing
			matchesWon = 0
			// Optional: log the error
			// log.Printf("Warning: failed to count match wins for user %s: %v", userID, err)
		}

		// ✅ Fetch recent tournament wins
		type RecentWin struct {
			TournamentID string    `json:"tournament_id"`
			Name         string    `json:"name"`
			Rank         int       `json:"rank"`
			XPEarned     int64     `json:"xp_earned"`
			CreatedAt    time.Time `json:"created_at"`
		}
		var recentWins []RecentWin
		if err := progressionService.DB.Raw(`
		SELECT tp.tournament_id, t.name, tp.final_rank AS rank, tp.xp_earned, tp.created_at
		FROM tournament_participations tp
		INNER JOIN tournaments t ON t.id = tp.tournament_id
		WHERE tp.external_user_id = ? AND tp.final_rank = 1
		ORDER BY tp.created_at DESC
		LIMIT 3
	`, userID).Scan(&recentWins).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to fetch recent wins",
				"cause": err.Error(),
			})
		}

		// ✅ Build response including matches_won
		response := fiber.Map{
			"id":                     prog.ID,
			"xp":                     prog.TotalXP,
			"level":                  prog.Level,
			"rank":                   prog.Rank,
			"rank_name":              rankName(prog.Rank),
			"total_matches":          prog.TotalMatches, // Total played, not won
			"total_tournaments":      prog.TotalTournaments, // Total played, not won
			"tournaments_won":        tournamentsWon,
			"bounties_won":           prog.TotalBounties,
			"matches_won":            matchesWon,
			"recent_tournament_wins": recentWins,
			"last_level_up_at":       prog.LastLevelUpAt,
			"last_rank_up_at":        prog.LastRankUpAt,
		}

		return c.JSON(response)
	})

	// Other secured routes remain the same
	securedGroup.Get("/user/progress/history", func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)
		page, _ := strconv.Atoi(c.Query("page", "1"))
		size, _ := strconv.Atoi(c.Query("size", "20"))
		history, err := progressionService.GetUserHistory(userID, page, size)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to get history",
				"cause": err.Error(),
			})
		}
		return c.JSON(history)
	})

	securedGroup.Get("/user/progress/recent", func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)
		days, _ := strconv.Atoi(c.Query("days", "7"))
		matches, err := progressionService.GetRecentMatches(userID, days)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to get recent matches",
				"cause": err.Error(),
			})
		}
		tournaments, err := progressionService.GetRecentTournaments(userID, days)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to get recent tournaments",
				"cause": err.Error(),
			})
		}
		return c.JSON(fiber.Map{
			"matches":     matches,
			"tournaments": tournaments,
		})
	})

	securedGroup.Get("/user/progress/badges", func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)
		var userBadges []struct {
			models.UserBadge
			models.BadgeType `gorm:"foreignKey:BadgeTypeID"`
		}
		if err := progressionService.DB.
			Preload("BadgeType").
			Where("external_user_id = ?", userID).
			Find(&userBadges).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to get badges",
				"cause": err.Error(),
			})
		}

		var response []fiber.Map
		for _, ub := range userBadges {
			response = append(response, fiber.Map{
				"id":            ub.UserBadge.ID,
				"badge_type_id": ub.BadgeType.ID,
				"code":          ub.BadgeType.Code,
				"name":          ub.BadgeType.Name,
				"description":   ub.BadgeType.Description,
				"icon_url":      ub.BadgeType.IconURL,
				"rarity":        ub.BadgeType.Rarity,
				"awarded_at":    ub.UserBadge.AwardedAt,
				"metadata":      ub.UserBadge.Metadata,
			})
		}
		return c.JSON(response)
	})

	// Admin endpoints
	adminGroup := app.Group("/s/admin", middleware.UserContextMiddleware())

	adminGroup.Post("/xp/grant", func(c *fiber.Ctx) error {
		type Req struct {
			UserID string `json:"user_id" validate:"required,uuid"`
			XP     int64  `json:"xp" validate:"required,min=1"`
			Reason string `json:"reason" validate:"max=255"`
		}
		var req Req
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid JSON",
				"cause": err.Error(),
			})
		}

		if _, err := progressionService.AwardXP(req.UserID, req.XP, req.Reason); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "XP award failed",
				"cause": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"message": "XP granted successfully",
			"user_id": req.UserID,
			"xp":      req.XP,
		})
	})
}

func rankName(rank int) string {
	switch rank {
	case 1:
		return "Rookie"
	case 2:
		return "Bronze"
	case 3:
		return "Silver"
	case 4:
		return "Gold"
	case 5:
		return "Platinum"
	case 6:
		return "Diamond"
	default:
		if rank > 6 {
			return "Legend"
		}
		return "Rookie"
	}
}