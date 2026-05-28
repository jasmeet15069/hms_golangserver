package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/hotelharmony/api/pkg/response"
)

type Handlers struct {
	Health    *HealthHandler
	Auth      *AuthHandler
	Hotels    *HotelHandler
	Payments  *PaymentHandler
	Dashboard *DashboardHandler
	Rooms     *RoomHandler
	Ops       *OperationsHandler
	AI        *AIHandler
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
				"GET /api/hotel/branding",
				"PUT /api/hotel/branding",
				"POST /api/onboarding/hotel",
				"GET /api/payment-config",
				"GET /api/exchange-rate",
				"POST /api/bookings/checkout",
				"POST /api/payments/checkout",
				"POST /api/payments/complete",
				"GET /api/dashboard/stats",
				"GET /api/rooms",
				"POST /api/housekeeping/guest-requests",
				"GET /api/plan/limits",
				"GET /api/reports/occupancy",
				"POST /api/ai/chat",
				"POST /api/functions/ai-menu-suggestions",
				"POST /api/functions/ai-complaint-analysis",
				"GET /api/settings/payment",
				"PUT /api/settings/payment",
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
	if h.Hotels != nil {
		h.Hotels.Register(api)
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
	if h.Ops != nil {
		h.Ops.Register(api)
	}
	if h.AI != nil {
		h.AI.Register(api)
	}
	if h.Compat != nil {
		h.Compat.Register(api)
	}
}
