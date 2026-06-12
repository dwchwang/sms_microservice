package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	sharedjwt "github.com/vcs-sms/shared/pkg/jwt"
)

func TestJWTAuthMiddleware_RejectsMissingAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/protected", JWTAuthMiddleware("test-secret", nil), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuthMiddleware_RejectsInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/protected", JWTAuthMiddleware("test-secret", nil), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not.a.jwt")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuthMiddleware_ForwardsClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "my-32-byte-secret-key-for-testing!"
	token, _, err := sharedjwt.GenerateAccessToken(
		sharedjwt.TokenConfig{Secret: secret, AccessTokenDuration: time.Minute, RefreshTokenDuration: time.Hour},
		"550e8400-e29b-41d4-a716-446655440000",
		"alice",
		"admin",
		[]string{"server:read"},
	)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer rdb.Close()

	r := gin.New()
	r.GET("/protected", JWTAuthMiddleware(secret, rdb), func(c *gin.Context) {
		if c.GetHeader("X-User-ID") != "550e8400-e29b-41d4-a716-446655440000" {
			t.Fatalf("missing forwarded user id")
		}
		if c.GetHeader("X-Scopes") != "server:read" {
			t.Fatalf("missing forwarded scopes")
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestScopeMiddleware_AllowsRequiredScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("scopes", []string{"server:read", "server:create"})
	})
	r.GET("/servers", ScopeMiddleware("server:read"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/servers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestScopeMiddleware_RejectsMissingScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/servers", ScopeMiddleware("server:read"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/servers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestScopeMiddleware_RejectsInvalidScopeFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("scopes", "server:read")
	})
	r.GET("/servers", ScopeMiddleware("server:read"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/servers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
