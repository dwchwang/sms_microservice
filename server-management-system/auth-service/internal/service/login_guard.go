package service

import (
	"context"
	"fmt"
	"time"
)

// LoginGuard provides protection against brute-force login attempts.
type LoginGuard struct {
	redis redisCommander
}

// NewLoginGuard creates a new LoginGuard.
func NewLoginGuard(rdb redisCommander) *LoginGuard {
	return &LoginGuard{
		redis: rdb,
	}
}

// CheckAttempts checks if the user has exceeded the maximum failed login attempts.
func (g *LoginGuard) CheckAttempts(ctx context.Context, email string) error {
	if g.redis == nil {
		return nil // Pass through if Redis is unavailable
	}
	key := fmt.Sprintf("auth:login_attempts:%s", email)
	count, err := g.redis.Get(ctx, key).Int()
	if err != nil {
		return nil // Key doesn't exist yet, no attempts
	}
	if count >= 5 {
		return ErrTooManyAttempts
	}
	return nil
}

// RecordFailedAttempt increments the failed login counter for an email.
func (g *LoginGuard) RecordFailedAttempt(ctx context.Context, email string) {
	if g.redis == nil {
		return
	}
	key := fmt.Sprintf("auth:login_attempts:%s", email)
	g.redis.Incr(ctx, key)
	g.redis.Expire(ctx, key, 15*time.Minute)
}

// ResetAttempts clears the failed login counter for an email.
func (g *LoginGuard) ResetAttempts(ctx context.Context, email string) {
	if g.redis == nil {
		return
	}
	key := fmt.Sprintf("auth:login_attempts:%s", email)
	g.redis.Del(ctx, key)
}
