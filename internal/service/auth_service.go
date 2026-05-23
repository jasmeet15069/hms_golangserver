// Package service contains all application business logic.
// Services orchestrate repositories and external integrations; they know
// nothing about HTTP (no fiber.Ctx, no request/response shapes).
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/hotelharmony/api/internal/cache"
	"github.com/hotelharmony/api/internal/config"
	"github.com/hotelharmony/api/internal/domain"
	"github.com/hotelharmony/api/internal/repository/postgres"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("a user with this email already exists")
	ErrTokenExpired       = errors.New("token has expired")
	ErrTokenInvalid       = errors.New("token is invalid")
	ErrForbidden          = errors.New("access denied")
)

// AuthClaims are embedded in every JWT.
type AuthClaims struct {
	jwt.RegisteredClaims
	UserID string   `json:"sub"`
	Email  string   `json:"email"`
	Roles  []string `json:"roles"`
}

// Session is returned to the client after successful authentication.
type Session struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresIn    int64        `json:"expires_in"`
	User         *SessionUser `json:"user"`
}

// SessionUser is the public-safe representation of the authenticated user.
type SessionUser struct {
	ID           string   `json:"id"`
	Email        string   `json:"email"`
	UserMetadata struct{} `json:"user_metadata"`
}

// AuthService handles sign-up, sign-in, token refresh and profile updates.
type AuthService interface {
	SignUp(ctx context.Context, email, password, fullName string) (*Session, error)
	SignIn(ctx context.Context, email, password string) (*Session, error)
	RefreshSession(ctx context.Context, refreshToken string) (*Session, error)
	GetUserFromToken(ctx context.Context, tokenStr string) (*domain.User, []domain.UserRole, error)
	UpdatePassword(ctx context.Context, userID uuid.UUID, newPassword string) error
	RevokeRefreshToken(ctx context.Context, refreshToken string) error
}

type authService struct {
	userRepo postgres.UserRepository
	cache    cache.Cache
	cfg      *config.Config
}

func NewAuthService(userRepo postgres.UserRepository, c cache.Cache, cfg *config.Config) AuthService {
	return &authService{userRepo: userRepo, cache: c, cfg: cfg}
}

// SignUp creates a new user, profile, assigns the guest role, and returns a session.
func (s *authService) SignUp(ctx context.Context, email, password, fullName string) (*Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, fmt.Errorf("email and password are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cfg.Auth.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("authService.SignUp hash: %w", err)
	}

	user, err := s.userRepo.Create(ctx, email, string(hash))
	if err != nil {
		if errors.Is(err, postgres.ErrConflict) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("authService.SignUp create: %w", err)
	}

	name := strings.TrimSpace(fullName)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	if _, err := s.userRepo.CreateProfile(ctx, user.ID, name, nil); err != nil {
		return nil, fmt.Errorf("authService.SignUp profile: %w", err)
	}

	if err := s.userRepo.AddRole(ctx, user.ID, domain.RoleGuest); err != nil {
		return nil, fmt.Errorf("authService.SignUp role: %w", err)
	}

	return s.buildSession(ctx, user, []domain.UserRole{domain.RoleGuest})
}

// SignIn validates credentials and returns a session.
func (s *authService) SignIn(ctx context.Context, email, password string) (*Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	roles, err := s.userRepo.GetRoles(ctx, user.ID)
	if err != nil {
		roles = []domain.UserRole{domain.RoleGuest}
	}

	return s.buildSession(ctx, user, roles)
}

// RefreshSession validates a refresh token and issues a new access+refresh pair.
func (s *authService) RefreshSession(ctx context.Context, refreshToken string) (*Session, error) {
	revoked, _ := s.cache.Exists(ctx, revokedKey(refreshToken))
	if revoked {
		return nil, ErrTokenInvalid
	}

	claims, err := s.parseToken(refreshToken, s.cfg.Auth.RefreshTokenSecret)
	if err != nil {
		return nil, err
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, ErrTokenInvalid
	}

	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, ErrTokenInvalid
	}

	roles, _ := s.userRepo.GetRoles(ctx, user.ID)
	_ = s.cache.Set(ctx, revokedKey(refreshToken), "1", s.cfg.Auth.RefreshTokenTTL)
	return s.buildSession(ctx, user, roles)
}

// GetUserFromToken validates an access token and returns the user + roles.
func (s *authService) GetUserFromToken(ctx context.Context, tokenStr string) (*domain.User, []domain.UserRole, error) {
	claims, err := s.parseToken(tokenStr, s.cfg.Auth.AccessTokenSecret)
	if err != nil {
		return nil, nil, err
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, nil, ErrTokenInvalid
	}

	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, nil, ErrTokenInvalid
	}

	roleStrs := claims.Roles
	roles := make([]domain.UserRole, len(roleStrs))
	for i, r := range roleStrs {
		roles[i] = domain.UserRole(r)
	}

	return user, roles, nil
}

// UpdatePassword hashes and persists a new password.
func (s *authService) UpdatePassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.cfg.Auth.BcryptCost)
	if err != nil {
		return err
	}
	return s.userRepo.UpdatePassword(ctx, userID, string(hash))
}

// RevokeRefreshToken adds a refresh token to the Redis deny-list.
func (s *authService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	return s.cache.Set(ctx, revokedKey(refreshToken), "1", s.cfg.Auth.RefreshTokenTTL)
}

func (s *authService) buildSession(ctx context.Context, user *domain.User, roles []domain.UserRole) (*Session, error) {
	roleStrs := make([]string, len(roles))
	for i, r := range roles {
		roleStrs[i] = string(r)
	}

	accessExp := time.Now().Add(s.cfg.Auth.AccessTokenTTL)
	accessToken, err := s.signToken(user.ID.String(), user.Email, roleStrs, accessExp, s.cfg.Auth.AccessTokenSecret)
	if err != nil {
		return nil, fmt.Errorf("buildSession access: %w", err)
	}

	refreshExp := time.Now().Add(s.cfg.Auth.RefreshTokenTTL)
	refreshToken, err := s.signToken(user.ID.String(), user.Email, roleStrs, refreshExp, s.cfg.Auth.RefreshTokenSecret)
	if err != nil {
		return nil, fmt.Errorf("buildSession refresh: %w", err)
	}

	return &Session{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "bearer",
		ExpiresIn:    int64(s.cfg.Auth.AccessTokenTTL.Seconds()),
		User: &SessionUser{
			ID:    user.ID.String(),
			Email: user.Email,
		},
	}, nil
}

func (s *authService) signToken(userID, email string, roles []string, exp time.Time, secret string) (string, error) {
	claims := AuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "hotel-harmony",
			Subject:   userID,
			ID:        uuid.New().String(),
		},
		UserID: userID,
		Email:  email,
		Roles:  roles,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func (s *authService) parseToken(tokenStr, secret string) (*AuthClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &AuthClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	claims, ok := token.Claims.(*AuthClaims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

func revokedKey(token string) string {
	if len(token) > 32 {
		return "revoked:" + token[len(token)-32:]
	}
	return "revoked:" + token
}

// HasRole returns true if the provided slice contains the required role.
func HasRole(roles []domain.UserRole, required ...domain.UserRole) bool {
	roleSet := make(map[domain.UserRole]struct{}, len(roles))
	for _, r := range roles {
		roleSet[r] = struct{}{}
	}
	if _, ok := roleSet[domain.RoleSuperAdmin]; ok {
		return true
	}
	if _, ok := roleSet[domain.RoleAdmin]; ok {
		return true
	}
	for _, r := range required {
		if _, ok := roleSet[r]; ok {
			return true
		}
	}
	return false
}
