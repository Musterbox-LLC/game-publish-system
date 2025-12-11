package models

import (
	"time"
)

// BadgeType: static config (loaded from DB or JSON)
type BadgeType struct {
	ID          string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Code        string `gorm:"uniqueIndex;not null"` // e.g., "FIRST_WIN", "TOURNAMENT_CHAMP"
	Name        string `gorm:"not null"`             // "First Victory", "Tournament Champion"
	Description string
	IconURL     string            `gorm:"type:text"` // e.g., R2 URL to SVG/png
	Rarity      string            `gorm:"type:varchar(16);default:'common'"` // common, rare, epic, legendary
	Threshold   map[string]int64 `gorm:"type:jsonb"` // e.g., {"total_matches": 10}, {"final_rank": 1, "tournaments": 3}
	CreatedAt   time.Time         `gorm:"autoCreateTime"`
}

// UserBadge: awarded instance (many-to-many)
type UserBadge struct {
	ID             string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ExternalUserID string    `gorm:"index;not null"`
	BadgeTypeID    string    `gorm:"index;not null"`
	AwardedAt      time.Time `gorm:"autoCreateTime"`
	Metadata       string    `gorm:"type:jsonb"` // e.g., {"tournament_id": "...", "score": 9999}
}

// Predefined badge triggers (example)
var BadgeTriggers = []BadgeType{
	{
		Code:        "WELCOME",
		Name:        "Welcome Aboard!",
		Description: "Joined the platform",
		Rarity:      "common",
		Threshold:   map[string]int64{"event": 1}, // awarded on signup
	},
	{
		Code:        "FIRST_MATCH",
		Name:        "First Blood",
		Description: "Played your first match",
		Rarity:      "common",
		Threshold:   map[string]int64{"total_matches": 1},
	},
	{
		Code:        "TOURNAMENT_CHAMP",
		Name:        "Tournament Champion",
		Description: "Won a tournament",
		Rarity:      "epic",
		Threshold:   map[string]int64{"tournament_wins": 1}, // Note: Requires tracking wins separately if needed
	},
	{
		Code:        "REFER_5",
		Name:        "Recruiter",
		Description: "Referred 5 friends who deposited",
		Rarity:      "rare",
		Threshold:   map[string]int64{"total_referrals": 5},
	},
	{
		Code:        "LEVEL_50",
		Name:        "Halfway There",
		Description: "Reached Level 50 (Platinum!)",
		Rarity:      "epic",
		Threshold:   map[string]int64{"level": 50},
	},
}
