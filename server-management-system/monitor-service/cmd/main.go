package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vcs-sms/monitor-service/config"
	"github.com/vcs-sms/monitor-service/internal/database"
	"github.com/vcs-sms/monitor-service/internal/monitor"
	"github.com/vcs-sms/monitor-service/internal/repository"
	"github.com/vcs-sms/shared/logger"
)

func main() {
	cfg := config.LoadConfig()

	log := logger.NewLogger(cfg.App.Name, &logger.LogConfig{
		Level:      cfg.Log.Level,
		Dir:        cfg.Log.Dir,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,
		Compress:   cfg.Log.Compress,
	})

	if err := cfg.Monitor.Validate(); err != nil {
		log.Fatal().Err(err).Msg("Invalid monitor configuration")
	}

	rdb := database.ConnectRedis(cfg.Redis, cfg.Monitor.WorkerCount)

	esClient, err := database.ConnectES(database.ESConfig{
		Addresses:   cfg.ES.Addresses,
		Username:    cfg.ES.Username,
		Password:    cfg.ES.Password,
		IndexPrefix: cfg.ES.IndexPrefix,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Elasticsearch")
	}

	templateCtx, cancelTemplate := context.WithTimeout(context.Background(), 10*time.Second)
	if err := database.EnsureIndexTemplate(templateCtx, esClient, cfg.ES.IndexPrefix); err != nil {
		log.Fatal().Err(err).Msg("Failed to install the health-fact index template")
	}
	cancelTemplate()

	registry := prometheus.NewRegistry()
	metrics := monitor.NewMetrics(registry)

	ops := monitor.NewRedisOps(rdb)
	writer := repository.NewFactWriter(esClient, cfg.ES.IndexPrefix)
	facts := monitor.NewFactBuffer(writer, cfg.Monitor.FactCapacity, metrics, log)
	pinger := monitor.NewTCPPinger(
		time.Duration(cfg.Monitor.TCPTimeout)*time.Millisecond, cfg.Monitor.TCPDialHost)

	scheduler := monitor.NewScheduler(ops, metrics, log)
	pool := monitor.NewPool(ops, pinger, facts, metrics, cfg.Monitor.WorkerCount, log)
	sampler := monitor.NewSampler(ops, metrics)

	// The scheduler competes for the round lock; the pool pings whether or not
	// this instance wins, so every instance adds ping capacity.
	runCtx, stopRun := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); scheduler.Run(runCtx) }()
	go func() { defer wg.Done(); pool.Run(runCtx) }()
	go func() { defer wg.Done(); facts.Run(runCtx) }()
	go func() { defer wg.Done(); sampler.Run(runCtx) }()

	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.HandlerFor(registry, promhttp.HandlerOpts{})))

	addr := fmt.Sprintf(":%s", cfg.App.Port)
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Info().Str("addr", addr).Int("workers", cfg.Monitor.WorkerCount).
			Msg("Starting monitor service")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start health endpoint")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("Shutting down monitor service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Health endpoint forced to shutdown")
	}

	stopRun()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		log.Warn().Msg("Monitor loops did not stop in time")
	}

	log.Info().Msg("Monitor service exited")
}
