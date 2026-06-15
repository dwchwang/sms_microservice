package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/gin-gonic/gin"

	"github.com/vcs-sms/monitor-service/config"
	"github.com/vcs-sms/monitor-service/internal/checker"
	"github.com/vcs-sms/monitor-service/internal/database"
	"github.com/vcs-sms/monitor-service/internal/repository"
	"github.com/vcs-sms/monitor-service/internal/scheduler"
	"github.com/vcs-sms/monitor-service/internal/service"
	"github.com/vcs-sms/monitor-service/internal/worker"
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

	log.Info().Msg("Starting monitor-service...")

	if err := cfg.Monitor.Validate(); err != nil {
		log.Fatal().Err(err).Msg("Invalid monitor configuration")
	}

	// 3. Connect DBs (2 connections: monitor_schema + server_schema)
	monitorDB := database.Connect(cfg.MonitorDB)
	serverDB := database.Connect(cfg.ServerDB)

	// 4. Connect Redis
	rdb := database.ConnectRedis(cfg.Redis)
	var redisClient scheduler.RedisClient
	if rdb != nil {
		redisClient = scheduler.NewRealRedisClient(rdb)
	}

	// 5. Connect Elasticsearch
	esCfg := elasticsearch.Config{
		Addresses: strings.Split(cfg.ES.Addresses, ","),
	}
	if cfg.ES.Username != "" {
		esCfg.Username = cfg.ES.Username
		esCfg.Password = cfg.ES.Password
	}
	esClient, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Elasticsearch client")
	}
	log.Info().Msg("Elasticsearch client created")

	// ── CRITICAL: Ensure ES index has correct mapping ──
	// The index MUST be created with "keyword" mapping for server_id, server_name,
	// status fields BEFORE any documents are bulk-indexed. Otherwise, dynamic mapping
	// will map them as "text" which breaks terms aggregation in report-service.
	if err := database.EnsureIndexMapping(context.Background(), esClient, cfg.ES.IndexName); err != nil {
		log.Fatal().Err(err).Msg("Failed to ensure Elasticsearch index mapping")
	}

	// 6. Connect Kafka
	brokers := strings.Split(cfg.Kafka.Brokers, ",")

	producer := kafka.NewSegmentioProducer(
		kafka.DefaultSegmentioProducerConfig(brokers),
		log,
	)
	defer producer.Close()

	consumer := kafka.NewSegmentioConsumer(
		kafka.DefaultSegmentioConsumerConfig(brokers, "monitor-group"),
		log,
	)
	defer consumer.Close()

	// 7. Init repos
	configRepo := repository.NewConfigRepo(monitorDB)
	serverReader := repository.NewServerReader(serverDB)
	esRepo := repository.NewESStatusLogRepo(esClient, cfg.ES.IndexName)

	// 8. Init checker (TCP only — no factory needed)
	healthChecker := checker.NewTCPChecker(
		time.Duration(cfg.Monitor.TCPTimeout) * time.Millisecond,
	)

	// 9. Init worker pool
	pool := worker.NewPool(cfg.Monitor.WorkerCount, healthChecker, log)

	// 10. Init scheduler
	checkScheduler := scheduler.NewHealthCheckScheduler(
		pool,
		serverReader,
		configRepo,
		esRepo,
		redisClient,
		producer,
		log,
		time.Duration(cfg.Monitor.CheckInterval)*time.Second,
	)

	// 11. Init event consumer
	eventConsumer := service.NewEventConsumer(configRepo, cfg.Monitor, log)
	if err := consumer.Subscribe("server.created", "monitor-group", eventConsumer.HandleServerCreated); err != nil {
		log.Fatal().Err(err).Msg("Failed to subscribe to server.created")
	}
	if err := consumer.Subscribe("server.deleted", "monitor-group", eventConsumer.HandleServerDeleted); err != nil {
		log.Fatal().Err(err).Msg("Failed to subscribe to server.deleted")
	}

	// 12. Create root context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 13. Start scheduler in goroutine
	go checkScheduler.Start(ctx)

	// 14. Start Kafka consumer in goroutine (track with WaitGroup)
	var consumerWg sync.WaitGroup
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		if err := consumer.Start(ctx); err != nil && err != context.Canceled {
			log.Error().Err(err).Msg("Kafka consumer stopped with error")
		}
	}()

	// 15. HTTP server (health endpoint)
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(middleware.LoggerMiddleware(log))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "monitor-service",
		})
	})
	r.GET("/api/v1/monitor/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":          "ok",
			"service":         "monitor-service",
			"check_interval":  cfg.Monitor.CheckInterval,
			"worker_count":    cfg.Monitor.WorkerCount,
			"tcp_timeout_ms":  cfg.Monitor.TCPTimeout,
			"elasticsearch":   cfg.ES.Addresses,
			"index":           cfg.ES.IndexName,
			"redis_available": rdb != nil,
		})
	})

	addr := fmt.Sprintf(":%s", cfg.App.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("HTTP server started")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	// 16. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down monitor-service...")

	// 1. Cancel root context — stops scheduler and consumer loops
	cancel()

	// 2. Wait for consumer goroutine to cleanly exit
	consumerWg.Wait()

	// 3. Close consumer (closes all Kafka readers)
	if err := consumer.Close(); err != nil {
		log.Error().Err(err).Msg("Consumer close error")
	}

	// 4. Close Kafka producer
	if err := producer.Close(); err != nil {
		log.Error().Err(err).Msg("Producer close error")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	log.Info().Msg("Monitor-service stopped")
}
