// models/game.go
package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	GameTypeWebGL  = "webgl"
	GameTypeNative = "native" // Android/iOS/PC
)

const (
	PlatformWeb     = "web"
	PlatformAndroid = "android"
	PlatformiOS     = "ios"
	PlatformPC      = "pc"
)

type Game struct {
	ID               string           `json:"id" gorm:"primaryKey"`
	Name             string           `json:"name" gorm:"not null"`
	ShortDescription string           `json:"short_description"`
	LongDescription  string           `json:"long_description"`
	GameType         string           `json:"game_type"`
	Platform         string           `json:"platform"`
	AgeRating        string           `json:"age_rating"`

	// 🖼️ Media
	MainLogoURL string `json:"main_logo_url"` // e.g., "/uploads/logo/abc.png"
	Screenshots []GameScreenshot `json:"screenshots" gorm:"foreignKey:GameID"`
	VideoLinks  []GameVideo      `json:"video_links" gorm:"foreignKey:GameID"`

	// 📁 Core file
	FileURL  string `json:"file_url"`
	PlayLink string `json:"play_link"`

	// 🔺 NEW: Store platform-specific links
	PlayStoreURL  string `json:"play_store_url,omitempty"`
	AppStoreURL   string `json:"app_store_url,omitempty"`
	PCDownloadURL string `json:"pc_download_url,omitempty"`

	// 🔺 NEW: Flag for Web3GL auto-hosted games
	IsWeb3GL        bool   `json:"is_web3gl"`
	Web3GLHostedURL string `json:"web3gl_hosted_url,omitempty"`

	// 🌟 NEW: Average rating from reviews
	AverageRating float64 `json:"average_rating" gorm:"default:0"`

	// 🎛️ Publishing state
	Status    string     `json:"status" gorm:"default:'draft'"` // draft | scheduled | published
	PublishAt *time.Time `json:"publish_at"`                   // only used if scheduled

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index"`

	// 🔗 Reviews
	Reviews []Review `json:"reviews" gorm:"foreignKey:GameID"`
}

type GameScreenshot struct {
	ID      string `json:"id" gorm:"primaryKey"`
	GameID  string `json:"game_id"`
	URL     string `json:"url"` // e.g., "/uploads/screenshots/xyz.jpg"
	Order   int    `json:"order"`
}

type GameVideo struct {
	ID      string `json:"id" gorm:"primaryKey"`
	GameID  string `json:"game_id"`
	URL     string `json:"url"` // e.g., "https://youtube.com/watch?v=..."
	Title   string `json:"title"`
	Order   int    `json:"order"`
}


type Review struct {
	ID            string    `json:"id" gorm:"primaryKey"`
	GameID        string    `json:"game_id" gorm:"index;not null"`
	UserID        string    `json:"user_id"` // 🔗 ties to user profile
	UserName      string    `json:"user_name"`
	UserAvatarURL string    `json:"user_avatar_url"` // ✅ from user profile picture link
	Rating        int       `json:"rating" gorm:"check:rating >= 1 and rating <= 5"`
	Comment       string    `json:"comment"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}