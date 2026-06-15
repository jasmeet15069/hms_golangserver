package handler

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/hotelharmony/api/internal/repository/postgres"
	"github.com/hotelharmony/api/pkg/response"
)

// baseHandler carries the cross-cutting context every protected handler needs:
// the JWT access-token secret used for stateless authentication and tenant
// resolution. Embedding it gives each handler the auth/tenant helpers below
// without changing method signatures.
type baseHandler struct {
	secret string
}

func newBase(secret string) baseHandler { return baseHandler{secret: secret} }

// hotelID resolves the tenant for the current request from the authenticated
// JWT's hotel_id claim. It falls back to the demo/default hotel only when the
// token carries no hotel_id (e.g. legacy single-tenant accounts), so existing
// data is never orphaned while properly-provisioned tenants stay isolated.
func (b baseHandler) hotelID(c *fiber.Ctx) uuid.UUID {
	if claims, err := jwtClaimsFromRequest(c, b.secret); err == nil {
		if raw, _ := claims["hotel_id"].(string); strings.TrimSpace(raw) != "" {
			if parsed, perr := uuid.Parse(strings.TrimSpace(raw)); perr == nil {
				return parsed
			}
		}
	}
	return postgres.DemoHotelID
}

// userID returns the authenticated user id from the JWT subject, if present.
func (b baseHandler) userID(c *fiber.Ctx) *uuid.UUID {
	if claims, err := jwtClaimsFromRequest(c, b.secret); err == nil {
		if raw, _ := claims["sub"].(string); strings.TrimSpace(raw) != "" {
			if parsed, perr := uuid.Parse(strings.TrimSpace(raw)); perr == nil {
				return &parsed
			}
		}
	}
	return nil
}

// isPlatformAdmin reports whether the caller holds a platform-wide admin role,
// which is allowed to operate across tenants.
func (b baseHandler) isPlatformAdmin(c *fiber.Ctx) bool {
	claims, err := jwtClaimsFromRequest(c, b.secret)
	if err != nil {
		return false
	}
	if pa, ok := claims["platform_admin"].(bool); ok && pa {
		return true
	}
	rawRoles, _ := claims["roles"].([]interface{})
	for _, rr := range rawRoles {
		if role, _ := rr.(string); role == "platform_admin" || role == "super_admin" {
			return true
		}
	}
	return false
}

// requireAuth enforces a valid bearer token. It writes the 401 response and
// returns false when authentication fails.
func (b baseHandler) requireAuth(c *fiber.Ctx) bool {
	return requireAuthenticatedRequest(c, b.secret)
}

// requireRoles enforces that the caller holds at least one of the allowed roles.
func (b baseHandler) requireRoles(c *fiber.Ctx, allowed ...string) bool {
	return requireAnyRoleFromToken(c, b.secret, allowed...)
}

// authGate is route-group middleware that rejects unauthenticated requests
// statelessly (signature verification only, no database round-trip), which is
// what keeps authentication cheap under high concurrency.
func authGate(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := jwtClaimsFromRequest(c, secret)
		if err != nil {
			return response.Error(c, fiber.StatusUnauthorized, "authentication is required")
		}
		if sub, _ := claims["sub"].(string); strings.TrimSpace(sub) != "" {
			c.Locals("user_id", sub)
		}
		if hid, _ := claims["hotel_id"].(string); strings.TrimSpace(hid) != "" {
			c.Locals("hotel_id", hid)
		}
		return c.Next()
	}
}

// roleGate is route-group middleware that requires one of the allowed roles.
func roleGate(secret string, allowed ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !requireAnyRoleFromToken(c, secret, allowed...) {
			return nil
		}
		return c.Next()
	}
}
