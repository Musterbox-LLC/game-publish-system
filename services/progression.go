package services

import (
	"fmt"
	"math"
	"time"

	"game-publish-system/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// XPWeights define relative values (tunable via config/env later)
type XPWeights struct {
	MatchXP        int64 `default:"10"`
	TournamentXP   int64 `default:"100"`  // 10× match
	BountyXP       int64 `default:"500"`  // 50× match
	ReferralXP     int64 `default:"250"`  // 25× match
	FirstDepositXP int64 `default:"1000"` // 100× match
}

var DefaultXPWeights = XPWeights{
	MatchXP:        10,
	TournamentXP:   100,
	BountyXP:       500,
	ReferralXP:     250,
	FirstDepositXP: 1000,
}

// LevelConfig: XP needed for *next* level (e.g., level 1 → 2 needs BaseXPPerLevel * 1^1.2)
const BaseXPPerLevel = 100

// xpForNextLevel returns XP required to reach level+1 from current level
// e.g., xpForNextLevel(1) = XP to go from L1 → L2
func xpForNextLevel(currentLevel int) int64 {
	if currentLevel < 1 {
		currentLevel = 1
	}
	// L_n = floor(BaseXPPerLevel * n^1.2)
	return int64(float64(BaseXPPerLevel) * math.Pow(float64(currentLevel), 1.2))
}

// RankThresholds: levels required before rank-up
// e.g., Bronze→Silver at level 10, Silver→Gold at level 25, etc.
var RankThresholds = map[int]int{ // rank → min level
	1: 1,   // Bronze (start)
	2: 10,  // Silver
	3: 25,  // Gold
	4: 50,  // Platinum
	5: 100, // Diamond
}

func determineRank(level int) int {
	for rank := 5; rank >= 1; rank-- {
		if level >= RankThresholds[rank] {
			return rank
		}
	}
	return 1
}

type ProgressionService struct {
	DB *gorm.DB
}

func NewProgressionService(db *gorm.DB) *ProgressionService {
	return &ProgressionService{DB: db}
}

// EnsureProgressRecord ensures a UserProgress row exists (idempotent)
func (s *ProgressionService) EnsureProgressRecord(externalUserID string) (*models.UserProgress, error) {
	var prog models.UserProgress
	err := s.DB.Where("external_user_id = ?", externalUserID).First(&prog).Error
	if err == gorm.ErrRecordNotFound {
		prog = models.UserProgress{
			ID:             uuid.NewString(),
			ExternalUserID: externalUserID,
			TotalXP:        0,
			Level:          1,
			Rank:           1,
		}
		if err := s.DB.Create(&prog).Error; err != nil {
			return nil, err
		}
		return &prog, nil
	}
	if err != nil {
		return nil, err
	}
	return &prog, nil
}

// AwardXP atomically updates XP, level, rank — returns updated progress
func (s *ProgressionService) AwardXP(externalUserID string, xp int64, reason string) (*models.UserProgress, error) {
	var updatedProg *models.UserProgress
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var prog models.UserProgress
		if err := tx.Where("external_user_id = ?", externalUserID).First(&prog).Error; err != nil {
			return fmt.Errorf("progress record not found for %s", externalUserID)
		}

		oldRank := prog.Rank

		prog.TotalXP += xp

		// Level-up logic: accumulate until enough for next level
		for prog.TotalXP >= int64(BaseXPPerLevel)*int64(prog.Level)+xpForNextLevel(prog.Level) {
			prog.Level++
			now := time.Now()
			prog.LastLevelUpAt = &now
		}

		// Rank-up logic
		newRank := determineRank(prog.Level)
		if newRank > oldRank {
			now := time.Now()
			prog.Rank = newRank
			prog.LastRankUpAt = &now
		}

		// Save via tx
		if err := tx.Save(&prog).Error; err != nil {
			return err
		}
		
		// Auto-award badges
		badgeSvc := NewBadgeService(s.DB)
		_ = badgeSvc.AutoAwardBadges(externalUserID) // fire-and-forget

		// Copy for return (avoid pointer to stack var)
		updatedProg = &models.UserProgress{}
		*updatedProg = prog

		// Log
		fmt.Printf("🎮 XP Awarded: %s → XP=%d, Lvl=%d, Rank=%d (reason: %s)\n",
			externalUserID, prog.TotalXP, prog.Level, prog.Rank, reason)

		return nil
	})
	if err != nil {
		return nil, err
	}
	return updatedProg, nil
}

// RecordMatch creates Match entry + awards XP
func (s *ProgressionService) RecordMatch(match *models.Match) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Insert match
		if err := tx.Create(match).Error; err != nil {
			return err
		}

		// Increment TotalMatches in progression
		var prog models.UserProgress
		if err := tx.Where("external_user_id = ?", match.ExternalUserID).First(&prog).Error; err != nil {
			return fmt.Errorf("progress record not found for %s", match.ExternalUserID)
		}
		prog.TotalMatches++
		if err := tx.Save(&prog).Error; err != nil {
			return err
		}

		// Award XP (match weight)
		_, err := s.AwardXP(match.ExternalUserID, DefaultXPWeights.MatchXP, "match_played")
		return err
	})
}

// RecordTournamentParticipation finalizes tournament XP + counters
func (s *ProgressionService) RecordTournamentParticipation(tp *models.TournamentParticipation) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(tp).Error; err != nil {
			return err
		}

		// Increment TotalTournaments
		var prog models.UserProgress
		if err := tx.Where("external_user_id = ?", tp.ExternalUserID).First(&prog).Error; err != nil {
			return fmt.Errorf("progress record not found for %s", tp.ExternalUserID)
		}
		prog.TotalTournaments++
		if err := tx.Save(&prog).Error; err != nil {
			return err
		}

		// Award base tournament XP + bonus for rank
		baseXP := DefaultXPWeights.TournamentXP
		if tp.FinalRank == 1 {
			baseXP *= 3 // triple for winner
		} else if tp.FinalRank <= 3 {
			baseXP *= 2 // double for podium
		}
		_, err := s.AwardXP(tp.ExternalUserID, baseXP, fmt.Sprintf("tournament_%s_rank_%d", tp.TournamentID, tp.FinalRank))
		return err
	})
}

// RecordBountyClaim
func (s *ProgressionService) RecordBountyClaim(claim *models.BountyClaim) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(claim).Error; err != nil {
			return err
		}

		// Increment TotalBounties
		var prog models.UserProgress
		if err := tx.Where("external_user_id = ?", claim.ExternalUserID).First(&prog).Error; err != nil {
			return fmt.Errorf("progress record not found for %s", claim.ExternalUserID)
		}
		prog.TotalBounties++
		if err := tx.Save(&prog).Error; err != nil {
			return err
		}

		_, err := s.AwardXP(claim.ExternalUserID, claim.XPEarned, fmt.Sprintf("bounty_%s", claim.BountyID))
		return err
	})
}

// ProcessReferralAward checks for first deposit → awards XP if valid
func (s *ProgressionService) ProcessReferralAward(referralID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var r models.Referral
		if err := tx.Where("id = ?", referralID).First(&r).Error; err != nil {
			return err
		}
		if r.BonusAwarded {
			return nil // already processed
		}

		// Require first deposit
		if r.FirstDepositID == nil || r.FirstDepositAmt <= 0 {
			return nil // skip until deposit verified externally
		}

		// Increment TotalReferrals for referrer
		var referrerProg models.UserProgress
		if err := tx.Where("external_user_id = ?", r.ReferrerID).First(&referrerProg).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Ensure exists
				refProg, err := s.EnsureProgressRecord(r.ReferrerID)
				if err != nil {
					return err
				}
				referrerProg = *refProg
			} else {
				return err
			}
		}
		referrerProg.TotalReferrals++
		if err := tx.Save(&referrerProg).Error; err != nil {
			return err
		}

		// Award XP
		_, err := s.AwardXP(r.ReferrerID,
			DefaultXPWeights.ReferralXP+DefaultXPWeights.FirstDepositXP,
			fmt.Sprintf("referral_%s_deposit", r.ReferredID),
		)
		if err != nil {
			return err
		}

		// Mark as awarded
		now := time.Now()
		r.BonusAwarded = true
		r.AwardedAt = &now
		return tx.Save(&r).Error
	})
}

// GetRecentMatches returns matches in last N days
func (s *ProgressionService) GetRecentMatches(externalUserID string, days int) ([]models.Match, error) {
	var matches []models.Match
	since := time.Now().AddDate(0, 0, -days)
	err := s.DB.Where("external_user_id = ? AND created_at >= ?", externalUserID, since).
		Order("created_at DESC").
		Find(&matches).Error
	return matches, err
}

// GetRecentTournaments returns participations in last N days
func (s *ProgressionService) GetRecentTournaments(externalUserID string, days int) ([]models.TournamentParticipation, error) {
	var parts []models.TournamentParticipation
	since := time.Now().AddDate(0, 0, -days)
	err := s.DB.Where("external_user_id = ? AND created_at >= ?", externalUserID, since).
		Preload("Tournament").
		Order("created_at DESC").
		Find(&parts).Error
	return parts, err
}

// GetUserHistory returns paginated history (matches + tournaments + bounties)
func (s *ProgressionService) GetUserHistory(externalUserID string, page, size int) (map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	offset := (page - 1) * size

	var totalMatches, totalTournaments, totalBounties int64

	// Parallel counts
	s.DB.Model(&models.Match{}).Where("external_user_id = ?", externalUserID).Count(&totalMatches)
	s.DB.Model(&models.TournamentParticipation{}).Where("external_user_id = ?", externalUserID).Count(&totalTournaments)
	s.DB.Model(&models.BountyClaim{}).Where("external_user_id = ?", externalUserID).Count(&totalBounties)

	var matches []models.Match
	s.DB.Where("external_user_id = ?", externalUserID).
		Order("created_at DESC").
		Limit(size).Offset(offset).
		Find(&matches)

	var tournaments []models.TournamentParticipation
	s.DB.Where("external_user_id = ?", externalUserID).
		Preload("Tournament").
		Order("created_at DESC").
		Limit(size).Offset(offset).
		Find(&tournaments)

	var bounties []models.BountyClaim
	s.DB.Where("external_user_id = ?", externalUserID).
		Order("claimed_at DESC").
		Limit(size).Offset(offset).
		Find(&bounties)

	totalItems := totalMatches + totalTournaments + totalBounties
	totalPages := int((totalItems + int64(size) - 1) / int64(size))

	return map[string]interface{}{
		"matches":           matches,
		"tournaments":       tournaments,
		"bounties":          bounties,
		"page":              page,
		"size":              size,
		"total_items":       totalItems,
		"total_pages":       totalPages,
		"total_matches":     totalMatches,
		"total_tournaments": totalTournaments,
		"total_bounties":    totalBounties,
	}, nil
}

// GetTournamentLeaderboardForUser returns leaderboard entries around user ±5
func (s *ProgressionService) GetTournamentLeaderboardForUser(tournamentID, externalUserID string) ([]models.LeaderboardEntry, error) {
	var userEntry models.LeaderboardEntry
	if err := s.DB.Where("tournament_id = ? AND user_id = ?", tournamentID, externalUserID).
		First(&userEntry).Error; err != nil {
		return nil, err
	}

	lower := userEntry.Rank - 5
	if lower < 1 {
		lower = 1
	}
	upper := userEntry.Rank + 5

	var entries []models.LeaderboardEntry
	err := s.DB.Where("tournament_id = ? AND rank BETWEEN ? AND ?", tournamentID, lower, upper).
		Order("rank ASC").
		Find(&entries).Error
	return entries, err
}
