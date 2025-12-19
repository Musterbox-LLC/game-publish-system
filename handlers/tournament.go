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
	
	// Tournament CRUD
	secured.Post("/tournaments", tournamentService.CreateTournament)
	secured.Put("/tournaments/:id", tournamentService.UpdateTournament)
	secured.Delete("/tournaments/:id", tournamentService.DeleteTournament)
	secured.Patch("/tournaments/:id/status", tournamentService.UpdateTournamentStatus)
	
	// Tournament subscriptions
	secured.Post("/tournaments/:id/subscribe", tournamentService.SubscribeToTournament)
	secured.Get("/tournaments/:id/subscribers", tournamentService.GetTournamentSubscribers)
	
	// Subscription management
	secured.Patch("/tournaments/:tournament_id/subscribers/:user_id/status", tournamentService.SuspendSubscription)
	secured.Post("/tournaments/:tournament_id/subscribers/:user_id/revoke", tournamentService.RevokeSubscription)
	secured.Post("/tournaments/:id/subscribers/:user_id/refund", tournamentService.RefundSubscription)

	// Structure: Batches
	secured.Get("/tournaments/:id/structure", tournamentService.GetTournamentStructure) 
	secured.Post("/tournaments/:id/batches", tournamentService.CreateBatch)
	secured.Put("/tournaments/:id/batches/:batch_id", tournamentService.UpdateBatch) 
	secured.Put("/batches/:id", tournamentService.UpdateBatch)
	secured.Delete("/batches/:id", tournamentService.DeleteBatch)
	
	// Structure: Batch with matches and rounds (complete structure)
	secured.Post("/tournaments/:id/batches-with-matches", tournamentService.CreateBatchWithMatchesAndRounds)
	
	// Structure: Matches
	secured.Post("/tournaments/:id/matches", tournamentService.CreateMatch)
	secured.Put("/tournaments/:id/matches/:match_id", tournamentService.UpdateMatch)
	secured.Delete("/tournaments/:id/matches/:match_id", tournamentService.DeleteMatch)
	
	// Structure: Rounds
	secured.Post("/tournaments/:id/rounds", tournamentService.CreateRound)
	secured.Put("/tournaments/:id/rounds/:round_id", tournamentService.UpdateRound)
	secured.Delete("/tournaments/:id/rounds/:round_id", tournamentService.DeleteRound)

	// Waiver endpoints
	secured.Get("/waivers", tournamentService.GetAllWaivers)
	secured.Get("/users/me/waivers", tournamentService.GetUserWaiversEndpoint)            
	secured.Get("/users/me/waivers/counts", tournamentService.GetUserWaiverCountsEndpoint)
	secured.Patch("/waivers/:id/claimed", tournamentService.MarkWaiverAsClaimedEndpoint)
	secured.Patch("/waivers/:id/viewed", tournamentService.MarkWaiverAsViewedEndpoint)    
	secured.Patch("/waivers/:id/redeemed", tournamentService.MarkWaiverAsRedeemedEndpoint)
	secured.Post("/waivers/:id/redeem", tournamentService.RedeemWaiver)
	secured.Put("/waivers/:id", tournamentService.UpdateWaiver)
	secured.Delete("/waivers/:id", tournamentService.DeleteWaiver)
	secured.Get("/users/:user_id/waivers/available", tournamentService.GetUserAvailableWaiversEndpoint)

	// 🔒 Admin-only routes
	admin := app.Group("/s/admin", middleware.UserContextMiddleware())
	admin.Post("/waivers", tournamentService.CreateWaiver)
	admin.Get("/waivers", tournamentService.GetAllWaivers) // sensitive: all waivers
	admin.Put("/waivers/:id",  tournamentService.UpdateWaiver)
}