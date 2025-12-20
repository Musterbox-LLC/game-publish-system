// handlers/game_routes.go
package handlers

import (
	"game-publish-system/middleware"
	"game-publish-system/services"
	"github.com/gofiber/fiber/v2"
)

func SetupGameRoutes(app *fiber.App, gameService *services.GameService) {
	// 🔓 Public routes — *no user context*, but **still require Gateway auth**
	app.Get("/games", gameService.GetAllGames)
	app.Get("/games/minimal", gameService.GetMinimalGames)
	app.Get("/games/:id", gameService.GetGameByID)

	app.Get("/games/featured", gameService.GetFeaturedGames)
	app.Get("/games/featured/minimal", gameService.GetFeaturedGamesMinimal)

	// 🔐 Secured routes — require user context (userID, roles), enforced via middleware
	// Note: Group under `/s` to match tournament design & allow UserContextMiddleware gating
	secured := app.Group("/", middleware.UserContextMiddleware())

	secured.Post("/games", gameService.UploadGame)
	secured.Put("/games/:id", gameService.UpdateGame)
	secured.Patch("/games/:id", gameService.UpdateGame)
	secured.Delete("/games/:id", gameService.DeleteGame)
	secured.Post("/games/:id/process-web3gl", gameService.ProcessWebGL)

	secured.Put("/games/:id/featured/:action", gameService.SetGameFeatured)

	// ✅ Review routes — auth required
	secured.Post("/games/:id/reviews", gameService.CreateReview)
	secured.Get("/games/:id/reviews", gameService.GetReviewsByGame)
	secured.Get("/games/:id/user-review", gameService.GetUserReviewStatus)
	secured.Put("/reviews/:review_id", gameService.UpdateReview)
	secured.Delete("/reviews/:review_id", gameService.DeleteReview)
}

func SetupRewardRoutes(app *fiber.App, rewardService *services.RewardService, authClient *services.AuthServiceClient) {
	// ✅ Dedicated SSE route — NO UserContextMiddleware, uses SSEAuthMiddleware for query param auth
	app.Get("/user/rewards/stream",
		middleware.SSEAuthMiddleware(authClient), // Uses ?token=...&device_id=...
		rewardService.StreamUserRewardsSSE,
	)

	// 🔐 All other reward routes use header-based auth + user context
	secured := app.Group("/", middleware.UserContextMiddleware())

	// User-specific reward endpoint - fetches rewards for the authenticated user
	secured.Get("/user/rewards", rewardService.GetUserRewards)
	// 🔥 NEW: Endpoint to get counts of user's rewards
	secured.Get("/user/rewards/counts", rewardService.GetUserRewardCountsEndpoint)
	secured.Post("/user/rewards/:id/view", rewardService.MarkRewardAsViewed)
	secured.Post("/user/rewards/viewed", rewardService.MarkAllRewardsAsViewed)
	secured.Post("/user/rewards/:id/claim", rewardService.ClaimReward) // Now uncommented

	// 🔒 Admin-only routes - Group under /admin/rewards to match tournament routes
	admin := app.Group("/admin/rewards", middleware.UserContextMiddleware())

	admin.Post("/create", rewardService.CreateReward)
	admin.Put("/:id", rewardService.UpdateReward)
	admin.Patch("/:id/status", rewardService.UpdateRewardStatus)
	admin.Delete("/rewards/:id", rewardService.DeleteReward)
	admin.Get("/", rewardService.GetAllRewards)
}
