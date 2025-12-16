package models

import (
	"time"
	gorm "gorm.io/gorm"
)

// RewardType indicates whether the reward is cash or an item
type RewardType string

const (
	RewardTypeCash RewardType = "cash"
	RewardTypeItem RewardType = "item"
)

type RewardCategory string

const (
	RewardCategoryBonus          RewardCategory = "bonus"
	RewardCategoryBounty         RewardCategory = "bounty"
	RewardCategoryReferral       RewardCategory = "referral"
	RewardCategoryTournamentPrize RewardCategory = "tournament_prize"
	RewardCategoryAchievement    RewardCategory = "achievement"
	RewardCategoryMilestone      RewardCategory = "milestone"
	RewardCategoryOther          RewardCategory = "other"
)

// RewardStatus indicates the publishing status of the reward
type RewardStatus string

const (
	RewardStatusDraft     RewardStatus = "draft"
	RewardStatusPublished RewardStatus = "published"
	RewardStatusArchived  RewardStatus = "archived"
)

// Reward represents a reward entity
type Reward struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Title       string         `gorm:"not null" json:"title"`
	Type        RewardType     `gorm:"not null" json:"type"`
	Category    RewardCategory `gorm:"not null" json:"category"`
	ImageURL    string         `gorm:"type:text" json:"image_url"`
	Emoji       string         `gorm:"size:10" json:"emoji"`
	Excerpt     string         `gorm:"type:text" json:"excerpt"`
	Amount      float64        `json:"amount"`
	ItemDetails string         `json:"item_details"`
	ExpiryDate  *time.Time     `json:"expiry_date,omitempty"`
	Claimed     bool           `gorm:"default:false" json:"claimed"`
	Viewed      bool           `gorm:"default:false;index" json:"viewed"` // ← NEW
	UserID      string         `gorm:"index" json:"user_id"`
	Level       int            `json:"level"`
	Status      RewardStatus   `gorm:"not null;default:'draft'" json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}