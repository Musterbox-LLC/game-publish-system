// handlers/game_routes.go — updated
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

	// 🔐 Secured routes — require user context (userID, roles), enforced via middleware
	// Note: Group under `/s` to match tournament design & allow UserContextMiddleware gating
	secured := app.Group("/", middleware.UserContextMiddleware())

	secured.Post("/games", gameService.UploadGame)
	secured.Put("/games/:id", gameService.UpdateGame)
	secured.Patch("/games/:id", gameService.UpdateGame)
	secured.Delete("/games/:id", gameService.DeleteGame)
	secured.Post("/games/:id/process-web3gl", gameService.ProcessWebGL)

	// ✅ Review routes — auth required
	secured.Post("/games/:id/reviews", gameService.CreateReview)
	secured.Get("/games/:id/reviews", gameService.GetReviewsByGame)
	secured.Get("/games/:id/user-review", gameService.GetUserReviewStatus)
	secured.Put("/reviews/:review_id", gameService.UpdateReview)
	secured.Delete("/reviews/:review_id", gameService.DeleteReview)
}