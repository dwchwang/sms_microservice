package router

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/api-gateway/config"
)

func TestSetupRouter_AuthUserManagementRoutesAreExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		JWTSecret:          "my-32-byte-secret-key-for-testing!",
		RateLimit:          100,
		RateLimitWindow:    time.Minute,
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		AuthServiceURL:     "http://auth-service:8081",
		ServerServiceURL:   "http://server-service:8082",
		MonitorServiceURL:  "http://monitor-service:8083",
		ReportServiceURL:   "http://report-service:8084",
		FileIOServiceURL:   "http://fileio-service:8085",
	}

	r := SetupRouter(cfg, redis.NewClient(&redis.Options{Addr: "localhost:6379"}))

	routes := map[string]bool{}
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
		if route.Path == "/api/v1/auth/*path" {
			t.Fatalf("auth wildcard route must not be public because /auth/users requires user:manage")
		}
	}

	for _, expected := range []string{
		"POST /api/v1/auth/register",
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/refresh",
		"GET /api/v1/auth/users",
		"PUT /api/v1/auth/users/:user_id/role",
	} {
		if !routes[expected] {
			t.Fatalf("expected route %s to be registered", expected)
		}
	}
}
