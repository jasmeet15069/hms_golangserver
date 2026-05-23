package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/hotelharmony/api/internal/service"
	"github.com/hotelharmony/api/pkg/response"
	"github.com/hotelharmony/api/pkg/validator"
)

type AuthHandler struct {
	auth     service.AuthService
	validate *validator.Validator
}

func NewAuthHandler(auth service.AuthService, validate *validator.Validator) *AuthHandler {
	return &AuthHandler{auth: auth, validate: validate}
}

func (h *AuthHandler) Register(r fiber.Router) {
	r.Post("/auth/sign-up", h.SignUp)
	r.Post("/auth/sign-in", h.SignIn)
	r.Post("/auth/refresh", h.Refresh)
	r.Patch("/auth/user", h.UpdatePassword)
}

type signUpRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
	FullName string `json:"full_name"`
}

func (h *AuthHandler) SignUp(c *fiber.Ctx) error {
	var req signUpRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	session, err := h.auth.SignUp(c.Context(), req.Email, req.Password, req.FullName)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.OK(c, session)
}

type signInRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

func (h *AuthHandler) SignIn(c *fiber.Ctx) error {
	var req signInRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validate.Struct(req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	session, err := h.auth.SignIn(c.Context(), req.Email, req.Password)
	if err != nil {
		return response.Error(c, fiber.StatusUnauthorized, err.Error())
	}
	return response.OK(c, session)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	var req refreshRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	session, err := h.auth.RefreshSession(c.Context(), req.RefreshToken)
	if err != nil {
		return response.Error(c, fiber.StatusUnauthorized, err.Error())
	}
	return response.OK(c, session)
}

type updatePasswordRequest struct {
	UserID          string `json:"user_id" validate:"required"`
	Password        string `json:"password" validate:"required,min=8"`
	CurrentPassword string `json:"current_password"`
}

func (h *AuthHandler) UpdatePassword(c *fiber.Ctx) error {
	var req updatePasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	id, err := uuid.Parse(req.UserID)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid user id")
	}
	var updateErr error
	if req.CurrentPassword != "" {
		updateErr = h.auth.UpdatePasswordWithCurrent(c.Context(), id, req.CurrentPassword, req.Password)
	} else {
		updateErr = h.auth.UpdatePassword(c.Context(), id, req.Password)
	}
	if updateErr != nil {
		return response.Error(c, fiber.StatusBadRequest, updateErr.Error())
	}
	return response.OK(c, map[string]string{"status": "updated"})
}
