package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"game-publish-system/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause" // ✅ Import clause
)

// WalletSyncClient now holds DB reference
type WalletSyncClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	DB         *gorm.DB // ✅ Add DB dependency
}

func NewWalletSyncClient(db *gorm.DB) *WalletSyncClient {
	baseURL := os.Getenv("SYNC_SERVICE_URL")
	if baseURL == "" {
		log.Fatal("SYNC_SERVICE_URL environment variable is required")
	}
	token := os.Getenv("GAME_SERVICE_TOKEN")
	if token == "" {
		log.Fatal("GAME_SERVICE_TOKEN environment variable is required for wallet sync")
	}

	return &WalletSyncClient{
		BaseURL: baseURL,
		Token:   token,
		DB:      db,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *WalletSyncClient) GetChangedWallets(ctx context.Context, since time.Time) ([]models.WalletMirror, error) {
	since = since.UTC()

	u, err := url.Parse(fmt.Sprintf("%s/api/v1/public/wallets", c.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("since", since.Format(time.RFC3339))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Service-Token", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call sync service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("sync service returned status %d: %s", resp.StatusCode, string(body))
	}

	// Decode directly into []models.WalletMirror (reuse same struct)
	var response struct {
		Wallets []models.WalletMirror `json:"wallets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode sync service response: %w", err)
	}

	return response.Wallets, nil
}

// PollWallets now persists to DB
func PollWallets(ctx context.Context, client *WalletSyncClient, pollInterval time.Duration) {
	log.Println("Starting wallet polling (DB-backed)...")
	lastSyncTime := time.Now().UTC().Add(-24 * time.Hour)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Wallet polling stopped.")
			return
		case <-ticker.C:
			logTime := time.Now().UTC()
			log.Printf("Polling for wallet changes since %s...", lastSyncTime.Format(time.RFC3339))

			wallets, err := client.GetChangedWallets(ctx, lastSyncTime)
			if err != nil {
				log.Printf("❌ Error polling wallets: %v", err)
				continue
			}

			count := len(wallets)
			log.Printf("📥 Received %d wallet change(s) from sync service.", count)

			if count == 0 {
				log.Println("➡️ No new wallet changes.")
				continue
			}

			// Batch upsert using GORM's Create with OnConflict (efficient & atomic)
			// Note: GORM's Create([]T) with OnConflict does bulk upsert in one statement (if DB supports it, e.g., PostgreSQL)
			if err := client.DB.Clauses(
				clause.OnConflict{
					Columns: []clause.Column{{Name: "address"}}, // unique constraint target
					DoUpdates: clause.AssignmentColumns([]string{
						"user_id",
						"chain",
						"token",
						"first_deposit_made",
						"derivation_index",
						"is_treasury",
						"is_active",
						"last_balance_check_at",
						"created_at",
						"updated_at",
					}),
				},
			).Create(&wallets).Error; err != nil {
				log.Printf("❌ Failed to upsert %d wallet(s) into wallet_mirror: %v", count, err)
				// Do NOT update lastSyncTime on failure — retry same window next tick
				continue
			}

			// ✅ Success: advance lastSyncTime to *now* to avoid reprocessing same batch
			// (We use logTime to avoid time skew from polling latency)
			lastSyncTime = logTime
			log.Printf("✅ Upserted %d wallet(s) into wallet_mirror table.", count)
		}
	}
}

// GetWalletByAddress now queries DB
func GetWalletByAddress(db *gorm.DB, address string) (models.WalletMirror, bool, error) {
	var wallet models.WalletMirror
	if err := db.Where("address = ?", address).First(&wallet).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return wallet, false, nil
		}
		return wallet, false, err
	}
	return wallet, true, nil
}

// GetAllWallets now queries DB (add pagination in production)
func GetAllWallets(db *gorm.DB) ([]models.WalletMirror, error) {
	var wallets []models.WalletMirror
	if err := db.Find(&wallets).Error; err != nil {
		return nil, err
	}
	return wallets, nil
}