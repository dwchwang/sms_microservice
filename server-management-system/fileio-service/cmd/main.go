package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/vcs-sms/fileio-service/config"
	"github.com/vcs-sms/fileio-service/internal/database"
	"github.com/vcs-sms/fileio-service/internal/excel"
	"github.com/vcs-sms/fileio-service/internal/handler"
	"github.com/vcs-sms/fileio-service/internal/repository"
	"github.com/vcs-sms/fileio-service/internal/service"
	"github.com/vcs-sms/shared/kafka"
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

	log.Info().Msg("Starting fileio-service...")

	// 3. Connect PostgreSQL — fileio_schema (own schema)
	fileioDB := database.Connect(cfg.FileIODB)

	// 4. Connect PostgreSQL — server_schema (cross-schema access)
	serverDB := database.Connect(cfg.ServerDB)

	// 5. Connect Redis (optional — degraded mode if unavailable)
	rdb := database.ConnectRedis(cfg.Redis)

	// 6. Init repositories
	importJobRepo := repository.NewImportJobRepo(fileioDB)
	serverWriter := repository.NewServerWriter(serverDB)

	// 7. Init Excel components
	parser := excel.NewExcelParser()
	generator := excel.NewExcelGenerator()

	// 8. Init Kafka producer
	brokers := strings.Split(cfg.Kafka.Brokers, ",")
	producer := kafka.NewSegmentioProducer(
		kafka.DefaultSegmentioProducerConfig(brokers),
		log,
	)
	defer producer.Close()

	// 9. Init services
	importSvc := service.NewImportService(
		importJobRepo,
		serverWriter,
		parser,
		producer,
		rdb,
		cfg,
		log,
	)
	exportSvc := service.NewExportService(serverWriter, generator, log)

	// 10. Init handlers
	importHandler := handler.NewImportHandler(importSvc)
	exportHandler := handler.NewExportHandler(exportSvc)

	// 11. Init Kafka consumer for async import processing
	consumer := kafka.NewSegmentioConsumer(
		kafka.DefaultSegmentioConsumerConfig(brokers, "fileio-group"),
		log,
	)
	defer consumer.Close()

	consumer.Subscribe("import.job.created", "fileio-group", func(ctx context.Context, event *kafka.Event) error {
		// Extract job_id from event data
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			// Try JSON unmarshal if data comes as raw JSON
			var rawData map[string]interface{}
			if jsonData, err := json.Marshal(event.Data); err == nil {
				if json.Unmarshal(jsonData, &rawData) == nil {
					data = rawData
				}
			}
		}

		if data == nil {
			log.Error().Msg("import.job.created event has no data")
			return nil // don't retry — invalid event
		}

		jobID, ok := data["job_id"].(string)
		if !ok {
			log.Error().Msg("import.job.created event missing job_id")
			return nil
		}

		log.Info().Str("job_id", jobID).Msg("Received import job from Kafka")

		// Process the import job
		if err := importSvc.ProcessImportJob(ctx, jobID); err != nil {
			log.Error().Err(err).Str("job_id", jobID).Msg("Failed to process import job")
			return err
		}

		return nil
	})

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Kafka consumer in background
	var consumerWg sync.WaitGroup
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		log.Info().Msg("Starting Kafka consumer for import.job.created...")
		if err := consumer.Start(ctx); err != nil {
			log.Error().Err(err).Msg("Kafka consumer stopped with error")
		}
	}()

	// 12. Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestIDMiddleware())

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": cfg.App.Name,
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// API routes
	api := router.Group("/api/v1")
	{
		api.POST("/servers/import", importHandler.ImportServers)
		api.GET("/servers/import/:job_id", importHandler.GetImportStatus)
		api.POST("/servers/export", exportHandler.ExportServers)
	}

	// 13. Start HTTP server
	addr := fmt.Sprintf(":%s", cfg.App.Port)
	log.Info().Str("addr", addr).Msg("File I/O service started")

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second, // longer for file uploads
		WriteTimeout: 60 * time.Second, // longer for file downloads
		IdleTimeout:  120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// 14. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to start HTTP server")
		}
	case <-quit:
		log.Info().Msg("Shutting down fileio-service...")

		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("HTTP server forced to shutdown")
		}

		consumerWg.Wait()
	}
}

// Ensure imports are used
var _ zerolog.Logger
