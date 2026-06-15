package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/hotelharmony/api/pkg/response"
)

type Handlers struct {
	Health       *HealthHandler
	Auth         *AuthHandler
	Hotels       *HotelHandler
	Payments     *PaymentHandler
	Dashboard    *DashboardHandler
	Rooms        *RoomHandler
	Ops          *OperationsHandler
	AI           *AIHandler
	Compat       *CompatHandler
	Users         *UserHandler
	Reservations  *ReservationHandler
	Billing       *BillingHandler
	Housekeeping  *HousekeepingHandler
	Revenue       *RevenueHandler
	Procurement   *ProcurementHandler
	CRM           *CRMHandler
	Channel       *ChannelHandler
	NightAudit    *NightAuditHandler
	Booking       *BookingHandler
	Asset         *AssetHandler
	Communications *CommunicationsHandler
}

func Register(app *fiber.App, h Handlers) {
	app.Get("/api", func(c *fiber.Ctx) error {
		return response.OK(c, map[string]interface{}{
			"name":    "Hotel Harmony Go API",
			"status":  "ok",
			"version": "1.0.0",
			"routes": []string{
				"GET /health",
				"GET /ready",
				"POST /api/auth/sign-in",
				"POST /api/auth/sign-up",
				"POST /api/auth/sign-out",
				"POST /api/auth/refresh",
				"PATCH /api/auth/user",
				"GET /api/hotel/branding",
				"PUT /api/hotel/branding",
				"POST /api/onboarding/hotel",
				"GET /api/payment-config",
				"GET /api/exchange-rate",
				"POST /api/bookings/checkout",
				"POST /api/bookings/hold",
				"POST /api/bookings/razorpay/order",
				"POST /api/payments/checkout",
				"POST /api/payments/complete",
				"POST /api/payments/razorpay/order",
				"POST /api/payments/razorpay/verify",
				"GET /api/dashboard/stats",
				"GET /api/dashboard/data",
				"GET /api/rooms",
				"POST /api/rooms",
				"PATCH /api/rooms/:id/status",
				"POST /api/housekeeping/guest-requests",
				"GET /api/housekeeping/tasks",
				"POST /api/housekeeping/tasks",
				"PATCH /api/housekeeping/tasks/:id",
				"GET /api/housekeeping/lost-items",
				"POST /api/housekeeping/lost-items",
				"PATCH /api/housekeeping/lost-items/:id",
				"GET /api/housekeeping/linen",
				"POST /api/housekeeping/linen/issue",
				"POST /api/housekeeping/linen/return",
				"GET /api/plan/limits",
				"GET /api/platform/plans",
				"GET /api/platform/tenants",
				"POST /api/platform/tenants",
				"PUT /api/platform/tenants/:id/plan",
				"GET /api/reports/occupancy",
				"GET /api/reports/revenue",
				"GET /api/reports/complaints",
				"GET /api/reports/bookings-pace",
				"GET /api/reports/staff-activity",
				"GET /api/reports/ai-usage",
				"GET /api/reports/consolidated",
				"POST /api/ai/chat",
				"POST /api/functions/ai-menu-suggestions",
				"POST /api/functions/ai-complaint-analysis",
				"POST /api/functions/voice-assistant-token",
				"GET /api/settings/payment",
				"PUT /api/settings/payment",
				"GET /api/settings/role-portals",
				"PUT /api/settings/role-portals",
				"GET /api/users",
				"GET /api/users/:id",
				"PATCH /api/users/:id",
				"POST /api/users/:id/roles",
				"DELETE /api/users/:id/roles/:role",
				"GET /api/reservations",
				"GET /api/reservations/calendar",
				"GET /api/reservations/:id",
				"POST /api/reservations",
				"PATCH /api/reservations/:id",
				"DELETE /api/reservations/:id",
				"POST /api/reservations/:id/checkin",
				"POST /api/reservations/:id/checkout",
				"GET /api/billing/folios",
				"GET /api/billing/folios/:id",
				"POST /api/billing/folios",
				"POST /api/billing/folios/:id/charges",
				"POST /api/billing/folios/:id/payments",
				"GET /api/billing/charges",
				"GET /api/billing/invoices",
				"POST /api/billing/invoices",
				"GET /api/billing/invoices/:id",
				"POST /api/billing/invoices/:id/email",
				"GET /api/billing/transactions",
				"GET /api/revenue/pricing",
				"GET /api/revenue/pricing-rules",
				"POST /api/revenue/pricing",
				"POST /api/revenue/pricing-rules",
				"PUT /api/revenue/pricing-rules/:id",
				"PATCH /api/revenue/pricing-rules/:id",
				"DELETE /api/revenue/pricing/:id",
				"DELETE /api/revenue/pricing-rules/:id",
				"GET /api/revenue/yield",
				"GET /api/revenue/competitors",
				"GET /api/revenue/forecast",
				"GET /api/procurement/vendors",
				"POST /api/procurement/vendors",
				"PATCH /api/procurement/vendors/:id",
				"GET /api/procurement/purchase-orders",
				"POST /api/procurement/purchase-orders",
				"PATCH /api/procurement/purchase-orders/:id/status",
				"PATCH /api/procurement/purchase-orders/:id",
				"GET /api/crm/guests",
				"GET /api/crm/guests/:id",
				"PATCH /api/crm/guests/:id",
				"GET /api/crm/loyalty/tiers",
				"POST /api/crm/loyalty/tiers",
				"PUT /api/crm/loyalty/tiers/:id",
				"PATCH /api/crm/loyalty/tiers/:id",
				"GET /api/crm/loyalty/members",
				"POST /api/crm/loyalty/points/award",
				"POST /api/crm/loyalty/points/redeem",
				"GET /api/channel/connections",
				"POST /api/channel/connections",
				"PATCH /api/channel/connections/:id",
				"DELETE /api/channel/connections/:id",
				"GET /api/channel/analytics",
				"GET /api/night-audit/checklist",
				"GET /api/night-audit/revenue-audit",
				"GET /api/night-audit/tax-audit",
				"POST /api/night-audit/close-day",
				"GET /api/night-audit/reports",
				"GET /api/booking/availability",
				"POST /api/booking/search",
				"GET /api/booking/promotions",
				"POST /api/booking/promotions",
				"PATCH /api/booking/promotions/:id",
				"DELETE /api/booking/promotions/:id",
				"POST /api/booking/reservations",
				"POST /api/booking/validate-promo",
				"GET /api/maintenance/assets",
				"POST /api/maintenance/assets",
				"PATCH /api/maintenance/assets/:id",
				"GET /api/maintenance/schedule",
				"POST /api/maintenance/schedule",
				"PATCH /api/maintenance/schedule/:id/complete",
				"POST /api/email/send",
				"POST /api/sms/send",
				"GET /api/tables/:table",
				"POST /api/tables/:table",
				"PATCH /api/tables/:table",
				"DELETE /api/tables/:table",
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
	if h.Users != nil {
		h.Users.Register(api)
	}
	if h.Reservations != nil {
		h.Reservations.Register(api)
	}
	if h.Billing != nil {
		h.Billing.Register(api)
	}
	if h.Housekeeping != nil {
		h.Housekeeping.Register(api)
	}
	if h.Revenue != nil {
		h.Revenue.Register(api)
	}
	if h.Procurement != nil {
		h.Procurement.Register(api)
	}
	if h.CRM != nil {
		h.CRM.Register(api)
	}
	if h.Channel != nil {
		h.Channel.Register(api)
	}
	if h.NightAudit != nil {
		h.NightAudit.Register(api)
	}
	if h.Booking != nil {
		h.Booking.Register(api)
	}
	if h.Asset != nil {
		h.Asset.Register(api)
	}
	if h.Communications != nil {
		h.Communications.Register(api)
	}
	if h.Reservations != nil {
		api.Post("/booking/reservations", h.Reservations.Create)
	}
	if h.Procurement != nil {
		api.Patch("/procurement/purchase-orders/:id", h.Procurement.UpdatePOStatus)
	}
}
