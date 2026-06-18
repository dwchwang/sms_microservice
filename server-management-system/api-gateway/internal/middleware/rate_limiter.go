package middleware

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/shared/response"
)

type rateLimitStore interface {
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, expiration time.Duration) error
}

type redisRateLimitStore struct {
	client *redis.Client
}

func (s *redisRateLimitStore) Incr(ctx context.Context, key string) (int64, error) {
	return s.client.Incr(ctx, key).Result()
}

func (s *redisRateLimitStore) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return s.client.Expire(ctx, key, expiration).Err()
}

// RateLimiterMiddleware implements a sliding window rate limiter using Redis.
func RateLimiterMiddleware(redisClient *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return rateLimiterMiddleware(&redisRateLimitStore{client: redisClient}, limit, window)
}

func rateLimiterMiddleware(store rateLimitStore, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		key := "rate:limit:" + ip

		// Redis INCR + EXPIRE pattern
		count, err := store.Incr(c.Request.Context(), key)
		if err != nil {
			// Redis unavailable — fail closed to prevent unbounded requests
			response.Error(c, 503, "Service temporarily unavailable")
			c.Abort()
			return
		}

		if count == 1 {
			_ = store.Expire(c.Request.Context(), key, window)
		}

		remaining := limit - int(count)
		if remaining < 0 {
			remaining = 0
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(window).Unix(), 10))

		if count > int64(limit) {
			response.Error(c, 429, "Rate limit exceeded. Please try again later.")
			c.Abort()
			return
		}

		c.Next()
	}
}
