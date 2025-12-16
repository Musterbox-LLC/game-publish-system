package services

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"game-publish-system/models"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// StreamUserRewardsSSE streams real-time reward updates for the authenticated user
func (s *RewardService) StreamUserRewardsSSE(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	// SSE headers
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // nginx

	// Use fasthttp stream writer (THIS replaces Flush)
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		var lastMaxCreatedAt time.Time

		// Initialize cursor
		var latest models.Reward
		if err := s.DB.
			Where("user_id = ?", userID).
			Order("created_at DESC").
			First(&latest).Error; err == nil {
			lastMaxCreatedAt = latest.CreatedAt
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("SSE init error for user %s: %v", userID, err)
		}

		// Initial keepalive (comment event)
		w.WriteString(":\n\n")
		w.Flush()

		for {
			select {
			case <-ticker.C:
				var newRewards []models.Reward

				err := s.DB.
					Where("user_id = ? AND status = ?", userID, models.RewardStatusPublished).
					Where("created_at > ?", lastMaxCreatedAt).
					Order("created_at ASC").
					Find(&newRewards).Error

				if err != nil {
					log.Printf("SSE query error for user %s: %v", userID, err)
					continue
				}

				if len(newRewards) == 0 {
					continue
				}

				lastMaxCreatedAt = newRewards[len(newRewards)-1].CreatedAt

				for _, r := range newRewards {
					payload, _ := json.Marshal(r)

					fmt.Fprintf(w,
						"event: reward\ndata: %s\n\n",
						payload,
					)
				}

				// This is the REAL "flush"
				if err := w.Flush(); err != nil {
					// Client disconnected
					return
				}

			case <-c.Context().Done():
				// Client closed connection
				return
			}
		}
	})

	return nil
}
