package models

import (
	"time"
)

// Tournament represents a leaderboard-style tournament
type Tournament struct {
	ID              string     `json:"id" gorm:"primaryKey"`
	GameID          string     `json:"game_id" gorm:"not null"`
	Name            string     `json:"name" gorm:"not null"` // Tournament name/title
	Description     string     `json:"description"`
	Rules           string     `json:"rules"`
	Guidelines      string     `json:"guidelines"`
	Genre           string     `json:"genre"` // e.g., "FPS", "Puzzle", "Racing"
	GenreTags       string     `json:"genre_tags" gorm:"column:genre_tags"`
	MaxSubscribers  int        `json:"max_subscribers" gorm:"default:0"` // 0 = unlimited
	EntryFee        float64    `json:"entry_fee" gorm:"default:0"`       // in USD or tokens (handle currency layer later)
	MainPhotoURL    string     `json:"main_photo_url"`                   // ✅ R2 public URL
	Status          string     `json:"status" gorm:"default:'draft'"`    // draft, active, completed, cancelled
	StartTime       time.Time  `json:"start_time" gorm:"not null"`
	EndTime         time.Time  `json:"end_time"`
	CreatedAt       time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
	PublishedAt     *time.Time `json:"published_at,omitempty" gorm:"index"` // When it was actually published (set on status change to 'published' or 'active')
	PrizePool       string     `json:"prize_pool"`                          // e.g., "$25,000" or numeric value
	Requirements    string     `json:"requirements" gorm:"type:text"`
	SponsorName     string     `json:"sponsor_name"`                     // Name of the tournament sponsor
	IsFeatured      bool       `json:"is_featured" gorm:"default:false"` // Whether the tournament is featured
	PublishSchedule *time.Time `json:"publish_schedule,omitempty"`       // Optional scheduled time to publish/activate the tournament
	AcceptsWaivers  bool       `gorm:"default:true"`
	// Relationships (properly defined)
	Game                   Game                     `json:"game,omitempty" gorm:"foreignKey:GameID"`              // ✅ 1-to-many: Game -> Tournament
	Photos                 []TournamentPhoto        `json:"photos,omitempty" gorm:"foreignKey:TournamentID"`      // ✅ 1-to-many: Tournament -> Photos
	Batches                []TournamentBatch        `json:"batches,omitempty" gorm:"foreignKey:TournamentID"`     // ✅ 1-to-many: Tournament -> Batches
	Subscriptions          []TournamentSubscription `json:"subscribers,omitempty" gorm:"foreignKey:TournamentID"` // ✅ 1-to-many: Tournament -> Subscriptions
	SubscribersCount       int64                    `json:"subscribers_count,omitempty" gorm:"-"`
	ActiveSubscribersCount int64                    `json:"active_subscribers_count,omitempty" gorm:"-"`
	AvailableSlots         int64                    `json:"available_slots,omitempty" gorm:"-"`
}

// TournamentPhoto stores additional photos (max 5)
type TournamentPhoto struct {
	ID           string `json:"id" gorm:"primaryKey"`
	TournamentID string `json:"tournament_id" gorm:"not null;index"`
	URL          string `json:"url"` // ✅ R2 public URL
	Order        int    `json:"order" gorm:"default:0"`
}

// TournamentSubscription tracks user participation & payment metadata
type TournamentSubscription struct {
	ID               string    `json:"id" gorm:"primaryKey"`
	TournamentID     string    `json:"tournament_id" gorm:"not null;index"`
	TournamentUserID string    `json:"tournament_user_id" gorm:"not null;index"` // ← local FK
	ExternalUserID   string    `json:"external_user_id"`
	UserName         string    `json:"user_name"`
	UserAvatarURL    *string   `json:"user_avatar_url,omitempty"`
	JoinedAt         time.Time `json:"joined_at" gorm:"autoCreateTime"`
	// ✅ Payment Metadata (enhanced)
	PaymentID        string  `json:"payment_id"`                              // Unique identifier for the *payment* (e.g., Stripe payment_intent ID, Solana tx hash)
	PaymentAmount    float64 `json:"payment_amount"`                          // Actual amount *paid* (USD or token, same unit as EntryFee)
	PaymentStatus    string  `json:"payment_status" gorm:"default:'pending'"` // paid, pending, failed, refunded, waived
	TransactionID    string  `json:"transaction_id,omitempty"`                // Optional: raw blockchain tx hash (if applicable); may differ from PaymentID
	WaiverCodeUsed   string  `gorm:"type:varchar(64)"`                        // e.g., "WELCOME10"
	WaiverAmountUsed float64 `gorm:"default:0"`                               // e.g., 5.0
	WaiverIDUsed     string  `gorm:"type:uuid"`                               // links to user_waivers.id (nullable)
	// Optional: for audit & reconciliation
	PaymentMethod string     `json:"payment_method,omitempty"` // e.g., "solana", "stripe", "manual"
	PaymentAt     *time.Time `json:"payment_at,omitempty"`     // When payment was confirmed
}

// TournamentBatch groups rounds (e.g., "Qualifier Group A", "Grand Finals")
type TournamentBatch struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	TournamentID string    `json:"tournament_id" gorm:"not null;index"`
	Name         string    `json:"name"` // e.g., "Batch 1", "Europe Qualifier"
	Description  string    `json:"description"`
	Order        int       `json:"order" gorm:"default:0"` // Sorting order
	StartDate    time.Time `json:"start_date"`             // Optional override
	EndDate      time.Time `json:"end_date"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
	// Nested rounds (optional, but useful for preloading)
	Rounds []TournamentRound `json:"rounds,omitempty" gorm:"foreignKey:BatchID"`
}

// TournamentRound is a scoring phase within a batch
type TournamentRound struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	BatchID      string    `json:"batch_id" gorm:"not null;index"`
	TournamentID string    `json:"tournament_id" gorm:"not null;index"` // denormalized for perf
	Name         string    `json:"name"`                                // e.g., "Daily Dash", "Boss Rush"
	Description  string    `json:"description"`
	Order        int       `json:"order" gorm:"default:0"`          // Round 1, 2, 3...
	StartDate    time.Time `json:"start_date"`                      // When round opens for submissions
	EndDate      time.Time `json:"end_date"`                        // Hard deadline
	DurationMins int       `json:"duration_mins"`                   // Optional: max play time (for time-limited challenges)
	Status       string    `json:"status" gorm:"default:'pending'"` // pending → active → closed → finalized
	// Scoring logic hints (client/backend can interpret)
	ScoreType string    `json:"score_type"` // "highest", "sum", "average", "last", "best_of_n"
	Attempts  int       `json:"attempts"`   // max attempts per user (0 = unlimited)
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// LeaderboardEntry — populated by game server webhook or client submission
type LeaderboardEntry struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	TournamentID string    `json:"tournament_id" gorm:"index"`
	BatchID      string    `json:"batch_id,omitempty" gorm:"index"` // optional for filtering
	RoundID      string    `json:"round_id" gorm:"index"`           // ✅ critical!
	UserID       string    `json:"user_id" gorm:"index"`
	Score        int64     `json:"score"`
	Rank         int       `json:"rank"`
	SubmittedAt  time.Time `json:"submitted_at" gorm:"autoCreateTime"`
	Metadata     string    `json:"metadata"` // e.g., {"attempt": 3, "level": "final_boss"}
}

// MiniTournament represents a brief summary of a tournament for listing
type MiniTournament struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	StartTime    time.Time  `json:"start_time"`
	MainPhotoURL string     `json:"main_photo_url"`
	EntryFee     float64    `json:"entry_fee"`
	PrizePool    string     `json:"prize_pool"`
	SponsorName  string     `json:"sponsor_name"`
	IsFeatured   bool       `json:"is_featured"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	// Add more fields from the full tournament for the list view
	GameID         string    `json:"game_id"`
	Genre          string    `json:"genre,omitempty"`
	Description    string    `json:"description,omitempty"`
	MaxSubscribers int       `json:"max_subscribers"`
	EndTime        time.Time `json:"end_time,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Game           Game      `json:"game"`
}

type UserWaiver struct {
	ID            string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID        string     `gorm:"index;not null" json:"user_id"` // Links to TournamentUser.ID
	Code          string     `gorm:"not null;index" json:"code"`
	Title         string     `gorm:"not null" json:"title"`                   // e.g., "Welcome Bonus"
	Type          string     `gorm:"not null;default:'discount'" json:"type"` // e.g., 'discount', 'cashback', 'entry_fee_reduction'
	ImageURL      string     `gorm:"type:text" json:"image_url"`              // Optional image URL for badge
	Emoji         string     `gorm:"size:10" json:"emoji"`                    // Optional emoji for badge
	Excerpt       string     `gorm:"type:text" json:"excerpt"`                // Short description or note
	Amount        float64    `gorm:"not null" json:"amount"`                  // Max value of the waiver
	UsedAmount    float64    `gorm:"not null;default:0.0" json:"used_amount"`
	Description   string     `json:"description"` // Longer description
	IsActive      bool       `gorm:"default:true" json:"is_active"`
	IsViewed      bool       `gorm:"default:false" json:"is_viewed"`   // New flag
	IsRedeemed    bool       `gorm:"default:false" json:"is_redeemed"` // New flag: True if used at least once
	IsClaimed     bool       `gorm:"default:false" json:"is_claimed"`
	DurationHours int        `gorm:"default:168" json:"duration_hours"` // New: Hours from first use until expiry (default 7 days = 168h)
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"` // Can be set manually or computed from DurationHours on first use
	IssuedByID    string     `json:"issued_by_id"`         // Links to TournamentUser.ID of the issuer
}
