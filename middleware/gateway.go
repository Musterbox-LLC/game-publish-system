// internal/middleware/gateway_auth.go (Inside Publisher Service)
package middleware

import (
	"log"
	"os"
	"strings"
	"github.com/gofiber/fiber/v2"
)

// GatewayAuthMiddleware validates the Bearer token set by the Gateway.
// This prevents direct access to the Publisher Service, enforcing traffic through the Gateway.
func GatewayAuthMiddleware() fiber.Handler {
	expectedToken := os.Getenv("GAME_SERVICE_TOKEN") // Token configured in Gateway
	if expectedToken == "" {
		log.Fatal("❌ GAME_SERVICE_TOKEN is not set in Game Publisher Service — cannot authenticate Gateway")
	}

	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization") // Header set by Gateway
		if authHeader == "" {
			log.Printf("[PUB-SVC] [GATEWAY_AUTH] Missing Authorization header for %s", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "gateway authentication token missing",
			})
		}

		// Parse "Bearer <token>"
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			// If no "Bearer " prefix, assume the whole header is the token (as Gateway might send it)
			token = authHeader
		}

		if token != expectedToken {
			log.Printf("[PUB-SVC] [GATEWAY_AUTH] Invalid token for %s (got prefix: %.10s...)", c.Path(), token)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid gateway authentication token",
			})
		}

		log.Printf("[PUB-SVC] [GATEWAY_AUTH] Request from Gateway accepted for %s", c.Path())
		return c.Next() // Proceed to the next middleware/handler
	}
}