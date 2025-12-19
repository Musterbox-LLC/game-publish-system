// main.go
package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"game-publish-system/handlers"
	"game-publish-system/middleware"
	"game-publish-system/models"
	"game-publish-system/services"
	"game-publish-system/utils"
	"game-publish-system/workers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, reading environment variables directly")
	}

	app := fiber.New(fiber.Config{
		BodyLimit: 5 * 1024 * 1024 * 1024, // 5GB
	})

	// 🔐❗ GLOBAL: Only Gateway requests allowed — no exceptions
	app.Use(middleware.GatewayAuthMiddleware())

	// Enhanced CORS configuration for mobile compatibility
	// Load allowed origins from environment variable
	allowedOriginsEnv := os.Getenv("ALLOWED_ORIGINS")
	if allowedOriginsEnv == "" {
		log.Println("⚠️  ALLOWED_ORIGINS environment variable not set, using default: http://localhost:3000")
		allowedOriginsEnv = "http://localhost:3000"
	}
	
	allowedOriginsList := strings.Split(allowedOriginsEnv, ",")
	for i, origin := range allowedOriginsList {
		allowedOriginsList[i] = strings.TrimSpace(origin)
	}
	
	allowedOriginsString := strings.Join(allowedOriginsList, ",")

	// Apply CORS middleware with specific configuration
	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOriginsString,
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH,HEAD",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Request-ID, User-Agent, Cache-Control, X-Session-Token, X-Service-Token, X-Device-ID",
		ExposeHeaders:    "Content-Length, Content-Type, Authorization, X-Request-ID, X-Access-Token, X-Refresh-Token, X-Otp-Not-Required",
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable not set")
	}

	if err := utils.InitR2(); err != nil {
		log.Fatal("failed to initialize R2 client:", err)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect to database:", err)
	}

	// 🔧 UPDATED: Include all new models for migration
	if err := db.AutoMigrate(
		&models.Game{},
		&models.GameScreenshot{},
		&models.GameVideo{},
		&models.Review{},
		
		// Tournament Models
		&models.Tournament{},
		&models.TournamentPhoto{},
		&models.TournamentSubscription{},
		&models.TournamentBatch{},
		&models.TournamentMatch{},       // Note: Changed from &models.Match{}
		&models.TournamentRound{},
		&models.LeaderboardEntry{},
		
		// User Models
		&models.UserWaiver{},
		&models.UserProgress{},
		&models.TournamentParticipation{},
		&models.BountyClaim{},
		&models.Referral{},
		
		// Badge & Reward Models
		&models.BadgeType{},
		&models.UserBadge{},
		&models.WalletMirror{},
		&models.Reward{},
		
		// 🔧 NEW: Pairing System Models
		&models.MatchTypeConfig{},
		&models.MatchPairing{},
		&models.PlayerSeeding{},
	); err != nil {
		log.Fatal("failed to migrate database:", err)
	}

	if err := utils.EnsureUploadDir(); err != nil {
		log.Fatal("failed to ensure upload dir:", err)
	}

	// Initialize services
	gameService := services.NewGameService(db)
	tournamentService := services.NewTournamentService(db)
	
	// 🔧 NEW: Initialize Pairing Service
	pairingService := services.NewPairingService(db)
	
	progressionService := services.NewProgressionService(db)
	badgeService := services.NewBadgeService(db)
	rewardService := services.NewRewardService(db)

	// Sync Service Configuration
	syncServiceURL := os.Getenv("SYNC_SERVICE_URL")
	if syncServiceURL == "" {
		log.Fatal("SYNC_SERVICE_URL environment variable not set")
	}
	gameServiceToken := os.Getenv("GAME_SERVICE_TOKEN")
	if gameServiceToken == "" {
		log.Fatal("GAME_SERVICE_TOKEN environment variable not set")
	}

	// Initialize workers
	syncWorker := workers.NewTournamentUserSyncWorker(db, syncServiceURL, "/api/v1/public/profiles", gameServiceToken)

	// Initialize Wallet Sync Client
	walletSyncClient := workers.NewWalletSyncClient(db)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go workers.PollWallets(ctx, walletSyncClient, 10*time.Second)

	// Start Tournament User Sync Worker
	go func() {
		log.Println("Starting Tournament User Sync Worker...")
		syncWorker.Start(ctx)
	}()

	// Start Game Service Publish Scheduler
	gameService.StartPublishScheduler()

	// ✅ Setup routes — now with pairing service
	handlers.SetupGameRoutes(app, gameService)
	
	// 🔧 UPDATED: Pass both tournamentService and pairingService
	handlers.SetupTournamentRoutes(app, tournamentService, pairingService)
	
	handlers.SetupProgressionRoutes(app, progressionService, badgeService)

	// Initialize Auth Service Client for SSE
	authServiceURL := os.Getenv("AUTH_SERVICE_URL")
	authServiceToken := os.Getenv("MS_SERVICE_TOKEN")
	if authServiceURL == "" || authServiceToken == "" {
		log.Fatal("❌ AUTH_SERVICE_URL and MS_SERVICE_TOKEN are required for SSE auth (e.g., /user/rewards/stream)")
	}
	authClient := services.NewAuthServiceClient(authServiceURL, authServiceToken)
	log.Printf("✅ Auth service client initialized for SSE: %s", authServiceURL)

	// Setup Reward Routes
	handlers.SetupRewardRoutes(app, rewardService, authClient)

	// Static file serving
	app.Static("/uploads", "./uploads")

	// Web3GL static file serving
	app.Use("/web3gl", func(c *fiber.Ctx) error {
		originalPath := c.Path()
		relativePath := strings.TrimPrefix(originalPath, "/web3gl")
		if relativePath == "" {
			relativePath = "/"
		}

		decodedPath, err := url.PathUnescape(relativePath)
		if err != nil {
			log.Printf("Error decoding path: %v", err)
			return c.Status(fiber.StatusNotFound).SendString("File not found")
		}

		ext := filepath.Ext(originalPath)
		if ext == ".br" {
			c.Set("Content-Encoding", "br")
		}

		c.Path(decodedPath)

		return filesystem.New(filesystem.Config{
			Root:         http.Dir("./public/web3gl"),
			PathPrefix:   "",
			Index:        "index.html",
			MaxAge:       3600,
			NotFoundFile: "index.html",
		})(c)
	})

	// Ensure web3gl directory exists
	if err := os.MkdirAll("./public/web3gl", os.ModePerm); err != nil {
		log.Fatal("failed to ensure web3gl public dir:", err)
	}

	// Start server
	go func() {
		if err := app.Listen(":5200"); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Startup logs
	log.Println("✅ Server running on http://localhost:5200")
	log.Println("✅ Tournament User Sync Worker running")
	log.Println("✅ Wallet polling running (every 10s)")
	log.Println("✅ GatewayAuthMiddleware enforced globally — all requests must come from Gateway")
	log.Printf("✅ CORS configured for origins: %s", allowedOriginsString)
	log.Println("✅ Pairing Service initialized with complete workflow support")
	log.Println("✅ Match Type Configuration available at /match-types endpoint")

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("Shutting down server...")
	
	// Give server time to shutdown gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}
	
	log.Println("Server shutdown complete")
}