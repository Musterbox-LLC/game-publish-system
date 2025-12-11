package models

import (
	"time"

	"gorm.io/gorm"
)

// UserProgress tracks gamified progression for each user (denormalized for performance)
type UserProgress struct {
	ID                string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ExternalUserID    string    `gorm:"uniqueIndex;not null" json:"external_user_id"` // links to profile service

	// Core progression
	TotalXP      int64 `json:"total_xp" gorm:"default:0"`
	Level        int   `json:"level" gorm:"default:1"`
	Rank         int   `json:"rank" gorm:"default:1"` // e.g., Bronze(1)→Silver(2)→Gold(3)→Platinum(4)→Diamond(5)

	// Activity counters
	TotalMatches      int64 `json:"total_matches" gorm:"default:0"`
	TotalTournaments  int64 `json:"total_tournaments" gorm:"default:0"`
	TotalBounties     int64 `json:"total_bounties" gorm:"default:0"`
	TotalReferrals    int64 `json:"total_referrals" gorm:"default:0"`

	TournamentsWon int64 `json:"tournaments_won,omitempty" gorm:"-"`
	BountiesWon    int64 `json:"bounties_won,omitempty" gorm:"-"`

	// Milestones
	LastLevelUpAt *time.Time `json:"last_level_up_at,omitempty"`
	LastRankUpAt  *time.Time `json:"last_rank_up_at,omitempty"`

	Timestamps
}

// Timestamps adds GORM auto-times
type Timestamps struct {
	CreatedAt time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`
}

