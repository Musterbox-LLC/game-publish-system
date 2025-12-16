// game-publish-system/middleware/sse_auth.go
package middleware

import (
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"game-publish-system/services"
)

type contextKey string

const (
	UserIDContextKey   contextKey = "userID"
	UserRolesContextKey contextKey = "userRoles"
	DeviceIDContextKey contextKey = "deviceID"
	OTPSkippedContextKey contextKey = "otpNotRequired"
)

// SSEAuthMiddleware validates `token` and `device_id` from query params
// via AuthServiceClient (e.g., auth.musterbox.org/validate).
//
// Usage:
//   app.Get("/user/rewards/stream", middleware.SSEAuthMiddleware(authClient), rewardService.StreamUserRewardsSSE)
func SSEAuthMiddleware(authClient *services.AuthServiceClient) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		log.Printf("[SSEAuth] Processing auth for %s, RemoteAddr: %s", c.Path(), c.IP())
		log.Printf("  → Raw Query: %s", c.Request().URI().QueryArgs().String())

		accessToken := strings.TrimSpace(string(c.Request().URI().QueryArgs().Peek("token")))
		deviceID := strings.TrimSpace(string(c.Request().URI().QueryArgs().Peek("device_id")))

		log.Printf("  → Extracted Token (len=%d)", len(accessToken))
		log.Printf("  → Extracted DeviceID: '%s'", deviceID)

		if accessToken == "" || deviceID == "" {
			log.Printf("[SSEAuth] ❌ Missing query params: token='%s', device_id='%s'", accessToken, deviceID)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Missing token or device_id in query",
			})
		}

		// ✅ Validate with Auth Service (same as wallet service)
		resp, err := authClient.ValidateToken(accessToken, deviceID)
		if err != nil {
			log.Printf("[SSEAuth] ❌ Validation failed for token (prefix: %s...), device %s: %v",
				accessToken[:min(10, len(accessToken))], deviceID, err)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Unauthorized",
			})
		}

		// ✅ Attach to Fiber context (like UserContextMiddleware, but from query)
		c.Locals(string(UserIDContextKey), resp.UserID)
		c.Locals(string(DeviceIDContextKey), resp.DeviceID)
		c.Locals(string(OTPSkippedContextKey), resp.OTPNotRequiredForDevice)
		c.Locals(string(UserRolesContextKey), "gamer") // or fetch from profile later

		log.Printf("[SSEAuth] ✅ Authenticated user %s (device %s)", resp.UserID, resp.DeviceID)
		return c.Next()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}