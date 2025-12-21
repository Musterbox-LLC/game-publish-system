package handlers

import (
	"game-publish-system/middleware"
	"game-publish-system/services"
	"github.com/gofiber/fiber/v2"
)

func SetupTournamentRoutes(app *fiber.App, tournamentService *services.TournamentService, pairingService *services.PairingService) {
	// 🔓 Public routes for users (only published tournaments)
	// Add /api/v1 prefix to match your frontend calls
	app.Get("/tournaments/published", tournamentService.GetAllPublishedTournaments) // NEW
	app.Get("/tournaments/published/:id", tournamentService.GetPublishedTournamentByID) // NEW
	app.Get("/match-types", tournamentService.GetSupportedMatchTypes)
	app.Get("/users/search", tournamentService.SearchUsers)

	// 🔐 Authenticated routes
	secured := app.Group("/", middleware.UserContextMiddleware())
	
	// Tournament CRUD (Admin/Manager only)
	secured.Post("/tournaments", tournamentService.CreateTournament)
	secured.Get("/tournaments", tournamentService.GetAllTournaments) // Admin view - all tournaments
	secured.Get("/tournaments/mini", tournamentService.GetAllTournamentsMini) // Admin view - mini list
	secured.Get("/tournaments/featured-mini", tournamentService.GetFeaturedTournamentsMini)
	secured.Get("/tournaments/:id", tournamentService.GetTournamentByID) // Admin view - full details
	secured.Put("/tournaments/:id", tournamentService.UpdateTournament)
	secured.Delete("/tournaments/:id", tournamentService.DeleteTournament)
	
	// Tournament status management
	secured.Patch("/tournaments/:id/status", tournamentService.UpdateTournamentStatus)
	secured.Patch("/tournaments/:id/feature", tournamentService.ToggleFeaturedStatus) // Feature/unfeature
	
	// NEW: Publish scheduling endpoints
	secured.Post("/tournaments/:id/publish/now", tournamentService.PublishNow) // Publish immediately
	secured.Post("/tournaments/:id/publish/schedule", tournamentService.SchedulePublish) // Schedule for later
	secured.Post("/tournaments/:id/publish/cancel", tournamentService.CancelScheduledPublish) // Cancel scheduled publish
	
	// Tournament subscriptions
	secured.Post("/tournaments/:id/subscribe", tournamentService.SubscribeToTournament)
	secured.Get("/tournaments/:id/subscribers", tournamentService.GetTournamentSubscribers)
	
	// Subscription management
	secured.Patch("/tournaments/:tournament_id/subscribers/:user_id/suspend", tournamentService.SuspendSubscription)
	secured.Post("/tournaments/:tournament_id/subscribers/:user_id/revoke", tournamentService.RevokeSubscription)
	secured.Post("/tournaments/:tournament_id/subscribers/:user_id/refund", tournamentService.RefundSubscription)

	// Structure: Batches
	secured.Get("/tournaments/:id/structure", tournamentService.GetTournamentStructure)
	secured.Post("/tournaments/:id/batches", tournamentService.CreateBatch)
	secured.Put("/tournaments/:id/batches/:batch_id", tournamentService.UpdateBatch)
	secured.Delete("/batches/:id", tournamentService.DeleteBatch)
	
	// Structure: Batch with matches and rounds
	secured.Post("/tournaments/:id/batches-with-matches", tournamentService.CreateBatchWithMatchesAndRounds)
	
	// Structure: Matches
	secured.Post("/tournaments/:id/matches", tournamentService.CreateMatch)
	secured.Put("/tournaments/:id/matches/:match_id", tournamentService.UpdateMatch)
	secured.Delete("/tournaments/:id/matches/:match_id", tournamentService.DeleteMatch)
	
	// Structure: Rounds
	secured.Post("/tournaments/:id/matches/:match_id/rounds", tournamentService.CreateRound)
	secured.Put("/tournaments/:id/rounds/:round_id", tournamentService.UpdateRound)
	secured.Delete("/tournaments/:id/rounds/:round_id", tournamentService.DeleteRound)

	// Pairing endpoints
	secured.Post("/matches/:match_id/pairings", pairingService.GeneratePairings)
	secured.Put("/pairings/:pairing_id", pairingService.UpdatePairings)
	secured.Post("/pairings/:pairing_id/approve", pairingService.ApprovePairings)
	secured.Post("/pairings/:pairing_id/publish", pairingService.PublishPairings)
	secured.Post("/pairings/:pairing_id/reject", pairingService.RejectPairings)
	secured.Get("/matches/:match_id/pairings/status", pairingService.GetPairingStatus)
	secured.Get("/matches/:match_id/pairings/history", pairingService.GetPairingHistory)

	// Waiver endpoints
	secured.Get("/users/me/waivers", tournamentService.GetUserWaiversEndpoint)
	secured.Get("/users/me/waivers/counts", tournamentService.GetUserWaiverCountsEndpoint)
	secured.Patch("/waivers/:id/claimed", tournamentService.MarkWaiverAsClaimedEndpoint)
	secured.Patch("/waivers/:id/viewed", tournamentService.MarkWaiverAsViewedEndpoint)
	secured.Patch("/waivers/:id/redeemed", tournamentService.MarkWaiverAsRedeemedEndpoint)
	secured.Post("/waivers/:id/redeem", tournamentService.RedeemWaiver)
	secured.Get("/users/:user_id/waivers/available", tournamentService.GetUserAvailableWaiversEndpoint)

	// 🔒 Admin-only routes
	admin := secured.Group("/admin")
	admin.Post("/waivers", tournamentService.CreateWaiver)
	admin.Get("/waivers", tournamentService.GetAllWaivers)
	admin.Put("/waivers/:id", tournamentService.UpdateWaiver)
	admin.Delete("/waivers/:id", tournamentService.DeleteWaiver)
}