package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/hotelharmony/api/internal/domain"
	"github.com/hotelharmony/api/internal/repository/postgres"
	"github.com/hotelharmony/api/internal/service"
	"github.com/hotelharmony/api/pkg/response"
	"github.com/hotelharmony/api/pkg/validator"
)

type UserHandler struct {
	userRepo postgres.UserRepository
	authSvc  service.AuthService
	validate *validator.Validator
}

func NewUserHandler(userRepo postgres.UserRepository, authSvc service.AuthService, validate *validator.Validator) *UserHandler {
	return &UserHandler{userRepo: userRepo, authSvc: authSvc, validate: validate}
}

func (h *UserHandler) Register(r fiber.Router) {
	r.Get("/users", h.List)
	r.Get("/users/:id", h.Get)
	r.Patch("/users/:id", h.Update)
	r.Post("/users/:id/roles", h.AddRole)
	r.Delete("/users/:id/roles/:role", h.RemoveRole)
}

type userListItem struct {
	ID       uuid.UUID         `json:"id"`
	Email    string            `json:"email"`
	FullName string            `json:"full_name"`
	Phone    *string           `json:"phone,omitempty"`
	Roles    []domain.UserRole `json:"roles"`
	JoinedAt string            `json:"joined_at"`
}

func (h *UserHandler) List(c *fiber.Ctx) error {
	users, err := h.userRepo.List(c.Context(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list users")
	}

	items := make([]userListItem, 0, len(users))
	for _, u := range users {
		profile, _ := h.userRepo.FindProfileByUserID(c.Context(), u.ID)
		roles, _ := h.userRepo.GetRoles(c.Context(), u.ID)
		fullName := ""
		if profile != nil {
			fullName = profile.FullName
		}
		items = append(items, userListItem{
			ID:       u.ID,
			Email:    u.Email,
			FullName: fullName,
			Phone:    nil,
			Roles:    roles,
			JoinedAt: u.CreatedAt.Format("2006-01-02"),
		})
	}
	return response.OK(c, items)
}

func (h *UserHandler) Get(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid user id")
	}

	user, err := h.userRepo.FindByID(c.Context(), id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "user not found")
	}

	profile, _ := h.userRepo.FindProfileByUserID(c.Context(), id)
	roles, _ := h.userRepo.GetRoles(c.Context(), id)

	return response.OK(c, map[string]interface{}{
		"id":             user.ID,
		"email":          user.Email,
		"platform_admin": user.PlatformAdmin,
		"profile":        profile,
		"roles":          roles,
		"created_at":     user.CreatedAt,
	})
}

type updateUserRequest struct {
	FullName *string `json:"full_name,omitempty"`
	Phone    *string `json:"phone,omitempty"`
}

func (h *UserHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid user id")
	}

	var req updateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	fields := make(map[string]interface{})
	if req.FullName != nil {
		fields["full_name"] = *req.FullName
	}
	if req.Phone != nil {
		fields["phone"] = *req.Phone
	}

	if len(fields) > 0 {
		if err := h.userRepo.UpdateProfile(c.Context(), id, fields); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update user")
		}
	}

	profile, _ := h.userRepo.FindProfileByUserID(c.Context(), id)
	return response.OK(c, profile)
}

func (h *UserHandler) AddRole(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid user id")
	}

	var req struct {
		Role domain.UserRole `json:"role"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Role == "" {
		return response.Error(c, fiber.StatusBadRequest, "role is required")
	}

	if err := h.userRepo.AddRole(c.Context(), id, req.Role); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to add role")
	}

	roles, _ := h.userRepo.GetRoles(c.Context(), id)
	return response.OK(c, roles)
}

func (h *UserHandler) RemoveRole(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid user id")
	}

	role := domain.UserRole(c.Params("role"))
	if role == "" {
		return response.Error(c, fiber.StatusBadRequest, "role is required")
	}

	if err := h.userRepo.RemoveRole(c.Context(), id, role); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to remove role")
	}

	roles, _ := h.userRepo.GetRoles(c.Context(), id)
	return response.OK(c, roles)
}
