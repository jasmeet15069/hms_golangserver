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

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
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

func (h *OperationsHandler) requireHotelAdmin(c *fiber.Ctx) bool {
	authHeader := c.Get("Authorization")
	tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if tokenString == "" || h.secretKey == "" {
		_ = response.Error(c, fiber.StatusUnauthorized, "staff authentication is required")
		return false
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("invalid signing method")
		}
		return []byte(h.secretKey), nil
	})
	if err != nil || !token.Valid {
		_ = response.Error(c, fiber.StatusUnauthorized, "invalid staff token")
		return false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
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
