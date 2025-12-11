package models

import (
	"time"
	"gorm.io/gorm"
)

// TournamentUser is a local snapshot of user data needed for tournaments.
// Owned and managed solely by the Tournament/Publishing service.
// Populated via sync worker from Profile Service's user table.
type TournamentUser struct {
	ID              string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ExternalUserID  string    `gorm:"uniqueIndex;not null" json:"external_user_id"` // The profile service's UUID (from users.external_id)
	Username        string    `gorm:"index;not null" json:"username"`
	Email           string    `json:"email,omitempty"`
	ProfilePictureURL *string  `json:"profile_picture_url,omitempty"`
	CoverPhotoURL   *string  `json:"cover_photo_url,omitempty"` 
	FirstName       *string   `json:"first_name,omitempty"`
	LastName        *string   `json:"last_name,omitempty"`
	Bio             *string   `json:"bio,omitempty"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	// Optional: caching for performance
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	IsBanned  bool       `json:"is_banned" gorm:"default:false"` // local tournament ban

	// Soft delete (if needed for history)
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}


// RemoteUser mirrors the schema of the foreign `users` table (read-only).
// Used by sync worker to fetch data from the profile service's DB.
type RemoteUser struct {
	ID                uint       `gorm:"column:id"`
	Username          string     `gorm:"column:username"`
	Email             string     `gorm:"column:email"`
	FirstName         *string    `gorm:"column:first_name"`
	LastName          *string    `gorm:"column:last_name"`
	Bio               *string    `gorm:"column:bio"`
	ProfilePictureURL *string    `gorm:"column:profile_picture_url"`
	CoverPhotoURL     *string    `gorm:"column:cover_photo_url"`
	ExternalID        string     `gorm:"column:external_id"` // ← critical: links to our TournamentUser.ExternalUserID
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	DeletedAt         *time.Time `gorm:"column:deleted_at"` // soft-delete marker
}