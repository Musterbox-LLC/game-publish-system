package models


// TournamentParticipation = subscription + activity summary
type TournamentParticipation struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ExternalUserID   string    `gorm:"index;not null" json:"external_user_id"`
	TournamentID     string    `gorm:"index;not null" json:"tournament_id"`
	SubscriptionID   string    `gorm:"index;not null" json:"subscription_id"` // links to TournamentSubscription.ID

	// Engagement
	TotalMatchesPlayed int64 `json:"total_matches_played" gorm:"default:0"`
	BestScore          int64 `json:"best_score" gorm:"default:0"`
	FinalRank          int   `json:"final_rank" gorm:"default:0"` // 0 = not ranked

	// XP & rewards
	XPEarned      int64  `json:"xp_earned" gorm:"default:0"`
	PrizeEarned   string `json:"prize_earned,omitempty"` // e.g., "100 USDC"
	BadgeAwarded  string `json:"badge_awarded,omitempty"`

	// Status
	Status string `json:"status" gorm:"type:varchar(16);default:'joined'"` // joined → active → completed → disqualified

	Timestamps
}