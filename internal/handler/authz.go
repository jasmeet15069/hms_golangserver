package handler

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/hotelharmony/api/pkg/response"
)

func jwtClaimsFromRequest(c *fiber.Ctx, secret string) (jwt.MapClaims, error) {
	authHeader := c.Get("Authorization")
	tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if tokenString == "" || tokenString == authHeader || secret == "" {
		return nil, fmt.Errorf("missing bearer token")
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("invalid signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid bearer token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid bearer token")
	}
	return claims, nil
}

func requireAuthenticatedRequest(c *fiber.Ctx, secret string) error {
	if _, err := jwtClaimsFromRequest(c, secret); err != nil {
		return response.Error(c, fiber.StatusUnauthorized, "authentication is required")
	}
	return nil
}

func requireAnyRoleFromToken(c *fiber.Ctx, secret string, allowed ...string) error {
	claims, err := jwtClaimsFromRequest(c, secret)
	if err != nil {
		return response.Error(c, fiber.StatusUnauthorized, "authentication is required")
	}

	rawRoles, ok := claims["roles"].([]interface{})
	if !ok {
		return response.Error(c, fiber.StatusForbidden, "required role is missing")
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, role := range allowed {
		allowedSet[role] = struct{}{}
	}
	for _, rawRole := range rawRoles {
		role, _ := rawRole.(string)
		if _, ok := allowedSet[role]; ok {
			return nil
		}
	}
	return response.Error(c, fiber.StatusForbidden, "access denied")
}
