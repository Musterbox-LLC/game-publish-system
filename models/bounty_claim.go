package models

import "time"

// BountyClaim = user completed a bounty (e.g., "Win 5 matches in 24h")
type BountyClaim struct {
	ID              string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ExternalUserID  string    `gorm:"index;not null" json:"external_user_id"`
	BountyID        string    `gorm:"not null" json:"bounty_id"`        // links to external bounty service or config
	BountyName      string    `json:"bounty_name"`                      // e.g., "Weekend Warrior"
	Description     string    `json:"description,omitempty"`
	XPEarned        int64     `json:"xp_earned" gorm:"not null"`       // e.g., 500
	Reward          string    `json:"reward,omitempty"`                 // e.g., "NFT Ticket"
	ClaimedAt       time.Time `json:"claimed_at" gorm:"autoCreateTime"`
}