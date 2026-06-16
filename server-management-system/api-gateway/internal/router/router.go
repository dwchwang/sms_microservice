package router

import (
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/api-gateway/config"
	"github.com/vcs-sms/api-gateway/internal/middleware"
	"github.com/vcs-sms/api-gateway/internal/proxy"
	"github.com/vcs-sms/api-gateway/internal/swagger"
	sharedmw "github.com/vcs-sms/shared/middleware"
)

// SetupRouter configures all routes and middleware for the API Gateway.
func SetupRouter(cfg *config.Config, redisClient *redis.Client) *gin.Engine {
	r := gin.New()

	// ── Global middleware ──
	r.Use(gin.Recovery())
	r.Use(sharedmw.RequestIDMiddleware())
	r.Use(middleware.CORSMiddleware(cfg.CORSAllowedOrigins))
	r.Use(middleware.RateLimiterMiddleware(redisClient, cfg.RateLimit, cfg.RateLimitWindow))

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	swagger.RegisterRoutes(r, "")

	// ── Public routes (no auth required) ──
	authProxy := proxy.NewReverseProxy(cfg.AuthServiceURL)
	public := r.Group("/api/v1")
	{
		// Only these auth endpoints are public.
		public.POST("/auth/register", authProxy)
		public.POST("/auth/login", authProxy)
		public.POST("/auth/refresh", authProxy)
	}

	// ── Protected routes (JWT required) ──
	protected := r.Group("/api/v1")
	protected.Use(middleware.JWTAuthMiddleware(cfg.JWTSecret, redisClient))
	{
		// Server CRUD
		servers := protected.Group("/servers")
		{
			servers.POST("", middleware.ScopeMiddleware("server:create"),
				proxy.NewReverseProxy(cfg.ServerServiceURL))
			servers.GET("", middleware.ScopeMiddleware("server:read"),
				proxy.NewReverseProxy(cfg.ServerServiceURL))
			servers.GET("/:server_id", middleware.ScopeMiddleware("server:read"),
				proxy.NewReverseProxy(cfg.ServerServiceURL))
			servers.PUT("/:server_id", middleware.ScopeMiddleware("server:update"),
				proxy.NewReverseProxy(cfg.ServerServiceURL))
			servers.DELETE("/:server_id", middleware.ScopeMiddleware("server:delete"),
				proxy.NewReverseProxy(cfg.ServerServiceURL))

			// Import/Export → FileIO Service (Phase 4)
			servers.POST("/import", middleware.ScopeMiddleware("server:import"),
				proxy.NewReverseProxy(cfg.FileIOServiceURL))
			servers.GET("/import/:job_id", middleware.ScopeMiddleware("server:import"),
				proxy.NewReverseProxy(cfg.FileIOServiceURL))
			servers.POST("/export", middleware.ScopeMiddleware("server:export"),
				proxy.NewReverseProxy(cfg.FileIOServiceURL))
		}

		// Reports → Report Service (Phase 3)
		reports := protected.Group("/reports")
		{
			reports.GET("/summary", middleware.ScopeMiddleware("report:view"),
				proxy.NewReverseProxy(cfg.ReportServiceURL))
			reports.POST("", middleware.ScopeMiddleware("report:send"),
				proxy.NewReverseProxy(cfg.ReportServiceURL))
		}

		// Monitor endpoints (Phase 2) — authenticated only (per architecture.md §4.1).
		monitor := protected.Group("/monitor")
		{
			monitor.GET("/status", proxy.NewReverseProxy(cfg.MonitorServiceURL))
		}

		// Auth endpoints that require a valid JWT.
		auth := protected.Group("/auth")
		{
			auth.POST("/logout", authProxy)
			auth.GET("/profile", authProxy)
			auth.GET("/users", middleware.ScopeMiddleware("user:manage"), authProxy)
			auth.PUT("/users/:user_id/role", middleware.ScopeMiddleware("user:manage"), authProxy)
		}
	}

	return r
}
