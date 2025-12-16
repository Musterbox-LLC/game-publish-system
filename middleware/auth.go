// internal/middleware/user_context.go
package middleware

import (
	"log"
	"strings"
	"github.com/gofiber/fiber/v2"
)

// UserContextMiddleware extracts user identity and roles set by the Gateway.
// It expects the Gateway to send X-User-ID, X-User-Roles, etc., for secured routes.
// It is applied only to routes requiring authentication (e.g., those under /s/ or /s/admin/).
func UserContextMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-ID") // Retrieved from header set by Gateway
		rolesStr := c.Get("X-User-Roles") // Retrieved from header set by Gateway
		otpNotRequiredStr := c.Get("X-Otp-Not-Required") // Retrieved from header set by Gateway

		// Enforce user context presence on *secured* paths (e.g., /s/ or /s/admin/)
		path := c.Path()
		isSecured := strings.HasPrefix(path, "/s/") || strings.HasPrefix(path, "/s/admin/")
		
		if isSecured && userID == "" {
			log.Printf("[PUB-SVC] [USER_CTX] X-User-ID required but missing on secured route: %s", path)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing X-User-ID — request must come through gateway with auth context",
			})
		}

		// Parse roles string into a slice
		var roles []string
		if rolesStr != "" {
			for _, r := range strings.Split(rolesStr, ",") {
				r = strings.TrimSpace(r)
				if r != "" {
					roles = append(roles, r)
				}
			}
		}

		// Parse OTP flag
		otpNotRequired := strings.ToLower(otpNotRequiredStr) == "true"

		// Attach user details to context locals for handlers to access
		c.Locals("user_id", userID)
		c.Locals("user_roles", roles)
		c.Locals("otp_not_required", otpNotRequired)

		log.Printf("[PUB-SVC] [USER_CTX] UserID=%s, Roles=%v, OTP_exempt=%t | Path: %s", 
			userID, roles, otpNotRequired, path)

		return c.Next() // Proceed to the next handler/route
	}
}