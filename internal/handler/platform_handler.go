package handler

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/hotelharmony/api/pkg/response"
)

type platformTenantRequest struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	PlanTier string `json:"plan_tier"`
	Country  string `json:"country"`
	Currency string `json:"currency"`
}

type tenantPlanUpdateRequest struct {
	PlanTier string `json:"plan_tier"`
	IsActive *bool  `json:"is_active"`
}

func (h *OperationsHandler) PlatformPlans(c *fiber.Ctx) error {
	if !h.requirePlatformAdmin(c) {
		return nil
	}
	return response.OK(c, planTierSpecs)
}

func (h *OperationsHandler) PlatformTenants(c *fiber.Ctx) error {
	if !h.requirePlatformAdmin(c) {
		return nil
	}

	rows, err := h.pool.Query(c.Context(), `
		SELECT
			h.id, h.name, h.slug, h.plan_tier, h.is_active, h.settings,
			h.country, h.currency, h.created_at, h.updated_at,
			(SELECT COUNT(*) FROM rooms r WHERE r.hotel_id = h.id) AS rooms_used,
			(SELECT COUNT(*) FROM users u WHERE u.hotel_id = h.id) AS users_used,
			(SELECT COUNT(*) FROM properties p WHERE p.hotel_id = h.id) AS properties_used
		FROM hotels h
		ORDER BY h.created_at DESC, h.name ASC`)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	tenants := []map[string]interface{}{}
	for rows.Next() {
		var id uuid.UUID
		var name, slug, plan string
		var isActive bool
		var settingsBytes []byte
		var country, currency *string
		var createdAt, updatedAt time.Time
		var roomsUsed, usersUsed, propertiesUsed int
		if err := rows.Scan(
			&id, &name, &slug, &plan, &isActive, &settingsBytes,
			&country, &currency, &createdAt, &updatedAt,
			&roomsUsed, &usersUsed, &propertiesUsed,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, err.Error())
		}
		settings := map[string]interface{}{}
		_ = json.Unmarshal(settingsBytes, &settings)
		spec := planTierByID(plan)
		tenants = append(tenants, map[string]interface{}{
			"id":              id,
			"name":            name,
			"slug":            slug,
			"plan_tier":       normalizePlanTier(plan),
			"plan_name":       spec.Name,
			"is_active":       isActive,
			"country":         country,
			"currency":        currency,
			"settings":        settings,
			"rooms_used":      roomsUsed,
			"rooms_max":       settings["max_rooms"],
			"users_used":      usersUsed,
			"users_max":       settings["max_users"],
			"properties_used": propertiesUsed,
			"properties_max":  settings["max_properties"],
			"database_name":   settings["database_name"],
			"created_at":      createdAt,
			"updated_at":      updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.OK(c, tenants)
}

func (h *OperationsHandler) CreatePlatformTenant(c *fiber.Ctx) error {
	if !h.requirePlatformAdmin(c) {
		return nil
	}

	var req platformTenantRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return response.Error(c, fiber.StatusUnprocessableEntity, "client hotel name is required")
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = normalizeSlug(name)
	}
	if slug == "" {
		return response.Error(c, fiber.StatusUnprocessableEntity, "client slug is required")
	}
	plan := normalizePlanTier(req.PlanTier)
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return response.Error(c, fiber.StatusUnprocessableEntity, "currency must be a 3-letter code")
	}
	country := strings.TrimSpace(req.Country)
	settings, _ := json.Marshal(settingsForPlanTier(plan, slug))
	hotelID := uuid.New()

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	defer tx.Rollback(c.Context())

	if _, err := tx.Exec(c.Context(), `
		INSERT INTO hotels (
			id, name, slug, plan_tier, is_active, settings,
			country, timezone, currency, primary_color, active_payment_gateway,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,true,$5::jsonb,NULLIF($6,''),'UTC',$7,'#000000','none',now(),now())`,
		hotelID, name, slug, plan, string(settings), country, currency,
	); err != nil {
		return response.Error(c, fiber.StatusConflict, err.Error())
	}

	if _, err := tx.Exec(c.Context(), `
		INSERT INTO hotel_branding (
			hotel_id, primary_color, client_primary_color, admin_primary_color,
			welcome_message, footer_text, updated_at
		) VALUES ($1,'#000000','#000000','#000000',$2,'Powered by HotelOps',now())
		ON CONFLICT (hotel_id) DO NOTHING`,
		hotelID, "Welcome to "+name,
	); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	if _, err := tx.Exec(c.Context(), `
		INSERT INTO payment_configs (hotel_id, active_gateway, default_currency, gateway_mode)
		VALUES ($1,'none',$2,'test')
		ON CONFLICT (hotel_id) DO NOTHING`,
		hotelID, currency,
	); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	if err := tx.Commit(c.Context()); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	if err := h.ensureRolePortalSettingsForHotel(c, hotelID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Created(c, map[string]interface{}{
		"id":         hotelID,
		"name":       name,
		"slug":       slug,
		"plan_tier":  plan,
		"currency":   currency,
		"country":    nullableText(country),
		"settings":   settingsForPlanTier(plan, slug),
		"is_active":  true,
		"created_at": time.Now().UTC(),
	})
}

func (h *OperationsHandler) UpdateTenantPlan(c *fiber.Ctx) error {
	if !h.requirePlatformAdmin(c) {
		return nil
	}
	hotelID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid tenant id")
	}

	var req tenantPlanUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	plan := normalizePlanTier(req.PlanTier)

	var slug string
	err = h.pool.QueryRow(c.Context(), `SELECT slug FROM hotels WHERE id = $1`, hotelID).Scan(&slug)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "client tenant not found")
	}

	settings, _ := json.Marshal(settingsForPlanTier(plan, slug))
	if req.IsActive == nil {
		_, err = h.pool.Exec(c.Context(), `
			UPDATE hotels
			SET plan_tier = $1, settings = $2::jsonb, updated_at = now()
			WHERE id = $3`,
			plan, string(settings), hotelID,
		)
	} else {
		_, err = h.pool.Exec(c.Context(), `
			UPDATE hotels
			SET plan_tier = $1, settings = $2::jsonb, is_active = $3, updated_at = now()
			WHERE id = $4`,
			plan, string(settings), *req.IsActive, hotelID,
		)
	}
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.OK(c, map[string]interface{}{
		"id":        hotelID,
		"plan_tier": plan,
		"settings":  settingsForPlanTier(plan, slug),
	})
}

func (h *OperationsHandler) requirePlatformAdmin(c *fiber.Ctx) bool {
	claims, err := jwtClaimsFromRequest(c, h.secretKey)
	if err != nil {
		_ = response.Error(c, fiber.StatusUnauthorized, "platform admin token is required")
		return false
	}
	if platformAdmin, _ := claims["platform_admin"].(bool); platformAdmin {
		return true
	}
	rawRoles, ok := claims["roles"].([]interface{})
	if !ok {
		_ = response.Error(c, fiber.StatusForbidden, "platform admin role is required")
		return false
	}
	for _, rawRole := range rawRoles {
		if role, _ := rawRole.(string); role == "platform_admin" {
			return true
		}
	}
	_ = response.Error(c, fiber.StatusForbidden, "platform admin role is required")
	return false
}
