// internal/middleware/user_context.go — updated
package middleware

import (
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// UserContextMiddleware extracts user identity and roles set by Gateway.
// It is applied only to routes under /s/ or /s/admin/ — but for safety, we guard.
func UserContextMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-ID")
		rolesStr := c.Get("X-User-Roles")
		otpNotRequiredStr := c.Get("X-Otp-Not-Required")

		// 🔐 Enforce user context on *secured* paths (i.e., /s/ or /s/admin/)
		path := c.Path()
		isSecured := strings.HasPrefix(path, "/s/") || strings.HasPrefix(path, "/s/admin/")
		if isSecured && userID == "" {
			log.Printf("❌ [USER_CTX] X-User-ID required but missing on secured route: %s", path)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing X-User-ID — request must come through gateway with auth context",
			})
		}

		var roles []string
		if rolesStr != "" {
			for _, r := range strings.Split(rolesStr, ",") {
				r = strings.TrimSpace(r)
				if r != "" {
					roles = append(roles, r)
				}
			}
		}

		otpNotRequired := strings.ToLower(otpNotRequiredStr) == "true"

		// Attach to ctx for handlers
		c.Locals("user_id", userID)
		c.Locals("user_roles", roles)
		c.Locals("otp_not_required", otpNotRequired)

		log.Printf(
			"👤 [USER_CTX] UserID=%s, Roles=%v, OTP exempt=%t | Path: %s",
			userID, roles, otpNotRequired, c.Path(),
		)

		return c.Next()
	}
}