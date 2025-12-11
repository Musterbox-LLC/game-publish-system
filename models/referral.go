package models

import "time"

// Referral tracks referrals and first-deposit bonuses
type Referral struct {
	ID                 string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ReferrerID         string    `gorm:"index;not null" json:"referrer_id"`         // ExternalUserID
	ReferredID         string    `gorm:"uniqueIndex;not null" json:"referred_id"`   // ExternalUserID

	ReferralCodeUsed string    `gorm:"not null" json:"referral_code_used"`
	FirstDepositID   *string   `gorm:"index" json:"first_deposit_id,omitempty"`   // links to deposit service
	FirstDepositAmt  float64   `json:"first_deposit_amt,omitempty"`
	XPEarned         int64     `json:"xp_earned" gorm:"default:0"`
	BonusAwarded     bool      `json:"bonus_awarded" gorm:"default:false"`
	AwardedAt        *time.Time `json:"awarded_at,omitempty"`

	Timestamps
}