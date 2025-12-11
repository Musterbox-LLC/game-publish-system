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
	"game-publish-system/workers" // Import the workers package

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
		allowedOriginsEnv = "http://localhost:3000" // Provide a default or handle appropriately
	}
	// Split the comma-separated string into a slice
	allowedOriginsList := strings.Split(allowedOriginsEnv, ",")
	// Trim spaces from each origin
	for i, origin := range allowedOriginsList {
		allowedOriginsList[i] = strings.TrimSpace(origin)
	}
	// Join the slice back into a string for Fiber's CORS config
	allowedOriginsString := strings.Join(allowedOriginsList, ",")

	// Apply CORS middleware with specific configuration
	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOriginsString, // Use the loaded origins
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH,HEAD",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Request-ID, User-Agent, Cache-Control, X-Session-Token, X-Service-Token, X-Device-ID", // Added X-Device-ID which is common
		ExposeHeaders:    "Content-Length, Content-Type, Authorization, X-Request-ID, X-Access-Token, X-Refresh-Token, X-Otp-Not-Required", // Added common headers returned by your system
		AllowCredentials: true, // Set to true if credentials (cookies, authorization headers) need to be included in requests
		MaxAge:           86400, // 24 hours
	}))

	// ... rest of the main function remains the same ...

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

	if err := db.AutoMigrate(
		&models.Game{},
		&models.GameScreenshot{},
		&models.GameVideo{},
		&models.Review{},
		&models.Tournament{},
		&models.TournamentPhoto{},
		&models.TournamentSubscription{},
		&models.TournamentBatch{},
		&models.TournamentRound{},
		&models.LeaderboardEntry{},
		&models.TournamentUser{},
		&models.UserWaiver{},
		&models.UserProgress{},
		&models.Match{},
		&models.TournamentParticipation{},
		&models.BountyClaim{},
		&models.Referral{},
		&models.BadgeType{},
		&models.UserBadge{},
		&models.WalletMirror{},
	); err != nil {
		log.Fatal("failed to migrate database:", err)
	}

	if err := utils.EnsureUploadDir(); err != nil {
		log.Fatal("failed to ensure upload dir:", err)
	}

	gameService := services.NewGameService(db)
	tournamentService := services.NewTournamentService(db)
	progressionService := services.NewProgressionService(db)
	badgeService := services.NewBadgeService(db)

	// --- CONFIGURE Sync Service Details for Tournament Users ---
	syncServiceURL := os.Getenv("SYNC_SERVICE_URL")
	if syncServiceURL == "" {
		log.Fatal("SYNC_SERVICE_URL environment variable not set")
	}
	gameServiceToken := os.Getenv("GAME_SERVICE_TOKEN")
	if gameServiceToken == "" {
		log.Fatal("GAME_SERVICE_TOKEN environment variable not set")
	}
	// --- END CONFIG ---

	syncWorker := workers.NewTournamentUserSyncWorker(db, syncServiceURL, "/api/v1/public/profiles", gameServiceToken)

	// --- NEW: Initialize Wallet Sync Client and Start Polling (using workers package) ---
	walletSyncClient := workers.NewWalletSyncClient(db)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go workers.PollWallets(ctx, walletSyncClient, 10*time.Second)
	// --- END NEW ---

	go func() {
		log.Println("Starting Tournament User Sync Worker...")
		syncWorker.Start(ctx)
	}()

	gameService.StartPublishScheduler()

	// ✅ Setup routes — now with enforced Gateway auth + consistent /s/ prefix
	handlers.SetupGameRoutes(app, gameService)
	handlers.SetupTournamentRoutes(app, tournamentService)
	handlers.SetupProgressionRoutes(app, progressionService, badgeService)

	app.Static("/uploads", "./uploads")

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

	if err := os.MkdirAll("./public/web3gl", os.ModePerm); err != nil {
		log.Fatal("failed to ensure web3gl public dir:", err)
	}

	go func() {
		if err := app.Listen(":5200"); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	log.Println("✅ Server running on http://localhost:5200")
	log.Println("✅ Tournament User Sync Worker running")
	log.Println("✅ Wallet polling running (every 10s)")
	log.Println("✅ GatewayAuthMiddleware enforced globally — all requests must come from Gateway")
	log.Printf("✅ CORS configured for origins: %s", allowedOriginsString)

	<-ctx.Done()
	log.Println("Shutting down server...")
}