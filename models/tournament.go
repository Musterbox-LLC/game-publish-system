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
	AcceptsWaivers  bool       `json:"accepts_waivers" gorm:"default:true"`
	// Relationships (properly defined)
	Game                   Game                     `json:"game,omitempty" gorm:"foreignKey:GameID"`              // ✅ 1-to-many: Game -> Tournament
	Photos                 []TournamentPhoto        `json:"photos,omitempty" gorm:"foreignKey:TournamentID"`      // ✅ 1-to-many: Tournament -> Photos
	Batches                []TournamentBatch        `json:"batches,omitempty" gorm:"foreignKey:TournamentID"`     // ✅ 1-to-many: Tournament -> Batches
	Subscriptions          []TournamentSubscription `json:"subscribers,omitempty" gorm:"foreignKey:TournamentID"` // ✅ 1-to-many: Tournament -> Subscriptions
	SubscribersCount       int64                    `json:"subscribers_count,omitempty" gorm:"-"`
	ActiveSubscribersCount int64                    `json:"active_subscribers_count,omitempty" gorm:"-"`
	AvailableSlots         int64                    `json:"available_slots,omitempty" gorm:"-"`
}

type TournamentPhoto struct {
    ID           string `json:"id" gorm:"primaryKey"`
    TournamentID string `json:"tournament_id" gorm:"not null;index"`
    URL          string `json:"url"`
    SortOrder    int    `json:"sort_order" gorm:"column:sort_order;default:0"` // Changed from "order" to "sort_order"
}

type TournamentBatch struct {
    ID           string    `json:"id" gorm:"primaryKey"`
    TournamentID string    `json:"tournament_id" gorm:"not null;index"`
    Name         string    `json:"name"`
    Description  string    `json:"description"`
    SortOrder    int       `json:"sort_order" gorm:"column:sort_order;default:0"` // Changed from "order" to "sort_order"
    StartDate    time.Time `json:"start_date"`
    EndDate      time.Time `json:"end_date"`
    CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
    Rounds       []TournamentRound `json:"rounds,omitempty" gorm:"foreignKey:BatchID"`
}

type TournamentRound struct {
    ID           string    `json:"id" gorm:"primaryKey"`
    BatchID      string    `json:"batch_id" gorm:"not null;index"`
    TournamentID string    `json:"tournament_id" gorm:"not null;index"`
    Name         string    `json:"name"`
    Description  string    `json:"description"`
    SortOrder    int       `json:"sort_order" gorm:"column:sort_order;default:0"` // Changed from "order" to "sort_order"
    StartDate    time.Time `json:"start_date"`
    EndDate      time.Time `json:"end_date"`
    DurationMins int       `json:"duration_mins"`
    Status       string    `json:"status" gorm:"default:'pending'"`
    ScoreType    string    `json:"score_type"`
    Attempts     int       `json:"attempts"`
    CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
    UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TournamentSubscription tracks user participation & payment metadata
type TournamentSubscription struct {
	ID               string    `json:"id" gorm:"primaryKey"`
	TournamentID     string    `json:"tournament_id" gorm:"not null;index"`
	// Removed: TournamentUserID string
	ExternalUserID   string    `json:"external_user_id" gorm:"not null;index"` // ✅ Now the primary user identifier
	UserName         string    `json:"user_name"` // Denormalized from profile service
	UserAvatarURL    *string   `json:"user_avatar_url,omitempty"` // Denormalized from profile service
	JoinedAt         time.Time `json:"joined_at" gorm:"autoCreateTime"`
	// ✅ Payment Metadata (enhanced)
	PaymentID        string  `json:"payment_id"`                              // Unique identifier for the *payment* (e.g., Stripe payment_intent ID, Solana tx hash)
	PaymentAmount    float64 `json:"payment_amount"`                          // Actual amount *paid* (USD or token, same unit as EntryFee)
	PaymentStatus    string  `json:"payment_status" gorm:"default:'pending'"` // paid, pending, failed, refunded, waived
	TransactionID    string  `json:"transaction_id,omitempty"`                // Optional: raw blockchain tx hash (if applicable); may differ from PaymentID
	WaiverCodeUsed   string  `json:"waiver_code_used" gorm:"type:varchar(64)"`                        // e.g., "WELCOME10"
	WaiverAmountUsed float64 `json:"waiver_amount_used" gorm:"default:0"`                               // e.g., 5.0
	WaiverIDUsed     string  `json:"waiver_id_used" gorm:"type:uuid"`                               // links to user_waivers.id (nullable)
	// Optional: for audit & reconciliation
	PaymentMethod string     `json:"payment_method,omitempty"` // e.g., "solana", "stripe", "manual"
	PaymentAt     *time.Time `json:"payment_at,omitempty"`     // When payment was confirmed
	// Optional fields for status tracking (e.g., suspend, revoke)
	SuspendedAt     *time.Time `json:"suspended_at,omitempty"`
	SuspendedReason string     `json:"suspended_reason,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevokedReason   string     `json:"revoked_reason,omitempty"`
}

// LeaderboardEntry — populated by game server webhook or client submission
type LeaderboardEntry struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	TournamentID string    `json:"tournament_id" gorm:"index"`
	BatchID      string    `json:"batch_id,omitempty" gorm:"index"` // optional for filtering
	RoundID      string    `json:"round_id" gorm:"index"`           // ✅ critical!
	UserID       string    `json:"user_id" gorm:"index"`            // ✅ Now stores the external_user_id
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
    EndTime      time.Time  `json:"end_time"` // Added this
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
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
    Game           Game      `json:"game"`
}

type UserWaiver struct {
	ID            string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID        string     `gorm:"index;not null" json:"user_id"` // ✅ Now links to ExternalUserID directly
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
	IssuedByID    string     `json:"issued_by_id"`         // ✅ Now links to ExternalUserID of the issuer
}

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