package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/config"
	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/email"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
)

// ReportService defines the business logic interface for reports.
type ReportService interface {
	// GetSummary retrieves uptime summary for a date range (no email sent).
	GetSummary(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error)

	// SendReport creates and sends a report email on demand.
	SendReport(ctx context.Context, req *dto.SendReportRequest) (*dto.SendReportResponse, error)

	// SendDailyReport sends the daily report for yesterday to the admin email.
	SendDailyReport(ctx context.Context) error
}

type reportService struct {
	esUptimeRepo   repository.UptimeCalculator
	serverCounter  repository.ServerCounter
	reportJobRepo  repository.ReportJobRepo
	snapshotRepo   repository.DailySnapshotRepo
	emailSender    email.EmailSender
	cache          summaryCache
	smtpAdminEmail string
	logger         zerolog.Logger
	clock          func() time.Time
}

type summaryCache interface {
	Get(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error)
	Set(ctx context.Context, startDate, endDate time.Time, summary *dto.ReportSummaryResponse) error
}

type redisSummaryCache struct {
	get func(ctx context.Context, key string) (string, error)
	set func(ctx context.Context, key string, value interface{}, ttl time.Duration) error
}

// NewReportService creates a new ReportService.
func NewReportService(
	esUptimeRepo repository.UptimeCalculator,
	serverCounter repository.ServerCounter,
	reportJobRepo repository.ReportJobRepo,
	snapshotRepo repository.DailySnapshotRepo,
	emailSender email.EmailSender,
	redisClient *redis.Client,
	cfg config.SMTPConfig,
	logger zerolog.Logger,
) ReportService {
	var cache summaryCache
	if redisClient != nil {
		cache = &redisSummaryCache{
			get: func(ctx context.Context, key string) (string, error) {
				return redisClient.Get(ctx, key).Result()
			},
			set: func(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
				return redisClient.Set(ctx, key, value, ttl).Err()
			},
		}
	}

	return &reportService{
		esUptimeRepo:   esUptimeRepo,
		serverCounter:  serverCounter,
		reportJobRepo:  reportJobRepo,
		snapshotRepo:   snapshotRepo,
		emailSender:    emailSender,
		cache:          cache,
		smtpAdminEmail: cfg.AdminEmail,
		logger:         logger,
		clock:          time.Now,
	}
}

// GetSummary retrieves uptime summary with Redis cache-aside.
func (s *reportService) GetSummary(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
	useCache := shouldUseSummaryCache(startDate, endDate, s.now())

	// 1. Check Redis cache
	if useCache {
		cached, err := s.getCachedSummary(ctx, startDate, endDate)
		if err == nil && cached != nil {
			s.logger.Debug().Msg("Report summary cache hit")
			return cached, nil
		}
	}

	// 2. Cache miss — query ES
	s.logger.Debug().Msg("Report summary cache miss, querying ES")

	summary, err := s.esUptimeRepo.GetUptimeSummary(ctx, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get uptime summary: %w", err)
	}

	if s.serverCounter != nil {
		totalServers, err := s.serverCounter.CountActiveServers(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to count active servers: %w", err)
		}
		summary.TotalServers = totalServers
		if totalServers >= summary.ServersOn {
			summary.ServersOff = totalServers - summary.ServersOn
		}
	}

	// 3. Get low uptime servers
	lowUptime, err := s.esUptimeRepo.GetLowUptimeServers(ctx, startDate, endDate, 10)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to get low uptime servers, continuing without")
	} else {
		summary.LowUptimeServers = lowUptime
	}

	// 4. Cache the complete summary.
	if useCache {
		s.setCachedSummary(ctx, startDate, endDate, summary)
	}

	return summary, nil
}

// SendReport creates and sends a report email on demand.
func (s *reportService) SendReport(ctx context.Context, req *dto.SendReportRequest) (*dto.SendReportResponse, error) {
	// 1. Parse dates
	startDate, endDate, queryEndDate, err := parseReportDateRange(req.StartDate, req.EndDate)
	if err != nil {
		return nil, err
	}

	// 3. Create report job (status=pending)
	job := &model.ReportJob{
		ReportType:     "on_demand",
		Status:         "pending",
		StartDate:      startDate,
		EndDate:        endDate,
		RecipientEmail: req.Email,
	}
	if err := s.reportJobRepo.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create report job: %w", err)
	}

	// 4. Update status to processing
	job.Status = "processing"
	if err := s.reportJobRepo.Update(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to update report job status: %w", err)
	}

	// 5. Get summary
	summary, err := s.GetSummary(ctx, startDate, queryEndDate)
	if err != nil {
		s.markJobFailed(ctx, job, err.Error())
		return nil, fmt.Errorf("failed to get report summary: %w", err)
	}

	// 6. Render HTML email
	reportData := &email.ReportData{
		ReportDate:       fmt.Sprintf("%s — %s", req.StartDate, req.EndDate),
		TotalServers:     summary.TotalServers,
		ServersOn:        summary.ServersOn,
		ServersOff:       summary.ServersOff,
		AvgUptimePct:     summary.AvgUptimePct,
		LowUptimeServers: summary.LowUptimeServers,
	}

	htmlBody, err := email.RenderDailyReport(reportData)
	if err != nil {
		s.markJobFailed(ctx, job, fmt.Sprintf("template render error: %v", err))
		return nil, fmt.Errorf("failed to render email template: %w", err)
	}

	// 7. Send email
	subject := fmt.Sprintf("📊 Server Status Report: %s — %s", req.StartDate, req.EndDate)
	if err := s.emailSender.SendHTML(ctx, req.Email, subject, htmlBody); err != nil {
		s.markJobFailed(ctx, job, fmt.Sprintf("SMTP error: %v", err))
		return nil, fmt.Errorf("failed to send email: %w", err)
	}

	// 8. Update job as completed
	now := time.Now()
	job.Status = "completed"
	job.SentAt = &now
	job.TotalServers = &summary.TotalServers
	job.ServersOn = &summary.ServersOn
	job.ServersOff = &summary.ServersOff
	job.AvgUptimePct = &summary.AvgUptimePct
	if err := s.reportJobRepo.Update(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to update completed report job: %w", err)
	}

	s.logger.Info().
		Str("report_id", job.ID).
		Str("recipient", req.Email).
		Msg("Report email sent successfully")

	return &dto.SendReportResponse{
		ReportID: job.ID,
		Status:   "completed",
		Message:  "Report sent successfully",
		Summary:  summary,
	}, nil
}

// SendDailyReport sends the daily report for yesterday to the admin email.
func (s *reportService) SendDailyReport(ctx context.Context) error {
	// Calculate yesterday as [00:00, next day 00:00).
	now := time.Now()
	yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
	yesterdayEnd := yesterday.Add(24 * time.Hour)

	s.logger.Info().
		Str("date", yesterday.Format("2006-01-02")).
		Str("recipient", s.smtpAdminEmail).
		Msg("Daily report cron triggered")

	job := &model.ReportJob{
		ReportType:     "daily",
		Status:         "pending",
		StartDate:      yesterday,
		EndDate:        yesterday,
		RecipientEmail: s.smtpAdminEmail,
	}
	if err := s.reportJobRepo.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create daily report job: %w", err)
	}
	job.Status = "processing"
	if err := s.reportJobRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update daily report job status: %w", err)
	}

	// Get summary
	summary, err := s.GetSummary(ctx, yesterday, yesterdayEnd)
	if err != nil {
		s.logger.Error().Err(err).Msg("Daily report failed at GetSummary")
		s.markJobFailed(ctx, job, err.Error())
		return fmt.Errorf("daily report summary failed: %w", err)
	}

	// Render HTML
	reportData := &email.ReportData{
		ReportDate:       yesterday.Format("02/01/2006"),
		TotalServers:     summary.TotalServers,
		ServersOn:        summary.ServersOn,
		ServersOff:       summary.ServersOff,
		AvgUptimePct:     summary.AvgUptimePct,
		LowUptimeServers: summary.LowUptimeServers,
	}

	htmlBody, err := email.RenderDailyReport(reportData)
	if err != nil {
		s.markJobFailed(ctx, job, fmt.Sprintf("template render error: %v", err))
		return fmt.Errorf("daily report template render failed: %w", err)
	}

	// Send email to admin
	subject := fmt.Sprintf("📊 Daily Server Status Report — %s", yesterday.Format("02/01/2006"))
	if err := s.emailSender.SendHTML(ctx, s.smtpAdminEmail, subject, htmlBody); err != nil {
		s.markJobFailed(ctx, job, fmt.Sprintf("SMTP error: %v", err))
		return fmt.Errorf("daily report email send failed: %w", err)
	}

	// Save daily snapshot
	snapshotServers := make([]model.ServerUptimeJSON, 0, len(summary.LowUptimeServers))
	for _, s := range summary.LowUptimeServers {
		snapshotServers = append(snapshotServers, model.ServerUptimeJSON{
			ServerID:    s.ServerID,
			ServerName:  s.ServerName,
			UptimePct:   s.UptimePct,
			TotalChecks: s.TotalChecks,
			OnChecks:    s.OnChecks,
		})
	}

	snapshot := &model.DailySnapshot{
		SnapshotDate: yesterday,
		TotalServers: summary.TotalServers,
		ServersOn:    summary.ServersOn,
		ServersOff:   summary.ServersOff,
		AvgUptimePct: summary.AvgUptimePct,
	}
	_ = snapshot.SetLowUptimeServers(snapshotServers)
	if err := s.snapshotRepo.Create(ctx, snapshot); err != nil {
		s.markJobFailed(ctx, job, fmt.Sprintf("snapshot save error: %v", err))
		return fmt.Errorf("daily report snapshot save failed: %w", err)
	}

	nowTime := time.Now()
	job.Status = "completed"
	job.SentAt = &nowTime
	job.TotalServers = &summary.TotalServers
	job.ServersOn = &summary.ServersOn
	job.ServersOff = &summary.ServersOff
	job.AvgUptimePct = &summary.AvgUptimePct
	if err := s.reportJobRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update completed daily report job: %w", err)
	}

	s.logger.Info().
		Str("date", yesterday.Format("2006-01-02")).
		Msg("Daily report sent successfully")

	return nil
}

// Redis cache helpers

func (s *reportService) getCachedSummary(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
	if s.cache == nil {
		return nil, fmt.Errorf("cache not available")
	}

	return s.cache.Get(ctx, startDate, endDate)
}

func (s *reportService) setCachedSummary(ctx context.Context, startDate, endDate time.Time, summary *dto.ReportSummaryResponse) {
	if s.cache == nil {
		return
	}

	if err := s.cache.Set(ctx, startDate, endDate, summary); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to cache report summary")
	}
}

func (s *reportService) markJobFailed(ctx context.Context, job *model.ReportJob, message string) {
	errMsg := message
	job.Status = "failed"
	job.ErrorMessage = &errMsg
	if err := s.reportJobRepo.Update(ctx, job); err != nil {
		s.logger.Warn().Err(err).Str("report_id", job.ID).Msg("Failed to update failed report job")
	}
}

func (s *reportService) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func parseReportDateRange(startDateStr, endDateStr string) (time.Time, time.Time, time.Time, error) {
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, fmt.Errorf("invalid start_date format: %w", err)
	}

	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, fmt.Errorf("invalid end_date format: %w", err)
	}

	if endDate.Before(startDate) {
		return time.Time{}, time.Time{}, time.Time{}, fmt.Errorf("end_date must be on or after start_date")
	}

	queryEndDate := endDate.AddDate(0, 0, 1)
	if queryEndDate.Sub(startDate) > 90*24*time.Hour {
		return time.Time{}, time.Time{}, time.Time{}, fmt.Errorf("date range must not exceed 90 days")
	}

	return startDate, endDate, queryEndDate, nil
}

func (c *redisSummaryCache) Get(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
	key := reportSummaryCacheKey(startDate, endDate)
	val, err := c.get(ctx, key)
	if err != nil {
		return nil, err
	}

	var summary dto.ReportSummaryResponse
	if err := json.Unmarshal([]byte(val), &summary); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached summary: %w", err)
	}

	return &summary, nil
}

func (c *redisSummaryCache) Set(ctx context.Context, startDate, endDate time.Time, summary *dto.ReportSummaryResponse) error {
	data, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("failed to marshal summary for cache: %w", err)
	}

	key := reportSummaryCacheKey(startDate, endDate)
	return c.set(ctx, key, data, 1*time.Hour)
}

func reportSummaryCacheKey(startDate, endDate time.Time) string {
	reportEndDate := endDate.AddDate(0, 0, -1)
	return fmt.Sprintf("report:summary:%s:%s", startDate.Format("2006-01-02"), reportEndDate.Format("2006-01-02"))
}

func shouldUseSummaryCache(startDate, endDate, now time.Time) bool {
	if endDate.IsZero() {
		return false
	}

	nowUTC := now.UTC()
	todayStart := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
	queryEnd := endDate.UTC()

	return !queryEnd.After(todayStart)
}
