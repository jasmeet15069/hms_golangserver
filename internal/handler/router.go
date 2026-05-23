package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/hotelharmony/api/pkg/response"
)

type Handlers struct {
	Health    *HealthHandler
	Auth      *AuthHandler
	Payments  *PaymentHandler
	Dashboard *DashboardHandler
	Rooms     *RoomHandler
	Compat    *CompatHandler
}

func Register(app *fiber.App, h Handlers) {
	app.Get("/api", func(c *fiber.Ctx) error {
		return response.OK(c, map[string]interface{}{
			"name":    "Hotel Harmony Go API",
			"status":  "ok",
			"version": "1.0.0",
			"routes": []string{
				"GET /health",
				"POST /api/auth/sign-in",
				"POST /api/auth/sign-up",
				"GET /api/payment-config",
				"GET /api/exchange-rate",
				"POST /api/bookings/checkout",
				"POST /api/payments/checkout",
				"POST /api/payments/complete",
				"GET /api/dashboard/stats",
				"GET /api/rooms",
			},
		})
	})
	if h.Health != nil {
		h.Health.Register(app)
	}
	api := app.Group("/api")
	if h.Auth != nil {
		h.Auth.Register(api)
	}
	if h.Payments != nil {
		h.Payments.Register(api)
	}
	if h.Dashboard != nil {
		h.Dashboard.Register(api)
	}
	if h.Rooms != nil {
		h.Rooms.Register(api)
	}
	if h.Compat != nil {
		h.Compat.Register(api)
	}
}
