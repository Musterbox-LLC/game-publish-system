package services

import (
	"fmt"
	"game-publish-system/models"
	"gorm.io/gorm"
)

type BadgeService struct {
	DB *gorm.DB
}

func NewBadgeService(db *gorm.DB) *BadgeService {
	return &BadgeService{DB: db}
}

// AutoAwardBadges checks all badge triggers for a user after a progress update
func (s *BadgeService) AutoAwardBadges(externalUserID string) error {
	var prog models.UserProgress
	if err := s.DB.Where("external_user_id = ?", externalUserID).First(&prog).Error; err != nil {
		return err
	}

	var awarded []string
	for _, trigger := range models.BadgeTriggers {
		if s.meetsThreshold(&prog, trigger.Threshold) {
			// Check if already awarded
			var count int64
			s.DB.Model(&models.UserBadge{}).
				Where("external_user_id = ? AND badge_type_id = ?", externalUserID, trigger.ID).
				Count(&count)
			if count == 0 {
				// Award!
				userBadge := models.UserBadge{
					ExternalUserID: externalUserID,
					BadgeTypeID:    trigger.ID,
				}
				if err := s.DB.Create(&userBadge).Error; err != nil {
					return err
				}
				awarded = append(awarded, trigger.Name)
				fmt.Printf("🎖️ Badge awarded: %s → %s\n", trigger.Name, externalUserID)
			}
		}
	}

	if len(awarded) > 0 {
		// Optional: emit event for push notification: "🎉 You earned: 'Tournament Champion'!"
	}
	return nil
}

func (s *BadgeService) meetsThreshold(prog *models.UserProgress, req map[string]int64) bool {
	for key, required := range req {
		switch key {
		case "total_matches":
			if prog.TotalMatches < required { return false }
		case "total_tournaments":
			if prog.TotalTournaments < required { return false }
		case "total_bounties":
			if prog.TotalBounties < required { return false }
		case "total_referrals":
			if prog.TotalReferrals < required { return false }
		case "level":
			if int64(prog.Level) < required { return false }
		case "rank":
			if int64(prog.Rank) < required { return false }
		case "event": // special: always true (e.g., signup)
			return true
		}
	}
	return true
}
