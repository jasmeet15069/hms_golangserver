package handler

import (
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/hotelharmony/api/internal/domain"
	"github.com/hotelharmony/api/internal/repository/postgres"
	"github.com/hotelharmony/api/pkg/response"
)

type HotelHandler struct {
	hotels postgres.HotelRepository
}

func NewHotelHandler(hotels postgres.HotelRepository) *HotelHandler {
	return &HotelHandler{hotels: hotels}
}

func (h *HotelHandler) Register(r fiber.Router) {
	r.Get("/hotel/branding", h.Branding)
	r.Put("/hotel/branding", h.UpdateBranding)
	r.Post("/onboarding/hotel", h.CreateHotel)
}

type createHotelRequest struct {
	Name           string  `json:"name"`
	Slug           string  `json:"slug"`
	PlanTier       string  `json:"plan_tier"`
	LogoURL        *string `json:"logo_url"`
	PrimaryColor   *string `json:"primary_color"`
	Address        *string `json:"address"`
	Country        *string `json:"country"`
	Timezone       *string `json:"timezone"`
	Currency       *string `json:"currency"`
	Phone          *string `json:"phone"`
	Email          *string `json:"email"`
	Website        *string `json:"website"`
	WelcomeMessage *string `json:"welcome_message"`
	FooterText     *string `json:"footer_text"`
	Property       *struct {
		Name       string  `json:"name"`
		Address    *string `json:"address"`
		StarRating *int    `json:"star_rating"`
		TotalRooms *int    `json:"total_rooms"`
	} `json:"property"`
}

func (h *HotelHandler) CreateHotel(c *fiber.Ctx) error {
	var req createHotelRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return response.Error(c, fiber.StatusUnprocessableEntity, "hotel name is required")
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = normalizeSlug(req.Name)
	}
	if slug == "" {
		return response.Error(c, fiber.StatusUnprocessableEntity, "hotel slug is required")
	}

	plan := strings.ToLower(strings.TrimSpace(req.PlanTier))
	if plan == "" {
		plan = "starter"
	}

	hotel, err := h.hotels.CreateHotel(c.Context(), &domain.Hotel{
		Name:         req.Name,
		Slug:         slug,
		PlanTier:     plan,
		LogoURL:      req.LogoURL,
		PrimaryColor: defaultStringPtr(req.PrimaryColor, "#000000"),
		Address:      req.Address,
		Country:      req.Country,
		Timezone:     defaultStringPtr(req.Timezone, "UTC"),
		Currency:     defaultStringPtr(req.Currency, "USD"),
		Phone:        req.Phone,
		Email:        req.Email,
		Website:      req.Website,
		Settings:     planSettings(plan),
	})
	if err != nil {
		status := fiber.StatusBadRequest
		if err == postgres.ErrConflict {
			status = fiber.StatusConflict
		}
		return response.Error(c, status, err.Error())
	}

	var property *domain.Property
	if req.Property != nil && strings.TrimSpace(req.Property.Name) != "" {
		property, err = h.hotels.CreateProperty(c.Context(), &domain.Property{
			HotelID:    hotel.ID,
			Name:       strings.TrimSpace(req.Property.Name),
			Address:    req.Property.Address,
			StarRating: req.Property.StarRating,
			TotalRooms: req.Property.TotalRooms,
		})
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, err.Error())
		}
	}

	branding, err := h.hotels.UpsertBranding(c.Context(), &domain.HotelBranding{
		HotelID:        hotel.ID,
		LogoURL:        req.LogoURL,
		PrimaryColor:   deref(defaultStringPtr(req.PrimaryColor, "#000000")),
		WelcomeMessage: req.WelcomeMessage,
		FooterText:     req.FooterText,
	})
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.Created(c, map[string]interface{}{
		"hotel":    hotel,
		"property": property,
		"branding": branding,
	})
}

func (h *HotelHandler) Branding(c *fiber.Ctx) error {
	slug := c.Query("slug")
	if slug != "" {
		branding, err := h.hotels.FindBrandingBySlug(c.Context(), slug)
		if err != nil {
			return response.Error(c, fiber.StatusNotFound, "hotel branding not found")
		}
		return response.OK(c, branding)
	}

	hotelID := postgres.DemoHotelID
	if raw := c.Query("hotel_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid hotel id")
		}
		hotelID = parsed
	}
	branding, err := h.hotels.FindBrandingByHotelID(c.Context(), hotelID)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "hotel branding not found")
	}
	return response.OK(c, branding)
}

type updateBrandingRequest struct {
	HotelID        string  `json:"hotel_id"`
	LogoURL        *string `json:"logo_url"`
	PrimaryColor   string  `json:"primary_color"`
	WelcomeMessage *string `json:"welcome_message"`
	FooterText     *string `json:"footer_text"`
}

func (h *HotelHandler) UpdateBranding(c *fiber.Ctx) error {
	var req updateBrandingRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	hotelID := postgres.DemoHotelID
	if strings.TrimSpace(req.HotelID) != "" {
		parsed, err := uuid.Parse(req.HotelID)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid hotel id")
		}
		hotelID = parsed
	}
	color := strings.TrimSpace(req.PrimaryColor)
	if color == "" {
		color = "#000000"
	}
	branding, err := h.hotels.UpsertBranding(c.Context(), &domain.HotelBranding{
		HotelID:        hotelID,
		LogoURL:        req.LogoURL,
		PrimaryColor:   color,
		WelcomeMessage: req.WelcomeMessage,
		FooterText:     req.FooterText,
	})
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.OK(c, branding)
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugPattern.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func planSettings(plan string) map[string]interface{} {
	switch plan {
	case "growth":
		return map[string]interface{}{"max_rooms": 200, "max_properties": 3, "ai_addon": false}
	case "enterprise":
		return map[string]interface{}{"max_rooms": nil, "max_properties": nil, "ai_addon": false}
	default:
		return map[string]interface{}{"max_rooms": 50, "max_properties": 1, "ai_addon": false}
	}
}

func defaultStringPtr(value *string, fallback string) *string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return value
	}
	return &fallback
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
