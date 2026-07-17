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
	"github.com/vcs-sms/report-service/config"
	"github.com/vcs-sms/report-service/internal/client"
	"github.com/vcs-sms/report-service/internal/database"
	"github.com/vcs-sms/report-service/internal/email"
	"github.com/vcs-sms/report-service/internal/handler"
	"github.com/vcs-sms/report-service/internal/repository"
	"github.com/vcs-sms/report-service/internal/scheduler"
	"github.com/vcs-sms/report-service/internal/service"
	"github.com/vcs-sms/report-service/internal/snapshot"
	"github.com/vcs-sms/shared/logger"
	"github.com/vcs-sms/shared/middleware"
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

	loc, err := time.LoadLocation(config.ReportTimezone)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load the report timezone")
	}

	db := database.Connect(cfg.Database)

	esClient, err := database.ConnectES(cfg.ES)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Elasticsearch")
	}

	snapshotRepo := repository.NewSnapshotRepository(db)
	jobRepo := repository.NewJobRepository(db)
	aggregator := repository.NewUptimeAggregator(esClient, cfg.ES.IndexPrefix)
	serverClient := client.NewServerClient(cfg.Server.BaseURL, cfg.Server.Timeout)

	renderer, err := email.NewRenderer()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to compile the report template")
	}
	sender := email.NewGmailSender(
		cfg.SMTP.Host, cfg.SMTP.Port, cfg.SMTP.Username,
		cfg.SMTP.Password, cfg.SMTP.From, cfg.SMTP.RecipientDomains,
	)

	reportSvc := service.NewReportService(
		snapshotRepo, loc, cfg.Report.MaxRangeDays, cfg.Report.CoverageThresholdPct)
	sendSvc := service.NewSendService(reportSvc, jobRepo, sender, renderer, loc, log)
	snapshotJob := snapshot.NewJob(serverClient, aggregator, snapshotRepo, loc, log)
	reportHandler := handler.NewReportHandler(reportSvc, sendSvc)

	cron := scheduler.NewScheduler(snapshotJob, sendSvc, cfg.Report.DailyRecipient, loc, log)
	if err := cron.Register(cfg.Report.SnapshotCron, cfg.Report.DailyCron); err != nil {
		log.Fatal().Err(err).Msg("Failed to register scheduled jobs")
	}
	cron.Start()

	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestIDMiddleware())
	r.Use(middleware.LoggerMiddleware(log))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Traefik ForwardAuth proves the JWT; scopes are enforced here (design §9.10).
	reports := r.Group("/api/v1/reports", middleware.AuthFromForwardAuth())
	{
		reports.GET("/summary", middleware.RequireScope("report:view"), reportHandler.GetSummary)
		reports.POST("", middleware.RequireScope("report:send"), reportHandler.SendReport)
		reports.GET("/:id", middleware.RequireScope("report:view_detail"), reportHandler.GetReport)
	}

	// Lets an operator re-run a snapshot whose 00:30 job failed, without
	// waiting a day. Internal only: not published through Traefik.
	r.POST("/internal/snapshots/:date", func(c *gin.Context) {
		date, err := time.ParseInLocation(time.DateOnly, c.Param("date"), loc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date must be YYYY-MM-DD"})
			return
		}
		result, err := snapshotJob.Run(c.Request.Context(), date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	})

	addr := fmt.Sprintf(":%s", cfg.App.Port)
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Info().Str("addr", addr).Msg("Starting report service")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start report service")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("Shutting down report service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Report service forced to shutdown")
	}
	cron.Stop()

	log.Info().Msg("Report service exited")
}
