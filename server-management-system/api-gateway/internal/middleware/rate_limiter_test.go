package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type fakeRateLimitStore struct {
	count       int64
	err         error
	expireCalls int
}

func (s *fakeRateLimitStore) Incr(ctx context.Context, key string) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	s.count++
	return s.count, nil
}

func (s *fakeRateLimitStore) Expire(ctx context.Context, key string, expiration time.Duration) error {
	s.expireCalls++
	return nil
}

func TestRateLimiterMiddleware_FailsClosedWhenRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer rdb.Close()

	r := gin.New()
	r.Use(RateLimiterMiddleware(rdb, 10, time.Minute))
	r.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestRateLimiterMiddleware_AllowsWithinLimitAndSetsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &fakeRateLimitStore{}
	r := gin.New()
	r.Use(rateLimiterMiddleware(store, 2, time.Minute))
	r.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if w.Header().Get("X-RateLimit-Limit") != "2" || w.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Fatalf("unexpected rate headers: limit=%s remaining=%s", w.Header().Get("X-RateLimit-Limit"), w.Header().Get("X-RateLimit-Remaining"))
	}
	if store.expireCalls != 1 {
		t.Fatalf("expected expire on first request, got %d", store.expireCalls)
	}
}

func TestRateLimiterMiddleware_RejectsOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &fakeRateLimitStore{count: 2}
	r := gin.New()
	r.Use(rateLimiterMiddleware(store, 2, time.Minute))
	r.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("expected 0 remaining, got %s", w.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestRateLimiterMiddleware_StoreError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &fakeRateLimitStore{err: errors.New("redis unavailable")}
	r := gin.New()
	r.Use(rateLimiterMiddleware(store, 2, time.Minute))
	r.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
