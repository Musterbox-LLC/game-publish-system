// handlers/tournament_routes.go — ✅ no change needed
package handlers

import (
	"game-publish-system/middleware"
	"game-publish-system/services"
	"github.com/gofiber/fiber/v2"
)

func SetupTournamentRoutes(app *fiber.App, tournamentService *services.TournamentService) {
	// 🔓 Public routes — Gateway-authenticated but no user context required
	app.Get("/tournaments", tournamentService.GetAllTournaments)
	app.Get("/tournaments/mini", tournamentService.GetAllTournamentsMini)
	app.Get("/tournaments/:id", tournamentService.GetTournamentByID)

	// ⚠️ `/users/search` is public, but GatewayAuthMiddleware ensures only Gateway can reach it
	app.Get("/users/search", tournamentService.SearchUsers)

	// 🔐 Authenticated routes — require X-User-ID etc.
	secured := app.Group("/", middleware.UserContextMiddleware())

	secured.Post("/tournaments", tournamentService.CreateTournament)
	secured.Put("/tournaments/:id", tournamentService.UpdateTournament)
	secured.Delete("/tournaments/:id", tournamentService.DeleteTournament)
	secured.Patch("/tournaments/:id/status", tournamentService.UpdateTournamentStatus)

	secured.Get("/waivers", tournamentService.GetAllWaivers)
	secured.Post("/waivers", tournamentService.CreateWaiver)
	secured.Post("/waivers/redeem", tournamentService.RedeemWaiver)
	secured.Put("/waivers/:id", tournamentService.UpdateWaiver)
	secured.Delete("/waivers/:id", tournamentService.DeleteWaiver)
	secured.Get("/users/:user_id/waivers/available", tournamentService.GetUserAvailableWaiversEndpoint)

	secured.Post("/tournaments/:id/subscribe", tournamentService.SubscribeToTournament)
	secured.Patch("/tournaments/:tournament_id/subscribers/:user_id/status", tournamentService.SuspendSubscription)
	secured.Post("/tournaments/:tournament_id/subscribers/:user_id/revoke", tournamentService.RevokeSubscription)
	secured.Get("/tournaments/:id/subscribers", tournamentService.GetTournamentSubscribers)
	secured.Post("/tournaments/:tournament_id/subscribers/:user_id/refund", tournamentService.RefundSubscription)

	secured.Post("/tournaments/:id/batches", tournamentService.CreateBatch)
	secured.Put("/batches/:id", tournamentService.UpdateBatch)
	secured.Delete("/batches/:id", tournamentService.DeleteBatch)

	secured.Post("/batches/:batch_id/rounds", tournamentService.CreateRound)
	secured.Put("/rounds/:id", tournamentService.UpdateRound)
	secured.Delete("/rounds/:id", tournamentService.DeleteRound)

	// 🔒 Admin-only routes
	admin := app.Group("/s/admin", middleware.UserContextMiddleware())
	admin.Get("/waivers", tournamentService.GetAllWaivers) // sensitive: all waivers
}
