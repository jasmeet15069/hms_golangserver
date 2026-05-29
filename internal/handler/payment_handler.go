package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/hotelharmony/api/internal/config"
	"github.com/hotelharmony/api/internal/service"
	"github.com/hotelharmony/api/pkg/response"
)

type PaymentHandler struct {
	payments  service.PaymentService
	secretKey string
}

func NewPaymentHandler(payments service.PaymentService, cfg *config.Config) *PaymentHandler {
	secret := ""
	if cfg != nil {
		secret = cfg.Auth.AccessTokenSecret
	}
	return &PaymentHandler{payments: payments, secretKey: secret}
}

func (h *PaymentHandler) Register(r fiber.Router) {
	r.Get("/payment-config", h.Config)
	r.Get("/exchange-rate", h.ExchangeRate)
	r.Post("/bookings/checkout", h.BookingCheckout)
	r.Post("/payments/checkout", h.PaymentCheckout)
	r.Post("/payments/complete", h.CompletePayment)
}

func (h *PaymentHandler) Config(c *fiber.Ctx) error {
	return response.OK(c, h.payments.GetConfig(c.Context()))
}

func (h *PaymentHandler) ExchangeRate(c *fiber.Ctx) error {
	base := c.Query("base", "USD")
	target := c.Query("target", "USD")
	rate, err := h.payments.GetExchangeRate(c.Context(), base, target)
	if err != nil {
		return response.Error(c, fiber.StatusBadGateway, err.Error())
	}
	return response.OK(c, map[string]interface{}{"base": base, "target": target, "rate": rate})
}

type bookingCheckoutRequest struct {
	RoomID       string `json:"room_id"`
	UserID       string `json:"user_id"`
	Currency     string `json:"currency"`
	CheckInDate  string `json:"check_in_date"`
	CheckOutDate string `json:"check_out_date"`
	GuestName    string `json:"guest_name"`
	GuestEmail   string `json:"guest_email"`
	GuestPhone   string `json:"guest_phone"`
	Country      string `json:"country"`
}

func (h *PaymentHandler) BookingCheckout(c *fiber.Ctx) error {
	if !requireAuthenticatedRequest(c, h.secretKey) {
		return nil
	}

	var req bookingCheckoutRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	roomID, err := uuid.Parse(req.RoomID)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid room id")
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid user id")
	}
	checkIn, err := parseDate(req.CheckInDate)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid check-in date")
	}
	checkOut, err := parseDate(req.CheckOutDate)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid check-out date")
	}
	result, err := h.payments.BookingCheckout(c.Context(), service.BookingCheckoutRequest{
		RoomID:       roomID,
		UserID:       userID,
		Currency:     req.Currency,
		CheckInDate:  checkIn,
		CheckOutDate: checkOut,
		GuestName:    req.GuestName,
		GuestEmail:   req.GuestEmail,
		GuestPhone:   req.GuestPhone,
		Country:      req.Country,
		OriginURL:    origin(c),
	})
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.OK(c, result)
}

type paymentCheckoutRequest struct {
	PaymentID string `json:"payment_id"`
	Currency  string `json:"currency"`
	Country   string `json:"country"`
}

func (h *PaymentHandler) PaymentCheckout(c *fiber.Ctx) error {
	if !requireAuthenticatedRequest(c, h.secretKey) {
		return nil
	}

	var req paymentCheckoutRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	paymentID, err := uuid.Parse(req.PaymentID)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid payment id")
	}
	result, err := h.payments.PaymentCheckout(c.Context(), paymentID, req.Currency, req.Country, origin(c))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.OK(c, result)
}

type completePaymentRequest struct {
	PaymentID string `json:"payment_id"`
	SessionID string `json:"session_id"`
}

func (h *PaymentHandler) CompletePayment(c *fiber.Ctx) error {
	if !requireAuthenticatedRequest(c, h.secretKey) {
		return nil
	}

	var req completePaymentRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	paymentID, err := uuid.Parse(req.PaymentID)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid payment id")
	}
	if err := h.payments.CompletePayment(c.Context(), paymentID, req.SessionID); err != nil {
		return response.Error(c, fiber.StatusConflict, err.Error())
	}
	return response.OK(c, map[string]string{"status": "completed"})
}

func parseDate(value string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, value)
}

func origin(c *fiber.Ctx) string {
	if o := c.Get("Origin"); o != "" {
		return o
	}
	return "http://localhost:8080"
}
