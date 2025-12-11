// models/wallet_mirror.go
package models

import (
	"time"

	"gorm.io/gorm"
)

// WalletMirror mirrors wallet data from sync service.
// Table name: wallet_mirror
type WalletMirror struct {
	ID                 string    `gorm:"primaryKey;type:uuid;not null" json:"id"`
	UserID             string    `gorm:"type:uuid;not null;index" json:"user_id"` // External user ID
	Chain              string    `gorm:"type:varchar(64);not null;index" json:"chain"`
	Token              string    `gorm:"type:varchar(64);not null" json:"token"`
	Address            string    `gorm:"type:varchar(128);not null;uniqueIndex" json:"address"` // Primary lookup key
	FirstDepositMade   bool      `gorm:"not null" json:"first_deposit_made"`
	DerivationIndex    int32     `gorm:"not null" json:"derivation_index"`
	IsTreasury         bool      `gorm:"not null" json:"is_treasury"`
	IsActive           bool      `gorm:"not null" json:"is_active"`
	LastBalanceCheckAt time.Time `gorm:"not null" json:"last_balance_check_at"`
	CreatedAt          time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time `gorm:"not null" json:"updated_at"`

	// GORM soft delete (optional — remove if you want hard delete)
	DeletedAt gorm.DeletedAt `gorm:"index"`
}