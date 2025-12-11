// services/scheduler.go
package services

import (
	"game-publish-system/models"
	"log"
	"time"

	"github.com/go-co-op/gocron/v2"
)

func (s *GameService) StartPublishScheduler() {
	sched, _ := gocron.NewScheduler()
	sched.Start()

	// Every minute: publish scheduled games
	_, _ = sched.NewJob(
		gocron.DurationJob(1*time.Minute),
		gocron.NewTask(func() {
			var games []models.Game
			now := time.Now()
			err := s.DB.Where("status = ? AND publish_at <= ?", "scheduled", now).
				Find(&games).Error
			if err != nil {
				log.Printf("[Scheduler] DB error: %v", err)
				return
			}

			for _, g := range games {
				g.Status = "published"
				g.PublishAt = nil
				if err := s.DB.Save(&g).Error; err != nil {
					log.Printf("[Scheduler] Failed to publish game %s: %v", g.ID, err)
				} else {
					log.Printf("✅ Auto-published game: %s", g.Name)
				}
			}
		}),
	)
}