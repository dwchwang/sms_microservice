package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/auth-service/config"
	"github.com/vcs-sms/auth-service/internal/database"
	"github.com/vcs-sms/auth-service/internal/handler"
	"github.com/vcs-sms/auth-service/internal/repository"
	"github.com/vcs-sms/auth-service/internal/service"
	"github.com/vcs-sms/shared/logger"
	"github.com/vcs-sms/shared/middleware"
)

func main() {
	// 1. Load config
	cfg := config.LoadConfig()

	// Validate critical secrets
	if len(cfg.JWT.Secret) < 32 {
		fmt.Fprintln(os.Stderr, "FATAL: JWT_SECRET must be at least 32 characters")
		os.Exit(1)
	}

	// 2. Init logger
	log := logger.NewLogger(cfg.App.Name, &logger.LogConfig{
		Level:      cfg.Log.Level,
		Dir:        cfg.Log.Dir,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,
		Compress:   cfg.Log.Compress,
	})

	// 3. Connect DB (GORM)
	db := database.Connect(cfg.Database)

	// 4. Connect Redis
	rdb := database.ConnectRedis(cfg.Redis)

	// 5. Init layers
	userRepo := repository.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, rdb, cfg.JWT)
	authHandler := handler.NewAuthHandler(authSvc, cfg.JWT.Secret)

	// 6. Setup Gin router
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(middleware.LoggerMiddleware(log))

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// ForwardAuth endpoint
	verifyHandler := handler.NewVerifyHandler(cfg.JWT.Secret)
	r.GET("/internal/verify", verifyHandler.Verify)

	// Auth routes
	auth := r.Group("/api/v1/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
		auth.POST("/refresh", authHandler.RefreshToken)
		auth.POST("/logout", authHandler.Logout)
		auth.GET("/profile", authHandler.GetProfile)
		auth.GET("/users", authHandler.ListUsers)
		auth.PUT("/users/:user_id/role", authHandler.UpdateUserRole)
	}

	// 7. Start server with graceful shutdown
	addr := fmt.Sprintf(":%s", cfg.App.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("Starting auth service")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start server")
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("Shutting down auth service...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}
	log.Info().Msg("Server exited")
}
