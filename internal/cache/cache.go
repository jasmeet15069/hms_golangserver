// Package cache provides a Redis-backed cache with typed methods, TTL strategy,
// and a simple distributed lock used for idempotency critical sections.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/hotelharmony/api/internal/config"
)

// TTL constants used across the application.
// Centralised here so changes are always global.
const (
	TTLDashboardStats   = 30 * time.Second
	TTLRoomList         = 60 * time.Second
	TTLMenuItems        = 5 * time.Minute
	TTLInventoryAlerts  = 2 * time.Minute
	TTLExchangeRate     = 10 * time.Minute
	TTLAIMenuSuggestion = 10 * time.Minute
	TTLSession          = 168 * time.Hour
	TTLRevokedToken     = 168 * time.Hour
	TTLLock             = 10 * time.Second
)

// Cache is the public interface consumed by services.
// All methods accept a context for cancellation propagation.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, key string) (bool, error)
	Increment(ctx context.Context, key string) (int64, error)
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Publish(ctx context.Context, channel, message string) error
	Ping(ctx context.Context) error
	Close() error
}

type redisCache struct {
	client *redis.Client
	log    *zap.Logger
}

// New creates a connected Redis client and verifies connectivity.
func New(ctx context.Context, cfg *config.Config, log *zap.Logger) (Cache, error) {
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		opt = &redis.Options{
			Addr:     cfg.Redis.URL,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		}
	}

	opt.DialTimeout = cfg.Redis.DialTimeout
	opt.ReadTimeout = cfg.Redis.ReadTimeout
	opt.WriteTimeout = cfg.Redis.WriteTimeout
	opt.PoolSize = cfg.Redis.PoolSize
	opt.MinIdleConns = cfg.Redis.MinIdleConns

	client := redis.NewClient(opt)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("cache: redis ping failed: %w", err)
	}

	log.Info("cache: redis connected", zap.String("addr", opt.Addr))
	return &redisCache{client: client, log: log}, nil
}

func (c *redisCache) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

func (c *redisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *redisCache) Delete(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

func (c *redisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, key).Result()
	return n > 0, err
}

// Increment is used for rate limiting counters.
func (c *redisCache) Increment(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

// SetNX is the building block for distributed locks.
func (c *redisCache) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, value, ttl).Result()
}

func (c *redisCache) Publish(ctx context.Context, channel, message string) error {
	return c.client.Publish(ctx, channel, message).Err()
}

func (c *redisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *redisCache) Close() error {
	return c.client.Close()
}

// Key builders used by services and handlers.
func KeyDashboardStats() string          { return "dashboard:stats" }
func KeyRoomList(status string) string   { return fmt.Sprintf("rooms:list:%s", status) }
func KeyRoomByID(id string) string       { return fmt.Sprintf("rooms:%s", id) }
func KeyMenuItems() string               { return "menu:items:all" }
func KeyInventoryAlerts() string         { return "inventory:alerts" }
func KeyExchangeRate(b, t string) string { return fmt.Sprintf("fx:%s:%s", b, t) }
func KeyAIMenu(h string) string          { return fmt.Sprintf("ai:menu:%s", h) }
func KeyRevokedToken(t string) string {
	if len(t) > 32 {
		return "revoked:" + t[len(t)-32:]
	}
	return "revoked:" + t
}

// NoopCache satisfies the Cache interface without any network I/O.
// Use in unit tests to avoid a Redis dependency.
type NoopCache struct{}

func (NoopCache) Get(_ context.Context, _ string) (string, error)           { return "", redis.Nil }
func (NoopCache) Set(_ context.Context, _, _ string, _ time.Duration) error { return nil }
func (NoopCache) Delete(_ context.Context, _ ...string) error               { return nil }
func (NoopCache) Exists(_ context.Context, _ string) (bool, error)          { return false, nil }
func (NoopCache) Increment(_ context.Context, _ string) (int64, error)      { return 0, nil }
func (NoopCache) SetNX(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
	return true, nil
}
func (NoopCache) Publish(_ context.Context, _, _ string) error { return nil }
func (NoopCache) Ping(_ context.Context) error                 { return nil }
func (NoopCache) Close() error                                 { return nil }
