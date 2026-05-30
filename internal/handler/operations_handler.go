package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hotelharmony/api/internal/config"
	"github.com/hotelharmony/api/internal/repository/postgres"
	"github.com/hotelharmony/api/pkg/response"
)

type OperationsHandler struct {
	pool      *pgxpool.Pool
	secretKey string
}

func NewOperationsHandler(pool *pgxpool.Pool, cfg *config.Config) *OperationsHandler {
	secret := ""
	if cfg != nil {
		secret = cfg.Auth.AccessTokenSecret
	}
	return &OperationsHandler{pool: pool, secretKey: secret}
}

func (h *OperationsHandler) Register(r fiber.Router) {
	r.Post("/housekeeping/guest-requests", h.CreateGuestHousekeepingRequest)
	r.Get("/plan/limits", h.PlanLimits)
	r.Get("/reports/occupancy", h.OccupancyReport)
	r.Get("/reports/revenue", h.RevenueReport)
	r.Get("/reports/complaints", h.ComplaintsReport)
	r.Get("/reports/bookings-pace", h.BookingsPaceReport)
	r.Get("/reports/staff-activity", h.StaffActivityReport)
	r.Get("/reports/ai-usage", h.AIUsageReport)
	r.Get("/settings/payment", h.PaymentSettings)
	r.Put("/settings/payment", h.UpdatePaymentSettings)
	r.Get("/settings/role-portals", h.RolePortalSettings)
	r.Put("/settings/role-portals", h.UpdateRolePortalSettings)
}

type housekeepingRequest struct {
	GuestStayID string `json:"guest_stay_id"`
	RequestType string `json:"request_type"`
	Notes       string `json:"notes"`
}

func (h *OperationsHandler) CreateGuestHousekeepingRequest(c *fiber.Ctx) error {
	if !requireAuthenticatedRequest(c, h.secretKey) {
		return nil
	}

	var req housekeepingRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	stayID, err := uuid.Parse(strings.TrimSpace(req.GuestStayID))
	if err != nil {
		return response.Error(c, fiber.StatusUnprocessableEntity, "guest_stay_id is required")
	}
	taskType := strings.TrimSpace(req.RequestType)
	if taskType == "" {
		taskType = "guest_request"
	}

	var hotelID, roomID uuid.UUID
	var guestID *uuid.UUID
	err = h.pool.QueryRow(c.Context(),
		`SELECT hotel_id, room_id, guest_id FROM guest_stays WHERE id = $1`,
		stayID,
	).Scan(&hotelID, &roomID, &guestID)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "active stay not found")
	}

	assignmentID := uuid.New()
	var createdAt interface{}
	err = h.pool.QueryRow(c.Context(), `
		INSERT INTO housekeeping_assignments (
			id, hotel_id, room_id, task_type, priority, status, notes, created_at, updated_at
		) VALUES ($1,$2,$3,$4,'normal','pending',$5,now(),now())
		RETURNING created_at`,
		assignmentID, hotelID, roomID, taskType, nullableText(req.Notes),
	).Scan(&createdAt)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	h.audit(c, hotelID, guestID, "CREATE", "housekeeping_assignments", assignmentID, map[string]interface{}{
		"id":            assignmentID,
		"guest_stay_id": stayID,
		"room_id":       roomID,
		"task_type":     taskType,
		"status":        "pending",
		"notes":         strings.TrimSpace(req.Notes),
	})

	return response.Created(c, map[string]interface{}{
		"id":         assignmentID,
		"hotel_id":   hotelID,
		"room_id":    roomID,
		"task_type":  taskType,
		"priority":   "normal",
		"status":     "pending",
		"notes":      nullableText(req.Notes),
		"created_at": createdAt,
	})
}

func (h *OperationsHandler) PlanLimits(c *fiber.Ctx) error {
	hotelID := postgres.DemoHotelID
	var plan string
	var settingsBytes []byte
	err := h.pool.QueryRow(c.Context(), `SELECT plan_tier, settings FROM hotels WHERE id = $1`, hotelID).Scan(&plan, &settingsBytes)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "hotel not found")
	}
	var roomsUsed, propertiesUsed int
	_ = h.pool.QueryRow(c.Context(), `SELECT COUNT(*) FROM rooms WHERE hotel_id = $1`, hotelID).Scan(&roomsUsed)
	_ = h.pool.QueryRow(c.Context(), `SELECT COUNT(*) FROM properties WHERE hotel_id = $1`, hotelID).Scan(&propertiesUsed)
	settings := map[string]interface{}{}
	_ = json.Unmarshal(settingsBytes, &settings)
	return response.OK(c, map[string]interface{}{
		"plan":            plan,
		"settings":        settings,
		"rooms_used":      roomsUsed,
		"rooms_max":       settings["max_rooms"],
		"properties_used": propertiesUsed,
		"properties_max":  settings["max_properties"],
		"ai_addon":        settings["ai_addon"],
	})
}

func (h *OperationsHandler) OccupancyReport(c *fiber.Ctx) error {
	return h.reportCounts(c, "occupancy", `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'occupied'), COUNT(*) FILTER (WHERE status = 'available')
		FROM rooms WHERE hotel_id = $1`,
		[]string{"total_rooms", "occupied_rooms", "available_rooms"},
	)
}

func (h *OperationsHandler) RevenueReport(c *fiber.Ctx) error {
	return h.reportCounts(c, "revenue", `
		SELECT COALESCE(SUM(amount), 0), COUNT(*) FILTER (WHERE status = 'completed'), COUNT(*)
		FROM payments WHERE hotel_id = $1`,
		[]string{"total_revenue", "completed_payments", "payment_count"},
	)
}

func (h *OperationsHandler) ComplaintsReport(c *fiber.Ctx) error {
	return h.reportCounts(c, "complaints", `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'open'), COUNT(*) FILTER (WHERE priority = 'critical')
		FROM complaints WHERE hotel_id = $1`,
		[]string{"total_complaints", "open_complaints", "critical_complaints"},
	)
}

func (h *OperationsHandler) BookingsPaceReport(c *fiber.Ctx) error {
	return h.reportCounts(c, "bookings_pace", `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE check_in_date::date >= CURRENT_DATE), COUNT(*) FILTER (WHERE actual_check_out IS NULL)
		FROM guest_stays WHERE hotel_id = $1`,
		[]string{"total_bookings", "future_arrivals", "active_or_pending"},
	)
}

func (h *OperationsHandler) StaffActivityReport(c *fiber.Ctx) error {
	return h.reportCounts(c, "staff_activity", `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE clock_out IS NULL), COUNT(DISTINCT user_id)
		FROM staff_shifts WHERE hotel_id = $1`,
		[]string{"shift_count", "clocked_in", "staff_count"},
	)
}

func (h *OperationsHandler) AIUsageReport(c *fiber.Ctx) error {
	return h.reportCounts(c, "ai_usage", `
		SELECT COUNT(*), COALESCE(SUM(tokens_used), 0), 0
		FROM ai_concierge_logs WHERE hotel_id = $1`,
		[]string{"conversation_turns", "tokens_used", "inventory_alerts"},
	)
}

func (h *OperationsHandler) PaymentSettings(c *fiber.Ctx) error {
	if err := h.ensurePaymentConfig(c); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	var activeGateway, defaultCurrency, gatewayMode string
	var stripeEnabled, razorpayEnabled, cashEnabled, cardEnabled, bankTransferEnabled bool
	var stripeAccountID, stripePublishableKey, stripeSecret, stripeWebhookSecret, razorpayKeyID, razorpaySecret, country *string
	var depositValue, cancellationPenaltyPercent float64
	var depositType *string
	var cancellationFreeHours int
	err := h.pool.QueryRow(c.Context(), `
		SELECT pc.active_gateway, pc.stripe_enabled, pc.stripe_account_id, pc.stripe_publishable_key,
		       pc.stripe_secret_key_encrypted, pc.stripe_webhook_secret_encrypted,
		       pc.razorpay_enabled, pc.razorpay_key_id, pc.razorpay_key_secret_encrypted,
		       pc.cash_enabled, pc.card_enabled, pc.bank_transfer_enabled,
		       pc.deposit_type, COALESCE(pc.deposit_value, 0),
		       COALESCE(pc.cancellation_free_hours, 24), COALESCE(pc.cancellation_penalty_percent, 0),
		       COALESCE(pc.default_currency, h.currency, 'USD'), pc.gateway_mode, h.country
		FROM payment_configs pc
		JOIN hotels h ON h.id = pc.hotel_id
		WHERE pc.hotel_id = $1`,
		postgres.DemoHotelID,
	).Scan(
		&activeGateway, &stripeEnabled, &stripeAccountID, &stripePublishableKey,
		&stripeSecret, &stripeWebhookSecret,
		&razorpayEnabled, &razorpayKeyID, &razorpaySecret,
		&cashEnabled, &cardEnabled, &bankTransferEnabled,
		&depositType, &depositValue,
		&cancellationFreeHours, &cancellationPenaltyPercent,
		&defaultCurrency, &gatewayMode, &country,
	)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "payment settings not found")
	}
	return response.OK(c, map[string]interface{}{
		"active_gateway":               activeGateway,
		"default_currency":             defaultCurrency,
		"country":                      country,
		"gateway_mode":                 gatewayMode,
		"stripe_enabled":               stripeEnabled,
		"stripe_account_id":            stripeAccountID,
		"stripe_publishable_key":       stripePublishableKey,
		"stripe_secret_configured":     stripeSecret != nil && strings.TrimSpace(*stripeSecret) != "",
		"stripe_webhook_configured":    stripeWebhookSecret != nil && strings.TrimSpace(*stripeWebhookSecret) != "",
		"razorpay_enabled":             razorpayEnabled,
		"razorpay_key_id":              razorpayKeyID,
		"razorpay_secret_configured":   razorpaySecret != nil && strings.TrimSpace(*razorpaySecret) != "",
		"cash_enabled":                 cashEnabled,
		"card_enabled":                 cardEnabled,
		"bank_transfer_enabled":        bankTransferEnabled,
		"deposit_type":                 depositType,
		"deposit_value":                depositValue,
		"cancellation_free_hours":      cancellationFreeHours,
		"cancellation_penalty_percent": cancellationPenaltyPercent,
	})
}

type updatePaymentSettingsRequest struct {
	ActiveGateway              string  `json:"active_gateway"`
	DefaultCurrency            string  `json:"default_currency"`
	Country                    *string `json:"country"`
	GatewayMode                string  `json:"gateway_mode"`
	StripeEnabled              bool    `json:"stripe_enabled"`
	StripeAccountID            *string `json:"stripe_account_id"`
	StripePublishableKey       *string `json:"stripe_publishable_key"`
	StripeSecretKey            *string `json:"stripe_secret_key"`
	StripeWebhookSecret        *string `json:"stripe_webhook_secret"`
	RazorpayEnabled            bool    `json:"razorpay_enabled"`
	RazorpayKeyID              *string `json:"razorpay_key_id"`
	RazorpayKeySecret          *string `json:"razorpay_key_secret"`
	CashEnabled                bool    `json:"cash_enabled"`
	CardEnabled                bool    `json:"card_enabled"`
	BankTransferEnabled        bool    `json:"bank_transfer_enabled"`
	DepositType                string  `json:"deposit_type"`
	DepositValue               float64 `json:"deposit_value"`
	CancellationFreeHours      int     `json:"cancellation_free_hours"`
	CancellationPenaltyPercent float64 `json:"cancellation_penalty_percent"`
}

func (h *OperationsHandler) UpdatePaymentSettings(c *fiber.Ctx) error {
	if !h.requireHotelAdmin(c) {
		return nil
	}

	var req updatePaymentSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	activeGateway := normalizeGateway(req.ActiveGateway)
	if activeGateway == "" {
		return response.Error(c, fiber.StatusUnprocessableEntity, "active_gateway must be none, stripe, razorpay, cash, card, or bank_transfer")
	}
	currency := strings.ToUpper(strings.TrimSpace(req.DefaultCurrency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return response.Error(c, fiber.StatusUnprocessableEntity, "default_currency must be a 3-letter ISO currency code")
	}
	gatewayMode := strings.ToLower(strings.TrimSpace(req.GatewayMode))
	if gatewayMode != "live" {
		gatewayMode = "test"
	}
	depositType := strings.ToLower(strings.TrimSpace(req.DepositType))
	if depositType != "fixed" {
		depositType = "percentage"
	}
	if req.CancellationFreeHours <= 0 {
		req.CancellationFreeHours = 24
	}

	stripeSecret, err := h.encryptSetting(req.StripeSecretKey)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "unable to protect Stripe secret")
	}
	stripeWebhook, err := h.encryptSetting(req.StripeWebhookSecret)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "unable to protect Stripe webhook secret")
	}
	razorpaySecret, err := h.encryptSetting(req.RazorpayKeySecret)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "unable to protect Razorpay secret")
	}

	if err := h.ensurePaymentConfig(c); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer tx.Rollback(c.Context())

	_, err = tx.Exec(c.Context(), `
		UPDATE hotels
		SET currency = $1,
		    country = COALESCE($2, country),
		    active_payment_gateway = $3,
		    stripe_enabled = $4,
		    stripe_account_id = NULLIF($5, ''),
		    razorpay_enabled = $6,
		    razorpay_key_id = NULLIF($7, ''),
		    updated_at = now()
		WHERE id = $8`,
		currency, nullableSettingString(req.Country), activeGateway, req.StripeEnabled, nullableSettingString(req.StripeAccountID),
		req.RazorpayEnabled, nullableSettingString(req.RazorpayKeyID), postgres.DemoHotelID,
	)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	_, err = tx.Exec(c.Context(), `
		INSERT INTO payment_configs (
			hotel_id, active_gateway, default_currency, gateway_mode,
			stripe_enabled, stripe_account_id, stripe_publishable_key, stripe_secret_key_encrypted, stripe_webhook_secret_encrypted,
			razorpay_enabled, razorpay_key_id, razorpay_key_secret_encrypted,
			cash_enabled, card_enabled, bank_transfer_enabled,
			deposit_type, deposit_value, cancellation_free_hours, cancellation_penalty_percent,
			updated_at
		) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,NULLIF($11,''),$12,$13,$14,$15,$16,$17,$18,$19,now())
		ON CONFLICT (hotel_id) DO UPDATE
		  SET active_gateway = EXCLUDED.active_gateway,
		      default_currency = EXCLUDED.default_currency,
		      gateway_mode = EXCLUDED.gateway_mode,
		      stripe_enabled = EXCLUDED.stripe_enabled,
		      stripe_account_id = EXCLUDED.stripe_account_id,
		      stripe_publishable_key = EXCLUDED.stripe_publishable_key,
		      stripe_secret_key_encrypted = COALESCE(EXCLUDED.stripe_secret_key_encrypted, payment_configs.stripe_secret_key_encrypted),
		      stripe_webhook_secret_encrypted = COALESCE(EXCLUDED.stripe_webhook_secret_encrypted, payment_configs.stripe_webhook_secret_encrypted),
		      razorpay_enabled = EXCLUDED.razorpay_enabled,
		      razorpay_key_id = EXCLUDED.razorpay_key_id,
		      razorpay_key_secret_encrypted = COALESCE(EXCLUDED.razorpay_key_secret_encrypted, payment_configs.razorpay_key_secret_encrypted),
		      cash_enabled = EXCLUDED.cash_enabled,
		      card_enabled = EXCLUDED.card_enabled,
		      bank_transfer_enabled = EXCLUDED.bank_transfer_enabled,
		      deposit_type = EXCLUDED.deposit_type,
		      deposit_value = EXCLUDED.deposit_value,
		      cancellation_free_hours = EXCLUDED.cancellation_free_hours,
		      cancellation_penalty_percent = EXCLUDED.cancellation_penalty_percent,
		      updated_at = now()`,
		postgres.DemoHotelID, activeGateway, currency, gatewayMode,
		req.StripeEnabled, nullableSettingString(req.StripeAccountID), nullableSettingString(req.StripePublishableKey), stripeSecret, stripeWebhook,
		req.RazorpayEnabled, nullableSettingString(req.RazorpayKeyID), razorpaySecret,
		req.CashEnabled, req.CardEnabled, req.BankTransferEnabled,
		depositType, req.DepositValue, req.CancellationFreeHours, req.CancellationPenaltyPercent,
	)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	if err := tx.Commit(c.Context()); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	h.audit(c, postgres.DemoHotelID, nil, "UPDATE", "payment_configs", postgres.DemoHotelID, map[string]interface{}{
		"active_gateway":        activeGateway,
		"default_currency":      currency,
		"country":               req.Country,
		"gateway_mode":          gatewayMode,
		"stripe_enabled":        req.StripeEnabled,
		"razorpay_enabled":      req.RazorpayEnabled,
		"cash_enabled":          req.CashEnabled,
		"card_enabled":          req.CardEnabled,
		"bank_transfer_enabled": req.BankTransferEnabled,
	})

	return h.PaymentSettings(c)
}

type rolePortalDefault struct {
	Role           string
	Label          string
	Description    string
	DefaultPath    string
	VisibleModules []string
}

var rolePortalDefaults = []rolePortalDefault{
	{
		Role:           "super_admin",
		Label:          "Hotel Admin (Owner)",
		Description:    "Owner control across setup, staff, reports, payments, and all operations.",
		DefaultPath:    "/dashboard",
		VisibleModules: []string{"dashboard", "rooms", "guests", "housekeeping", "maintenance", "complaints", "payments", "menu", "inventory", "order_queue", "reports", "settings", "staff"},
	},
	{
		Role:           "hotel_admin",
		Label:          "Hotel Admin",
		Description:    "Owns hotel setup, billing, staff, and operations.",
		DefaultPath:    "/dashboard",
		VisibleModules: []string{"dashboard", "rooms", "guests", "housekeeping", "maintenance", "complaints", "payments", "menu", "inventory", "order_queue", "reports", "settings", "staff"},
	},
	{
		Role:           "property_manager",
		Label:          "Property Manager",
		Description:    "Manages property operations and reports.",
		DefaultPath:    "/dashboard",
		VisibleModules: []string{"dashboard", "staff", "rooms", "guests", "housekeeping", "maintenance", "complaints", "payments", "menu", "inventory", "order_queue", "reports"},
	},
	{
		Role:           "receptionist",
		Label:          "Receptionist",
		Description:    "Front desk operations, check-in/out, and guest management.",
		DefaultPath:    "/guests",
		VisibleModules: []string{"dashboard", "staff", "rooms", "guests", "complaints", "payments"},
	},
	{
		Role:           "admin",
		Label:          "Receptionist (Legacy)",
		Description:    "Front desk operations, check-in/out, and guest management.",
		DefaultPath:    "/guests",
		VisibleModules: []string{"dashboard", "staff", "rooms", "guests", "complaints", "payments"},
	},
	{
		Role:           "housekeeping",
		Label:          "Housekeeping",
		Description:    "Room readiness and housekeeping assignments.",
		DefaultPath:    "/housekeeping",
		VisibleModules: []string{"staff", "housekeeping"},
	},
	{
		Role:           "maintenance",
		Label:          "Maintenance",
		Description:    "Work orders and maintenance queues.",
		DefaultPath:    "/maintenance",
		VisibleModules: []string{"staff", "maintenance"},
	},
	{
		Role:           "food_manager",
		Label:          "Food Manager",
		Description:    "Menu CRUD, recipes, suppliers, and food inventory.",
		DefaultPath:    "/menu",
		VisibleModules: []string{"dashboard", "staff", "menu", "inventory", "complaints"},
	},
	{
		Role:           "kitchen_manager",
		Label:          "Kitchen Manager",
		Description:    "Live order queue, cooking workflow, and inventory awareness.",
		DefaultPath:    "/kitchen",
		VisibleModules: []string{"dashboard", "staff", "order_queue", "inventory"},
	},
	{
		Role:           "waiter",
		Label:          "Waiter/Kooli",
		Description:    "Delivery assignments, pickup/delivery status, and active service queue.",
		DefaultPath:    "/kitchen",
		VisibleModules: []string{"staff", "order_queue"},
	},
}

var rolePortalDefaultsByRole = func() map[string]rolePortalDefault {
	out := make(map[string]rolePortalDefault, len(rolePortalDefaults))
	for _, item := range rolePortalDefaults {
		out[item.Role] = item
	}
	return out
}()

var rolePortalModulePath = map[string]string{
	"dashboard":    "/dashboard",
	"rooms":        "/rooms",
	"guests":       "/guests",
	"housekeeping": "/housekeeping",
	"maintenance":  "/maintenance",
	"complaints":   "/complaints",
	"payments":     "/payments",
	"menu":         "/menu",
	"inventory":    "/inventory",
	"order_queue":  "/kitchen",
	"reports":      "/reports",
	"settings":     "/settings",
	"staff":        "/staff",
	"platform":     "/platform",
}

type updateRolePortalSettingsRequest struct {
	Role           string   `json:"role"`
	DefaultPath    string   `json:"default_path"`
	VisibleModules []string `json:"visible_modules"`
}

func (h *OperationsHandler) RolePortalSettings(c *fiber.Ctx) error {
	if !requireAuthenticatedRequest(c, h.secretKey) {
		return nil
	}
	if err := h.ensureRolePortalSettings(c); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	rows, err := h.pool.Query(c.Context(), `
		SELECT role, default_path, visible_modules, locked, updated_at
		FROM role_portal_settings
		WHERE hotel_id = $1`,
		postgres.DemoHotelID,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	settingsByRole := map[string]map[string]interface{}{}
	for rows.Next() {
		var role, defaultPath string
		var rawModules []byte
		var locked bool
		var updatedAt time.Time
		if err := rows.Scan(&role, &defaultPath, &rawModules, &locked, &updatedAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, err.Error())
		}
		settingsByRole[role] = rolePortalPayload(role, defaultPath, rawModules, locked, updatedAt)
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	out := make([]map[string]interface{}, 0, len(rolePortalDefaults))
	for _, def := range rolePortalDefaults {
		if item, ok := settingsByRole[def.Role]; ok {
			out = append(out, item)
			continue
		}
		raw, _ := json.Marshal(def.VisibleModules)
		out = append(out, rolePortalPayload(def.Role, def.DefaultPath, raw, false, time.Now().UTC()))
	}
	return response.OK(c, out)
}

func (h *OperationsHandler) UpdateRolePortalSettings(c *fiber.Ctx) error {
	if !h.requireHotelAdmin(c) {
		return nil
	}
	var req updateRolePortalSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	def, ok := rolePortalDefaultsByRole[role]
	if !ok {
		return response.Error(c, fiber.StatusUnprocessableEntity, "unknown role")
	}
	defaultPath := strings.TrimSpace(req.DefaultPath)
	if defaultPath == "" {
		defaultPath = def.DefaultPath
	}
	if !rolePortalPathAllowed(defaultPath, def.VisibleModules) {
		return response.Error(c, fiber.StatusUnprocessableEntity, "default_path must point to a module this role can use")
	}
	visibleModules := sanitizeRolePortalModules(req.VisibleModules, def.VisibleModules, defaultPath)
	rawModules, _ := json.Marshal(visibleModules)

	if err := h.ensureRolePortalSettings(c); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	var updatedAt time.Time
	var locked bool
	err := h.pool.QueryRow(c.Context(), `
		INSERT INTO role_portal_settings (hotel_id, role, default_path, visible_modules, locked, updated_at)
		VALUES ($1,$2,$3,$4::jsonb,false,now())
		ON CONFLICT (hotel_id, role) DO UPDATE
		  SET default_path = EXCLUDED.default_path,
		      visible_modules = EXCLUDED.visible_modules,
		      updated_at = now()
		RETURNING locked, updated_at`,
		postgres.DemoHotelID, role, defaultPath, string(rawModules),
	).Scan(&locked, &updatedAt)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	h.audit(c, postgres.DemoHotelID, nil, "UPDATE", "role_portal_settings", postgres.DemoHotelID, map[string]interface{}{
		"role":            role,
		"default_path":    defaultPath,
		"visible_modules": visibleModules,
	})

	return response.OK(c, rolePortalPayload(role, defaultPath, rawModules, locked, updatedAt))
}

func (h *OperationsHandler) ensureRolePortalSettings(c *fiber.Ctx) error {
	if _, err := h.pool.Exec(c.Context(), `
		CREATE TABLE IF NOT EXISTS role_portal_settings (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			hotel_id UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
			role VARCHAR(50) NOT NULL,
			default_path TEXT NOT NULL,
			visible_modules JSONB NOT NULL DEFAULT '[]',
			locked BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (hotel_id, role)
		)`); err != nil {
		return err
	}
	for _, def := range rolePortalDefaults {
		raw, _ := json.Marshal(def.VisibleModules)
		if _, err := h.pool.Exec(c.Context(), `
			INSERT INTO role_portal_settings (hotel_id, role, default_path, visible_modules)
			VALUES ($1,$2,$3,$4::jsonb)
			ON CONFLICT (hotel_id, role) DO NOTHING`,
			postgres.DemoHotelID, def.Role, def.DefaultPath, string(raw),
		); err != nil {
			return err
		}
	}
	return nil
}

func rolePortalPayload(role, defaultPath string, rawModules []byte, locked bool, updatedAt time.Time) map[string]interface{} {
	def := rolePortalDefaultsByRole[role]
	visibleModules := []string{}
	_ = json.Unmarshal(rawModules, &visibleModules)
	if len(visibleModules) == 0 {
		visibleModules = append(visibleModules, def.VisibleModules...)
	}
	return map[string]interface{}{
		"role":            role,
		"label":           def.Label,
		"description":     def.Description,
		"default_path":    defaultPath,
		"visible_modules": visibleModules,
		"locked":          locked,
		"updated_at":      updatedAt,
	}
}

func sanitizeRolePortalModules(input, allowed []string, defaultPath string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, module := range allowed {
		allowedSet[module] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(input))
	for _, module := range input {
		module = strings.TrimSpace(module)
		if _, ok := allowedSet[module]; !ok {
			continue
		}
		if _, exists := seen[module]; exists {
			continue
		}
		seen[module] = struct{}{}
		out = append(out, module)
	}
	defaultModule := rolePortalModuleByPath(defaultPath)
	if defaultModule != "" {
		if _, ok := seen[defaultModule]; !ok {
			out = append([]string{defaultModule}, out...)
		}
	}
	if len(out) == 0 && len(allowed) > 0 {
		out = append(out, allowed[0])
	}
	return out
}

func rolePortalPathAllowed(path string, modules []string) bool {
	for _, module := range modules {
		if rolePortalModulePath[module] == path {
			return true
		}
	}
	return false
}

func rolePortalModuleByPath(path string) string {
	for module, modulePath := range rolePortalModulePath {
		if modulePath == path {
			return module
		}
	}
	return ""
}

func (h *OperationsHandler) requireHotelAdmin(c *fiber.Ctx) bool {
	claims, err := jwtClaimsFromRequest(c, h.secretKey)
	if err != nil {
		_ = response.Error(c, fiber.StatusUnauthorized, "invalid staff token")
		return false
	}

	rawRoles, ok := claims["roles"].([]interface{})
	if !ok {
		_ = response.Error(c, fiber.StatusForbidden, "hotel admin role is required")
		return false
	}
	for _, rawRole := range rawRoles {
		role, _ := rawRole.(string)
		switch role {
		case "platform_admin", "hotel_admin", "super_admin":
			return true
		}
	}
	_ = response.Error(c, fiber.StatusForbidden, "hotel admin role is required")
	return false
}

func (h *OperationsHandler) reportCounts(c *fiber.Ctx, name, sql string, keys []string) error {
	values := make([]interface{}, len(keys))
	ptrs := make([]interface{}, len(keys))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := h.pool.QueryRow(c.Context(), sql, postgres.DemoHotelID).Scan(ptrs...); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	data := map[string]interface{}{"report": name}
	for i, key := range keys {
		data[key] = values[i]
	}
	return response.OK(c, data)
}

func (h *OperationsHandler) audit(c *fiber.Ctx, hotelID uuid.UUID, userID *uuid.UUID, action, resource string, resourceID uuid.UUID, newData map[string]interface{}) {
	data, _ := json.Marshal(newData)
	_, _ = h.pool.Exec(c.Context(), `
		INSERT INTO audit_logs (
			id, hotel_id, user_id, action, table_name, record_id, resource_type, resource_id,
			new_data, user_agent, ai_triggered, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,false,now())`,
		uuid.New(), hotelID, userID, action, resource, resourceID, resource, resourceID, data, c.Get("User-Agent"),
	)
}

func (h *OperationsHandler) ensurePaymentConfig(c *fiber.Ctx) error {
	_, err := h.pool.Exec(c.Context(), `
		INSERT INTO payment_configs (hotel_id, default_currency)
		SELECT id, COALESCE(currency, 'USD') FROM hotels WHERE id = $1
		ON CONFLICT (hotel_id) DO NOTHING`,
		postgres.DemoHotelID,
	)
	return err
}

func normalizeGateway(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "none":
		return "none"
	case "stripe", "razorpay", "cash", "card", "bank_transfer":
		return value
	default:
		return ""
	}
}

func nullableSettingString(value *string) interface{} {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func (h *OperationsHandler) encryptSetting(value *string) (*string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	keyMaterial := h.secretKey
	if keyMaterial == "" {
		keyMaterial = "hotelops-local-development-secret"
	}
	key := sha256.Sum256([]byte(keyMaterial))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(strings.TrimSpace(*value)), nil)
	encoded := fmt.Sprintf("v1:%s:%s", base64.RawStdEncoding.EncodeToString(nonce), base64.RawStdEncoding.EncodeToString(ciphertext))
	return &encoded, nil
}

func nullableText(value string) interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
