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
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/config"
	"github.com/vcs-sms/server-service/internal/consumer"
	"github.com/vcs-sms/server-service/internal/database"
	"github.com/vcs-sms/server-service/internal/excel"
	"github.com/vcs-sms/server-service/internal/handler"
	"github.com/vcs-sms/server-service/internal/projection"
	"github.com/vcs-sms/server-service/internal/repository"
	"github.com/vcs-sms/server-service/internal/service"
	"github.com/vcs-sms/server-service/internal/status"
	"github.com/vcs-sms/server-service/internal/validator"
	"github.com/vcs-sms/shared/logger"
	"github.com/vcs-sms/shared/middleware"
)

// runRebuild repopulates the monitor target projection and exits.
func runRebuild(targets projection.TargetProjection, repo repository.ServerRepository, log zerolog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	written, err := targets.Rebuild(ctx, projection.NewRepoSource(repo))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to rebuild monitor target projection")
	}
	log.Info().Int("targets", written).Dur("took", time.Since(start)).
		Msg("Monitor target projection rebuilt")
}

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
	cidr, err := validator.NewCIDRValidator(cfg.Security.CIDRAllowlist)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid SERVER_CIDR_ALLOWLIST")
	}
	if cfg.Security.CIDRAllowlist == "" {
		log.Warn().Msg("SERVER_CIDR_ALLOWLIST is empty — every ipv4 will be rejected")
	}

	serverRepo := repository.NewServerRepository(db)
	targets := projection.NewTargetProjection(rdb)

	if len(os.Args) > 1 && os.Args[1] == "rebuild-monitor-cache" {
		runRebuild(targets, serverRepo, log)
		return
	}

	lastCheck := status.NewLastCheckReader(rdb)
	serverSvc := service.NewServerService(serverRepo, rdb, cidr, targets, lastCheck, log)
	serverHandler := handler.NewServerHandler(serverSvc)

	importSvc := service.NewImportService(
		serverRepo, excel.NewParser(), cidr, targets, rdb, log)
	importHandler := handler.NewImportHandler(importSvc)

	exportSvc := service.NewExportService(serverRepo, excel.NewGenerator(), lastCheck)
	exportHandler := handler.NewExportHandler(exportSvc)

	internalHandler := handler.NewInternalHandler(serverRepo)
	idempotency := handler.Idempotency(repository.NewIdempotencyRepository(db), log)

	// 6. Start the status stream consumer
	consumerCtx, stopConsumer := context.WithCancel(context.Background())
	defer stopConsumer()

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to resolve hostname for consumer name")
	}
	statusConsumer := consumer.NewStatusConsumer(rdb, serverRepo, hostname, log)

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		statusConsumer.Run(consumerCtx)
	}()

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
		// Idempotency guards the two mutations that create rows.
		servers.POST("", idempotency, serverHandler.CreateServer)
		servers.POST("/import", idempotency, importHandler.ImportServers)

		servers.GET("", serverHandler.ListServers)
		servers.GET("/stats", serverHandler.GetStats)
		servers.POST("/export", exportHandler.ExportServers)
		servers.GET("/:server_id", serverHandler.GetServer)
		servers.PUT("/:server_id", serverHandler.UpdateServer)
		servers.DELETE("/:server_id", serverHandler.DeleteServer)
	}

	// Not published through Traefik; reachable only on the internal network.
	r.GET("/internal/servers", internalHandler.ListPopulation)

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

	stopConsumer()
	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		log.Warn().Msg("Status consumer did not stop in time")
	}

	log.Info().Msg("Server exited")
}
