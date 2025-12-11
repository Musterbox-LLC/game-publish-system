// workers/tournament_user_sync_worker.go
package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"game-publish-system/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MirroredUserFromProfile matches the JSON response from the new sync service.
type MirroredUserFromProfile struct {
	ID                    string    `json:"id"`
	ExternalID            string    `json:"external_id"`
	Username              string    `json:"username"`
	Email                 string    `json:"email"`
	FirstName             *string   `json:"first_name,omitempty"`
	LastName              *string   `json:"last_name,omitempty"`
	Bio                   *string   `json:"bio,omitempty"`
	ProfilePictureURL     *string   `json:"profile_picture_url,omitempty"`
	CoverPhotoURL         *string   `json:"cover_photo_url,omitempty"`
	AccountStatus         string    `json:"account_status"`
	EmailVerified         bool      `json:"email_verified"`
	ProfileCompletion     bool      `json:"profile_completion"`
	ProfileCompletionStat string    `json:"profile_completion_status"`
	PreferredLanguage     *string   `json:"preferred_language,omitempty"`
	ReferredByID          *string   `json:"referred_by_id,omitempty"`
	ReferralCode          string    `json:"referral_code"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// GetUserChangesResponse is the top-level structure of the sync service response.
type GetUserChangesResponse struct {
	Users []MirroredUserFromProfile `json:"users"`
}

type TournamentUserSyncWorker struct {
	db           *gorm.DB
	interval     time.Duration
	baseURL      string // e.g., "http://localhost:8500"
	endpointPath string // e.g., "/api/v1/public/profiles"
	serviceToken string
	httpClient   *http.Client
}

// NewTournamentUserSyncWorker now requires the sync service URL and its own service token.
func NewTournamentUserSyncWorker(db *gorm.DB, syncServiceBaseURL, endpointPath, gameServiceToken string) *TournamentUserSyncWorker {
	return &TournamentUserSyncWorker{
		db:           db,
		interval:     1 * time.Minute,
		baseURL:      syncServiceBaseURL,
		endpointPath: endpointPath,
		serviceToken: gameServiceToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (w *TournamentUserSyncWorker) Start(ctx context.Context) {
	log.Println("🔁 Starting Tournament User Sync Worker (sync-service → tournament_users)…")
	go w.run(ctx)
}

func (w *TournamentUserSyncWorker) run(ctx context.Context) {
	// Initial sync (backfill if needed) - sync from the beginning of time
	if err := w.syncBatch(ctx, time.Time{}); err != nil {
		log.Printf("⚠️ Initial sync failed: %v", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// For incremental syncs, we use the last update time from our local table
			lastSyncTime := w.getLastSyncTime()
			if err := w.syncBatch(ctx, lastSyncTime); err != nil {
				log.Printf("❌ Sync batch failed: %v", err)
			}
		case <-ctx.Done():
			log.Println("⏹️ Tournament User Sync Worker stopped")
			return
		}
	}
}

// getLastSyncTime finds the most recent UpdatedAt from our local TournamentUser table.
func (w *TournamentUserSyncWorker) getLastSyncTime() time.Time {
	var lastTime time.Time
	// Use a subquery or raw SQL if GORM's MAX is problematic with time.Time
	err := w.db.Raw("SELECT MAX(updated_at) FROM tournament_users WHERE deleted_at IS NULL").Scan(&lastTime).Error
	if err != nil || lastTime.IsZero() {
		return time.Unix(0, 0) // Fallback to epoch if no records or error
	}
	return lastTime
}

// syncBatch fetches user changes from the new sync service and updates the local TournamentUser table.
func (w *TournamentUserSyncWorker) syncBatch(ctx context.Context, since time.Time) error {
	sinceStr := since.UTC().Format(time.RFC3339) // Normalize to UTC for consistency
	log.Printf("[SYNC] 📡 Fetching user changes from sync service since=%s (local=%v)", sinceStr, since)

	// Validate and parse base URL only once (construction-time validation is better, but we double-check)
	base, err := url.Parse(w.baseURL)
	if err != nil {
		return fmt.Errorf("invalid base sync service URL '%s': %w", w.baseURL, err)
	}

	// Safely join base URL and endpoint path (handles trailing/leading slashes)
	endpointURL := base.JoinPath(w.endpointPath)

	// Add 'since' query param
	q := endpointURL.Query()
	q.Set("since", sinceStr)
	endpointURL.RawQuery = q.Encode()
	finalURL := endpointURL.String()

	log.Printf("[SYNC] ➡️  GET %s", finalURL)

	req, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request to %s: %w", finalURL, err)
	}

	// Authenticate with dedicated service token
	req.Header.Set("X-Service-Token", w.serviceToken)
	// Optional: Log masked token for debug (only if safe in your env)
	// log.Printf("[SYNC] 🔑 Using X-Service-Token: %s***", w.serviceToken[:4])

	resp, err := w.httpClient.Do(req)
	if err != nil {
		log.Printf("[SYNC] ❌ Request to %s failed: %v", finalURL, err)
		return fmt.Errorf("HTTP request to sync service failed: %w", err)
	}
	defer func() {
		// Always drain & close to prevent connection leaks
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		// Read limited error body (avoid massive payloads)
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if readErr != nil {
			log.Printf("[SYNC] ⚠️ Failed to read error body from %s: %v", finalURL, readErr)
		}
		errMsg := string(body)
		log.Printf("[SYNC] ❌ Sync service returned %d for %s: %s", resp.StatusCode, finalURL, errMsg)
		return fmt.Errorf("sync service non-200 response: %d — %s", resp.StatusCode, errMsg)
	}

	var response GetUserChangesResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Printf("[SYNC] ❌ Failed to decode JSON response from %s: %v", finalURL, err)
		return fmt.Errorf("failed to decode sync service response: %w", err)
	}

	if len(response.Users) == 0 {
		log.Printf("[SYNC] ✅ No user changes received since %s", sinceStr)
		return nil
	}

	log.Printf("[SYNC] 📥 Processing %d user(s) from sync service…", len(response.Users))

	var upsertCount, errorCount int
	for _, remoteUser := range response.Users {
		localUser := models.TournamentUser{
			ExternalUserID:    remoteUser.ExternalID,
			Username:          remoteUser.Username,
			Email:             remoteUser.Email,
			ProfilePictureURL: remoteUser.ProfilePictureURL,
			CoverPhotoURL:     remoteUser.CoverPhotoURL,
			FirstName:         remoteUser.FirstName,
			LastName:          remoteUser.LastName,
			Bio:               remoteUser.Bio,
			CreatedAt:         remoteUser.CreatedAt,
			UpdatedAt:         remoteUser.UpdatedAt,
		}

		// Optional: soft-delete handling (e.g., if account_status signals deactivation)
		// if remoteUser.AccountStatus == "deactivated" || remoteUser.AccountStatus == "suspended" {
		// 	localUser.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
		// }

		if err := w.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "external_user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"username", "email", "profile_picture_url", "cover_photo_url",
				"first_name", "last_name", "bio", "created_at", "updated_at",
				// Add "deleted_at" here if you implement soft-delete sync
			}),
		}).Create(&localUser).Error; err != nil {
			errorCount++
			log.Printf("[SYNC] ⚠️ Failed to upsert tournament_user (external_id=%q, username=%q): %v",
				remoteUser.ExternalID, remoteUser.Username, err)
		} else {
			upsertCount++
		}
	}

	// Determine latest update for logging
	var latestUpdate time.Time
	var latestUserID string
	for _, u := range response.Users {
		if u.UpdatedAt.After(latestUpdate) {
			latestUpdate = u.UpdatedAt
			latestUserID = u.ExternalID
		}
	}

	if latestUpdate.IsZero() {
		log.Printf("[SYNC] ✅ Synced %d user(s) (0 updates detected in timestamps)", len(response.Users))
	} else {
		log.Printf("[SYNC] ✅ Synced %d users (%d upserted, %d errors). Latest: external_id=%s, updated_at=%v",
			len(response.Users), upsertCount, errorCount, latestUserID, latestUpdate.Format(time.RFC3339))
	}

	return nil
}
