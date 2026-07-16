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
	"github.com/vcs-sms/server-service/config"
	"github.com/vcs-sms/server-service/internal/database"
	"github.com/vcs-sms/server-service/internal/handler"
	"github.com/vcs-sms/server-service/internal/repository"
	"github.com/vcs-sms/server-service/internal/service"
	"github.com/vcs-sms/shared/logger"
	"github.com/vcs-sms/shared/middleware"
)

func main() {
	// 1. Load config
	cfg := config.LoadConfig()

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
	serverRepo := repository.NewServerRepository(db)
	serverSvc := service.NewServerService(serverRepo, rdb)
	serverHandler := handler.NewServerHandler(serverSvc)

	// 7. Setup Gin router
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

	// Server routes (JWT auth is handled by API Gateway)
	servers := r.Group("/api/v1/servers")
	{
		servers.POST("", serverHandler.CreateServer)
		servers.GET("", serverHandler.ListServers)
		servers.GET("/:server_id", serverHandler.GetServer)
		servers.PUT("/:server_id", serverHandler.UpdateServer)
		servers.DELETE("/:server_id", serverHandler.DeleteServer)
	}

	// 8. Start server with graceful shutdown
	addr := fmt.Sprintf(":%s", cfg.App.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("Starting server service")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start server")
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("Shutting down server service...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}
	log.Info().Msg("Server exited")
}
