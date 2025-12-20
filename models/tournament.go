package models

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// Tournament represents a leaderboard-style tournament
// Tournament represents a leaderboard-style tournament
type Tournament struct {
	ID              string     `json:"id" gorm:"primaryKey"`
	GameID          string     `json:"game_id" gorm:"not null"`
	Name            string     `json:"name" gorm:"not null"`
	Description     string     `json:"description"`
	Rules           string     `json:"rules"`
	Guidelines      string     `json:"guidelines"`
	Genre           string     `json:"genre"`
	GenreTags       string     `json:"genre_tags" gorm:"column:genre_tags"`
	MaxSubscribers  int        `json:"max_subscribers" gorm:"default:0"`
	EntryFee        float64    `json:"entry_fee" gorm:"default:0"`
	MainPhotoURL    string     `json:"main_photo_url"`
	Status          string     `json:"status" gorm:"default:'draft'"`
	StartTime       time.Time  `json:"start_time" gorm:"not null"`
	EndTime         time.Time  `json:"end_time"`
	CreatedAt       time.Time  `json:"created_at" gorm:"autoCreateTime"`
    DeletedAt        gorm.DeletedAt    `json:"deleted_at,omitempty" gorm:"index"` 
	UpdatedAt       time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
	PublishedAt     *time.Time `json:"published_at,omitempty" gorm:"index"`
	PrizePool       string     `json:"prize_pool"`
	Requirements    string     `json:"requirements" gorm:"type:text"`
	SponsorName     string     `json:"sponsor_name"`
	IsFeatured      bool       `json:"is_featured" gorm:"default:false"`
	FeaturedOrder   int        `json:"featured_order" gorm:"default:0"`
	FeaturedAt      *time.Time `json:"featured_at,omitempty"`
	PublishSchedule *time.Time `json:"publish_schedule,omitempty"`
	AcceptsWaivers  bool       `json:"accepts_waivers" gorm:"default:true"`

	// Relationships
	Game          Game                     `json:"game,omitempty" gorm:"foreignKey:GameID"`
	Photos        []TournamentPhoto        `json:"photos,omitempty" gorm:"foreignKey:TournamentID"`
	Batches       []TournamentBatch        `json:"batches,omitempty" gorm:"foreignKey:TournamentID"`
	Subscriptions []TournamentSubscription `json:"subscribers,omitempty" gorm:"foreignKey:TournamentID"`

	// Calculated fields (not stored in DB)
	SubscribersCount       int64 `json:"subscribers_count,omitempty" gorm:"-"`
	ActiveSubscribersCount int64 `json:"active_subscribers_count,omitempty" gorm:"-"`
	AvailableSlots         int64 `json:"available_slots,omitempty" gorm:"-"`
}

// TournamentBatch contains Matches
type TournamentBatch struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	TournamentID string    `json:"tournament_id" gorm:"not null;index"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	SortOrder    int       `json:"sort_order" gorm:"column:sort_order;default:0"`
	StartDate    time.Time `json:"start_date"`
	EndDate      time.Time `json:"end_date"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`

	// Relationship: One Batch has many Matches
	Matches []TournamentMatch `json:"matches,omitempty" gorm:"foreignKey:BatchID"`
}

// Add to your models/tournament.go

// MatchTypeConfig stores the configuration for different match types
type MatchTypeConfig struct {
	ID          string `json:"id" gorm:"primaryKey"`
	MatchType   string `json:"match_type" gorm:"not null;index"` // e.g., "SINGLE_ELIMINATION_1V1", "LEADERBOARD_CHALLENGE"
	Name        string `json:"name" gorm:"not null"`
	Description string `json:"description"`

	// Match configuration
	MatchFormat     string `json:"match_format"`     // HEAD_TO_HEAD, FREE_FOR_ALL, TEAM_BASED
	ProgressionType string `json:"progression_type"` // SINGLE_ELIMINATION, DOUBLE_ELIMINATION, ROUND_ROBIN, LEADERBOARD, etc.
	AdvancementRule string `json:"advancement_rule"` // WINNER_ADVANCES, TOP_N_ADVANCE, POINTS_BASED, etc.
	EliminationRule string `json:"elimination_rule"` // IMMEDIATE_ELIMINATION, N_LOSSES_ELIMINATION, etc.

	// Player configuration
	DefaultPlayerCountMin int `json:"default_player_count_min" gorm:"default:2"`
	DefaultPlayerCountMax int `json:"default_player_count_max" gorm:"default:2"`
	PlayersPerTeam        int `json:"players_per_team" gorm:"default:1"`
	NumberOfTeams         int `json:"number_of_teams" gorm:"default:2"`

	// Round configuration
	DefaultRoundsPerMatch int `json:"default_rounds_per_match" gorm:"default:1"`

	// Seeding configuration
	SeedingMethod string `json:"seeding_method"` // RANDOM, RANK_BASED, SKILL_BASED, CUSTOM

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// MatchPairing stores proposed pairings before approval
type MatchPairing struct {
	ID           string `json:"id" gorm:"primaryKey"`
	MatchID      string `json:"match_id" gorm:"not null;index"`
	TournamentID string `json:"tournament_id" gorm:"not null;index"`
	BatchID      string `json:"batch_id" gorm:"not null;index"`

	// Pairing configuration
	PairingType   string `json:"pairing_type"`   // AUTO, MANUAL, HYBRID
	SeedingMethod string `json:"seeding_method"` // RANDOM, RANK_BASED, SKILL_BASED
	AlgorithmUsed string `json:"algorithm_used"` // Swiss, RoundRobin, Elimination, etc.

	// Pairing data (stored as JSON)
	PairsJSON    string `json:"pairs_json" gorm:"type:jsonb"`    // Store the actual pairings
	MetadataJSON string `json:"metadata_json" gorm:"type:jsonb"` // Additional metadata

	// Status
	Status  string `json:"status" gorm:"default:'pending'"` // pending, proposed, approved, rejected, published
	Version int    `json:"version" gorm:"default:1"`

	// Audit fields
	ProposedBy  string `json:"proposed_by"`  // User ID who proposed the pairing
	ApprovedBy  string `json:"approved_by"`  // User ID who approved
	RejectedBy  string `json:"rejected_by"`  // User ID who rejected
	PublishedBy string `json:"published_by"` // User ID who published

	// Timestamps
	ProposedAt  time.Time  `json:"proposed_at" gorm:"autoCreateTime"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty"`
	RejectedAt  *time.Time `json:"rejected_at,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`

	// Reasons
	RejectionReason string `json:"rejection_reason,omitempty"`

	// Relationships
	Match      TournamentMatch `json:"match,omitempty" gorm:"foreignKey:MatchID"`
	Tournament Tournament      `json:"tournament,omitempty" gorm:"foreignKey:TournamentID"`
	Batch      TournamentBatch `json:"batch,omitempty" gorm:"foreignKey:BatchID"`
}

// PlayerSeeding stores seeding information for players in a tournament
type PlayerSeeding struct {
	ID           string `json:"id" gorm:"primaryKey"`
	TournamentID string `json:"tournament_id" gorm:"not null;index"`
	UserID       string `json:"user_id" gorm:"not null;index"` // ExternalUserID
	UserName     string `json:"user_name"`

	// Seeding data
	SeedNumber   int     `json:"seed_number" gorm:"default:0"` // 0 = unseeded
	SkillRating  float64 `json:"skill_rating" gorm:"default:0"`
	PreviousRank int     `json:"previous_rank" gorm:"default:0"`

	// Statistics for seeding
	WinCount     int     `json:"win_count" gorm:"default:0"`
	LossCount    int     `json:"loss_count" gorm:"default:0"`
	TotalScore   int64   `json:"total_score" gorm:"default:0"`
	AverageScore float64 `json:"average_score" gorm:"default:0"`

	// Metadata
	SeedingMethod string `json:"seeding_method"` // MANUAL, AUTOMATIC, HYBRID
	Notes         string `json:"notes"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	// Relationships
	Tournament Tournament `json:"tournament,omitempty" gorm:"foreignKey:TournamentID"`
}

// Add match_type field to TournamentMatch
// Update the TournamentMatch struct to include:
type TournamentMatch struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	BatchID     string    `json:"batch_id" gorm:"not null;index"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status" gorm:"default:'pending'"`
	SortOrder   int       `json:"sort_order" gorm:"column:sort_order;default:0"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	// NEW: Match type configuration
	MatchType string `json:"match_type" gorm:"not null;default:'SINGLE_ELIMINATION_1V1'"`

	// Optional: For 1v1 matches
	WinnerID   string `json:"winner_id,omitempty"`
	WinnerName string `json:"winner_name,omitempty"`

	// NEW: Pairing reference
	CurrentPairingID string `json:"current_pairing_id,omitempty"`

	// Relationship: One Match has many Rounds
	Rounds []TournamentRound `json:"rounds,omitempty" gorm:"foreignKey:MatchID"`

	// NEW: Relationship to pairing
	CurrentPairing *MatchPairing `json:"current_pairing,omitempty" gorm:"foreignKey:CurrentPairingID"`
}

// TournamentRound is the smallest scoring unit
type TournamentRound struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	MatchID      string    `json:"match_id" gorm:"not null;index"`
	BatchID      string    `json:"batch_id" gorm:"not null;index"`
	TournamentID string    `json:"tournament_id" gorm:"not null;index"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	SortOrder    int       `json:"sort_order" gorm:"column:sort_order;default:0"`
	StartDate    time.Time `json:"start_date"`
	EndDate      time.Time `json:"end_date"`
	DurationMins int       `json:"duration_mins"`
	Status       string    `json:"status" gorm:"default:'pending'"`
	ScoreType    string    `json:"score_type"` // "highest", "sum", "average", etc.
	Attempts     int       `json:"attempts" gorm:"default:1"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

type TournamentPhoto struct {
	ID           string `json:"id" gorm:"primaryKey"`
	TournamentID string `json:"tournament_id" gorm:"not null;index"`
	URL          string `json:"url"`
	SortOrder    int    `json:"sort_order" gorm:"column:sort_order;default:0"` // Changed from "order" to "sort_order"
}

// TournamentSubscription tracks user participation & payment metadata
type TournamentSubscription struct {
	ID           string `json:"id" gorm:"primaryKey"`
	TournamentID string `json:"tournament_id" gorm:"not null;index"`
	// Removed: TournamentUserID string
	ExternalUserID string    `json:"external_user_id" gorm:"not null;index"` // ✅ Now the primary user identifier
	UserName       string    `json:"user_name"`                              // Denormalized from profile service
	UserAvatarURL  *string   `json:"user_avatar_url,omitempty"`              // Denormalized from profile service
	JoinedAt       time.Time `json:"joined_at" gorm:"autoCreateTime"`
	// ✅ Payment Metadata (enhanced)
	PaymentID        string  `json:"payment_id"`                               // Unique identifier for the *payment* (e.g., Stripe payment_intent ID, Solana tx hash)
	PaymentAmount    float64 `json:"payment_amount"`                           // Actual amount *paid* (USD or token, same unit as EntryFee)
	PaymentStatus    string  `json:"payment_status" gorm:"default:'pending'"`  // paid, pending, failed, refunded, waived
	TransactionID    string  `json:"transaction_id,omitempty"`                 // Optional: raw blockchain tx hash (if applicable); may differ from PaymentID
	WaiverCodeUsed   string  `json:"waiver_code_used" gorm:"type:varchar(64)"` // e.g., "WELCOME10"
	WaiverAmountUsed float64 `json:"waiver_amount_used" gorm:"default:0"`      // e.g., 5.0
	WaiverIDUsed     string  `json:"waiver_id_used" gorm:"type:uuid"`          // links to user_waivers.id (nullable)
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
	MatchID      string    `json:"match_id,omitempty" gorm:"index"` // optional for filtering
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
	UserID        string     `gorm:"index;not null" json:"user_id"` // ✅ Links to ExternalUserID directly
	Code          string     `gorm:"not null;index" json:"code"`
	Title         string     `gorm:"not null" json:"title"`                     // e.g., "Welcome Bonus", "Respawn Token"
	Type          string     `gorm:"not null;default:'discount'" json:"type"`   // 'discount', 'cashback', 'entry_fee_reduction', 'respawn', 'custom'
	Amount        float64    `gorm:"default:0.0" json:"amount"`                 // Max value (e.g., $10 discount)
	UsedAmount    float64    `gorm:"not null;default:0.0" json:"used_amount"`   // Amount already applied (e.g., $5 of $10 used)
	UsesRemaining *int       `gorm:"default:1" json:"uses_remaining,omitempty"` // Nullable: Num uses left. NULL = unlimited. 1 = single use.
	MaxUses       *int       `gorm:"default:1" json:"max_uses,omitempty"`       // Nullable: Total allowed uses. NULL = unlimited.
	ImageURL      string     `gorm:"type:text" json:"image_url"`                // Optional image URL for badge
	Emoji         string     `gorm:"size:10" json:"emoji"`                      // Optional emoji for badge
	Excerpt       string     `gorm:"type:text" json:"excerpt"`                  // Short description or note
	Description   string     `json:"description"`                               // Longer description
	IsActive      bool       `gorm:"default:true" json:"is_active"`
	IsViewed      bool       `gorm:"default:false" json:"is_viewed"`    // New flag
	IsRedeemed    bool       `gorm:"default:false" json:"is_redeemed"`  // True if used at least once / fully consumed based on logic
	IsClaimed     bool       `gorm:"default:false" json:"is_claimed"`   // Acknowledged/revealed
	DurationHours int        `gorm:"default:168" json:"duration_hours"` // Hours from first *use* until expiry (default 7 days = 168h)
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`              // Set manually or computed from DurationHours on first use/hard expiry
	IssuedByID    string     `json:"issued_by_id"`                      // Links to ExternalUserID of the issuer
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// TournamentParticipation = subscription + activity summary
type TournamentParticipation struct {
	ID             string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ExternalUserID string `gorm:"index;not null" json:"external_user_id"`
	TournamentID   string `gorm:"index;not null" json:"tournament_id"`
	SubscriptionID string `gorm:"index;not null" json:"subscription_id"` // links to TournamentSubscription.ID

	// Engagement
	TotalMatchesPlayed int64 `json:"total_matches_played" gorm:"default:0"`
	BestScore          int64 `json:"best_score" gorm:"default:0"`
	FinalRank          int   `json:"final_rank" gorm:"default:0"` // 0 = not ranked

	// XP & rewards
	XPEarned     int64  `json:"xp_earned" gorm:"default:0"`
	PrizeEarned  string `json:"prize_earned,omitempty"` // e.g., "100 USDC"
	BadgeAwarded string `json:"badge_awarded,omitempty"`

	// Status
	Status string `json:"status" gorm:"type:varchar(16);default:'joined'"` // joined → active → completed → disqualified

	Timestamps
}

// BeforeCreate hook to enforce rules based on Type
func (w *UserWaiver) BeforeCreate(tx *gorm.DB) error {
	// Ensure Type is one of the known values
	supportedTypes := map[string]bool{
		"discount":            true,
		"cashback":            true,
		"entry_fee_reduction": true,
		"respawn":             true, // Add the new type
		"custom":              true, // For flexibility
	}
	if !supportedTypes[w.Type] {
		return errors.New("unsupported waiver type: " + w.Type)
	}

	// For 'respawn' type, Amount is likely irrelevant. Ensure it's zero.
	if w.Type == "respawn" {
		w.Amount = 0.0
		// UsesRemaining and MaxUses become crucial for respawn.
		// Default of 1 use makes sense for a single-respawn token.
		// If UsesRemaining is explicitly set to 0 or negative, it might indicate an error during creation.
		if w.UsesRemaining != nil && *w.UsesRemaining <= 0 {
			return errors.New("respawn waiver must have at least 1 use remaining")
		}
	}

	// For financial types, UsesRemaining/MaxUses are less relevant, maybe set them to NULL or 1.
	if w.Type == "discount" || w.Type == "cashback" || w.Type == "entry_fee_reduction" {
		// GORM will set default 1 if not specified. For purely monetary waivers,
		// setting them explicitly to NULL might be cleaner if they are not used.
		// However, leaving the default 1 is also acceptable if the business logic handles it.
		// Let's keep the default for now unless specific logic requires NULL.
	}

	return nil
}

func (w *UserWaiver) ConsumeUsage(tx *gorm.DB, amountToConsume float64) error {
	// 1. Check general validity
	now := time.Now()
	if !w.IsActive {
		return errors.New("waiver is inactive")
	}
	if w.ExpiresAt != nil && now.After(*w.ExpiresAt) {
		return errors.New("waiver has expired")
	}
	if w.IsRedeemed && w.Type != "respawn" {
		// For non-respawn types, IsRedeemed might mean fully used up based on Amount.
		// For respawn, IsRedeemed could mean it was used once, but UsesRemaining tracks further uses.
		return errors.New("waiver is marked as fully redeemed")
	}

	// 2. Type-specific consumption logic
	switch w.Type {
	case "discount", "cashback", "entry_fee_reduction":
		// Financial type: consume based on amount
		newUsedAmount := w.UsedAmount + amountToConsume
		if newUsedAmount > w.Amount {
			return errors.New("attempting to consume more than the waiver's total amount")
		}
		w.UsedAmount = newUsedAmount
		if w.UsedAmount >= w.Amount {
			w.IsRedeemed = true // Mark as fully consumed financially
		}

	case "respawn":
		// Gameplay type: consume based on usage count
		if w.UsesRemaining == nil {
			// If UsesRemaining is NULL, it means unlimited uses for this specific instance.
			// You might want to track usage elsewhere or consider this an error if decrementation is expected.
			// For now, let's assume NULL means infinite, so no decrement is needed.
			// If the intent was to decrement even NULL, this logic needs adjustment.
			// Let's assume the caller checks if UsesRemaining > 0 before calling if it matters.
			// If UsesRemaining is NULL, we proceed assuming it's okay to use (infinite).
			// If it's not NULL, we decrement.
			if w.UsesRemaining != nil { // This check is redundant now, kept for clarity if logic changes
				usesLeft := *w.UsesRemaining
				if usesLeft <= 0 {
					return errors.New("respawn waiver has no uses remaining")
				}
				newUsesLeft := usesLeft - 1
				w.UsesRemaining = &newUsesLeft
				if newUsesLeft <= 0 {
					w.IsRedeemed = true // Mark as fully consumed gameplay-wise
				}
			}
			// If UsesRemaining was NULL, we don't change it, implying infinite use.
			// The caller should handle this case appropriately.
		} else {
			usesLeft := *w.UsesRemaining
			if usesLeft <= 0 {
				return errors.New("respawn waiver has no uses remaining")
			}
			newUsesLeft := usesLeft - 1
			w.UsesRemaining = &newUsesLeft
			if newUsesLeft <= 0 {
				w.IsRedeemed = true // Mark as fully consumed gameplay-wise
			}
		}

	case "custom":
		// Handle custom logic here if needed, potentially based on Description or other fields.
		// For now, return an error as it's not implemented.
		return errors.New("consumption logic for 'custom' waiver type not implemented")

	default:
		return errors.New("unknown waiver type: " + w.Type)
	}

	// 3. Update the waiver record in the provided transaction
	updates := map[string]interface{}{
		"used_amount":    w.UsedAmount,
		"uses_remaining": w.UsesRemaining,
		"is_redeemed":    w.IsRedeemed,
		"updated_at":     time.Now(),
	}
	// If this is the first use, potentially set ExpiresAt based on DurationHours
	if (w.Type == "discount" || w.Type == "cashback" || w.Type == "entry_fee_reduction") && w.UsedAmount > 0 && w.ExpiresAt == nil && w.DurationHours > 0 {
		expiryTime := time.Now().Add(time.Duration(w.DurationHours) * time.Hour)
		updates["expires_at"] = &expiryTime
	} else if w.Type == "respawn" && w.UsesRemaining != nil && *w.UsesRemaining < *w.MaxUses && w.ExpiresAt == nil && w.DurationHours > 0 {
		// For respawn, expiry might be set on first *use* or creation, depending on rules.
		// This example sets it on first decrement of UsesRemaining.
		// Adjust logic as per your specific requirement for respawn expiry.
		if *w.UsesRemaining == *w.MaxUses-1 { // Meaning this is the *first* use
			expiryTime := time.Now().Add(time.Duration(w.DurationHours) * time.Hour)
			updates["expires_at"] = &expiryTime
		}
	}

	result := tx.Model(w).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// This could happen if the record was concurrently modified.
		return errors.New("failed to update waiver, record might have changed")
	}

	return nil
}
