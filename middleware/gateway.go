// internal/middleware/gateway_auth.go
package middleware

import (
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// GatewayAuthMiddleware validates the Bearer token from the Gateway
func GatewayAuthMiddleware() fiber.Handler {
	expectedToken := os.Getenv("GAME_SERVICE_TOKEN")
	if expectedToken == "" {
		log.Fatal("❌ GAME_SERVICE_TOKEN is not set — service cannot authenticate Gateway")
	}

	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			log.Printf("🚫 [GATEWAY_AUTH] Missing Authorization header for %s", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "gateway authentication token missing",
			})
		}

		// Parse "Bearer <token>"
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			// no "Bearer " prefix — try raw value (e.g., if Gateway sends raw token)
			token = authHeader
		}

		if token != expectedToken {
			log.Printf("❌ [GATEWAY_AUTH] Invalid token for %s (got prefix: %.10s...)", c.Path(), token)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid gateway authentication token",
			})
		}

		log.Printf("✅ [GATEWAY_AUTH] Request from Gateway accepted for %s", c.Path())
		return c.Next()
	}
}