package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/vcs-sms/report-service/config"
	"github.com/vcs-sms/report-service/internal/dto"
	emailMocks "github.com/vcs-sms/report-service/internal/email/mocks"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
	repoMocks "github.com/vcs-sms/report-service/internal/repository/mocks"
)

type fakeSummaryCache struct {
	summary *dto.ReportSummaryResponse
	getErr  error
	gets    int
	sets    int
	setFunc func(ctx context.Context, startDate, endDate time.Time, summary *dto.ReportSummaryResponse) error
}

func (f *fakeSummaryCache) Get(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.summary, nil
}

func (f *fakeSummaryCache) Set(ctx context.Context, startDate, endDate time.Time, summary *dto.ReportSummaryResponse) error {
	f.sets++
	if f.setFunc != nil {
		return f.setFunc(ctx, startDate, endDate, summary)
	}
	return nil
}

func createTestService(
	esMock *repoMocks.UptimeCalculatorMock,
	serverCounter repository.ServerCounter,
	jobMock *repoMocks.ReportJobRepoMock,
	snapMock *repoMocks.DailySnapshotRepoMock,
	emailMock *emailMocks.EmailSenderMock,
) ReportService {
	logger := zerolog.Nop()
	return NewReportService(
		esMock,
		serverCounter,
		jobMock,
		snapMock,
		emailMock,
		nil, // redis nil
		config.SMTPConfig{AdminEmail: "admin@test.com"},
		logger,
	)
}

func TestNewReportService_WithRedisClientInitializesCache(t *testing.T) {
	svc := NewReportService(
		nil,
		nil,
		nil,
		nil,
		nil,
		redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
		config.SMTPConfig{AdminEmail: "admin@test.com"},
		zerolog.Nop(),
	)

	reportSvc, ok := svc.(*reportService)
	assert.True(t, ok)
	assert.NotNil(t, reportSvc.cache)
}

func TestGetSummary_Success(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{
				TotalServers: 100,
				ServersOn:    95,
				ServersOff:   5,
				AvgUptimePct: 95.5,
				TotalChecks:  10000,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return []dto.ServerUptime{
				{ServerID: "srv-1", ServerName: "test-1", UptimePct: 50.0, TotalChecks: 100, OnChecks: 50},
			}, nil
		},
	}

	svc := createTestService(esMock, nil, nil, nil, nil)

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	summary, err := svc.GetSummary(context.Background(), start, end)

	assert.NoError(t, err)
	assert.Equal(t, 100, summary.TotalServers)
	assert.Equal(t, 95, summary.ServersOn)
	assert.Equal(t, 5, summary.ServersOff)
	assert.Equal(t, 95.5, summary.AvgUptimePct)
	assert.Len(t, summary.LowUptimeServers, 1)
}

func TestGetSummary_ESError(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return nil, fmt.Errorf("ES connection failed")
		},
	}

	svc := createTestService(esMock, nil, nil, nil, nil)

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	_, err := svc.GetSummary(context.Background(), start, end)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ES connection failed")
}

func TestGetSummary_LowUptimeErrorIsTolerated(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{
				TotalServers: 50,
				ServersOn:    48,
				ServersOff:   2,
				AvgUptimePct: 96.0,
				TotalChecks:  5000,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return nil, fmt.Errorf("low uptime query failed")
		},
	}

	svc := createTestService(esMock, nil, nil, nil, nil)

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	summary, err := svc.GetSummary(context.Background(), start, end)

	// Should succeed even if low uptime query fails
	assert.NoError(t, err)
	assert.Equal(t, 50, summary.TotalServers)
	assert.Empty(t, summary.LowUptimeServers)
}

func TestGetSummary_UsesPostgresTotalServerCount(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{
				TotalServers: 2,
				ServersOn:    1,
				ServersOff:   1,
				AvgUptimePct: 50.0,
				TotalChecks:  20,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return nil, nil
		},
	}
	serverCounter := &repoMocks.ServerCounterMock{
		CountActiveServersFunc: func(ctx context.Context) (int, error) {
			return 10, nil
		},
	}

	svc := createTestService(esMock, serverCounter, nil, nil, nil)

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	summary, err := svc.GetSummary(context.Background(), start, end)

	assert.NoError(t, err)
	assert.Equal(t, 10, summary.TotalServers)
	assert.Equal(t, 1, summary.ServersOn)
	assert.Equal(t, 9, summary.ServersOff)
}

func TestGetSummary_CacheHitDoesNotQueryES(t *testing.T) {
	esCalled := false
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			esCalled = true
			return nil, fmt.Errorf("should not query ES on cache hit")
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			esCalled = true
			return nil, fmt.Errorf("should not query ES on cache hit")
		},
	}

	svc := &reportService{
		esUptimeRepo: esMock,
		cache: &fakeSummaryCache{summary: &dto.ReportSummaryResponse{
			StartDate:    "2026-06-01",
			EndDate:      "2026-06-01",
			TotalServers: 10,
			ServersOn:    9,
			ServersOff:   1,
			AvgUptimePct: 90,
			LowUptimeServers: []dto.ServerUptime{
				{ServerID: "SRV-1", UptimePct: 10},
			},
		}},
		logger: zerolog.Nop(),
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	summary, err := svc.GetSummary(context.Background(), start, end)

	assert.NoError(t, err)
	assert.False(t, esCalled)
	assert.Equal(t, 10, summary.TotalServers)
	assert.Len(t, summary.LowUptimeServers, 1)
}

func TestShouldUseSummaryCache_BypassesRangesIncludingToday(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	historicalStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	historicalEnd := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	assert.True(t, shouldUseSummaryCache(historicalStart, historicalEnd, now))

	currentStart := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	assert.False(t, shouldUseSummaryCache(currentStart, currentEnd, now))
}

func TestGetSummary_BypassesCacheForCurrentDayRange(t *testing.T) {
	esCalled := false
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			esCalled = true
			return &dto.ReportSummaryResponse{
				TotalServers: 100,
				ServersOn:    80,
				ServersOff:   20,
				AvgUptimePct: 80,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return nil, nil
		},
	}
	cache := &fakeSummaryCache{summary: &dto.ReportSummaryResponse{
		TotalServers: 10,
		ServersOn:    9,
		ServersOff:   1,
		AvgUptimePct: 90,
	}}
	svc := &reportService{
		esUptimeRepo: esMock,
		cache:        cache,
		logger:       zerolog.Nop(),
		clock: func() time.Time {
			return time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
		},
	}

	start := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	summary, err := svc.GetSummary(context.Background(), start, end)

	assert.NoError(t, err)
	assert.True(t, esCalled)
	assert.Equal(t, 0, cache.gets)
	assert.Equal(t, 0, cache.sets)
	assert.Equal(t, 100, summary.TotalServers)
}

func TestGetSummary_CacheMissStoresCompleteSummary(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{
				StartDate:    "2026-06-01",
				EndDate:      "2026-06-10",
				TotalServers: 2,
				ServersOn:    1,
				ServersOff:   1,
				AvgUptimePct: 50,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return []dto.ServerUptime{{ServerID: "SRV-2", UptimePct: 10}}, nil
		},
	}

	var cachedSummary *dto.ReportSummaryResponse
	svc := &reportService{
		esUptimeRepo: esMock,
		cache: &fakeSummaryCache{
			getErr: fmt.Errorf("cache miss"),
			setFunc: func(ctx context.Context, startDate, endDate time.Time, summary *dto.ReportSummaryResponse) error {
				cachedSummary = summary
				assert.Equal(t, "report:summary:2026-06-01:2026-06-10", reportSummaryCacheKey(startDate, endDate))
				return nil
			},
		},
		logger: zerolog.Nop(),
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	summary, err := svc.GetSummary(context.Background(), start, end)

	assert.NoError(t, err)
	assert.Equal(t, summary, cachedSummary)
	assert.Len(t, cachedSummary.LowUptimeServers, 1)
}

func TestRedisSummaryCache_Get(t *testing.T) {
	cache := &redisSummaryCache{
		get: func(ctx context.Context, key string) (string, error) {
			assert.Equal(t, "report:summary:2026-06-01:2026-06-10", key)
			return `{"start_date":"2026-06-01","end_date":"2026-06-10","total_servers":10,"servers_on":9,"servers_off":1,"avg_uptime_pct":90,"low_uptime_servers":[{"server_id":"SRV-1","uptime_pct":10}]}`, nil
		},
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	summary, err := cache.Get(context.Background(), start, end)

	assert.NoError(t, err)
	assert.Equal(t, 10, summary.TotalServers)
	assert.Len(t, summary.LowUptimeServers, 1)
}

func TestRedisSummaryCache_Set(t *testing.T) {
	cache := &redisSummaryCache{
		set: func(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
			assert.Equal(t, "report:summary:2026-06-01:2026-06-10", key)
			assert.Equal(t, time.Hour, ttl)
			data, ok := value.([]byte)
			assert.True(t, ok)
			assert.Contains(t, string(data), `"low_uptime_servers":[{"server_id":"SRV-1"`)
			return nil
		},
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	err := cache.Set(context.Background(), start, end, &dto.ReportSummaryResponse{
		StartDate:    "2026-06-01",
		EndDate:      "2026-06-10",
		TotalServers: 10,
		LowUptimeServers: []dto.ServerUptime{
			{ServerID: "SRV-1", UptimePct: 10},
		},
	})

	assert.NoError(t, err)
}

func TestSetCachedSummary_ToleratesCacheError(t *testing.T) {
	svc := &reportService{
		cache: &fakeSummaryCache{
			setFunc: func(ctx context.Context, startDate, endDate time.Time, summary *dto.ReportSummaryResponse) error {
				return fmt.Errorf("redis unavailable")
			},
		},
		logger: zerolog.Nop(),
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	assert.NotPanics(t, func() {
		svc.setCachedSummary(context.Background(), start, end, &dto.ReportSummaryResponse{})
	})
}

func TestMarkJobFailed_ToleratesUpdateError(t *testing.T) {
	jobMock := &repoMocks.ReportJobRepoMock{
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			return fmt.Errorf("db unavailable")
		},
	}
	svc := &reportService{
		reportJobRepo: jobMock,
		logger:        zerolog.Nop(),
	}
	job := &model.ReportJob{ID: "job-id", Status: "processing"}

	assert.NotPanics(t, func() {
		svc.markJobFailed(context.Background(), job, "failed")
	})
	assert.Equal(t, "failed", job.Status)
	assert.NotNil(t, job.ErrorMessage)
}

func TestSendReport_Success(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{
				TotalServers: 200,
				ServersOn:    190,
				ServersOff:   10,
				AvgUptimePct: 95.0,
				TotalChecks:  20000,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return []dto.ServerUptime{}, nil
		},
	}

	var savedJob *model.ReportJob
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "test-job-id"
			savedJob = job
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			savedJob = job
			return nil
		},
	}

	emailSent := false
	emailMock := &emailMocks.EmailSenderMock{
		SendHTMLFunc: func(ctx context.Context, to, subject, htmlBody string) error {
			emailSent = true
			assert.Contains(t, subject, "Server Status Report")
			return nil
		},
	}

	svc := createTestService(esMock, nil, jobMock, nil, emailMock)

	req := &dto.SendReportRequest{
		StartDate: "2026-06-01",
		EndDate:   "2026-06-10",
		Email:     "user@test.com",
	}

	result, err := svc.SendReport(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "test-job-id", result.ReportID)
	assert.True(t, emailSent)
	assert.NotNil(t, savedJob)
}

func TestSendReport_InvalidStartDate(t *testing.T) {
	svc := createTestService(nil, nil, nil, nil, nil)

	req := &dto.SendReportRequest{
		StartDate: "invalid-date",
		EndDate:   "2026-06-10",
		Email:     "user@test.com",
	}

	_, err := svc.SendReport(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid start_date")
}

func TestSendReport_CreateJobFails(t *testing.T) {
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			return fmt.Errorf("db down")
		},
	}
	svc := createTestService(nil, nil, jobMock, nil, nil)

	_, err := svc.SendReport(context.Background(), &dto.SendReportRequest{
		StartDate: "2026-06-01",
		EndDate:   "2026-06-01",
		Email:     "user@test.com",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create report job")
}

func TestSendReport_ProcessingUpdateFails(t *testing.T) {
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "job-id"
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			return fmt.Errorf("db update failed")
		},
	}
	svc := createTestService(nil, nil, jobMock, nil, nil)

	_, err := svc.SendReport(context.Background(), &dto.SendReportRequest{
		StartDate: "2026-06-01",
		EndDate:   "2026-06-01",
		Email:     "user@test.com",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update report job status")
}

func TestSendReport_EndDateBeforeStartDate(t *testing.T) {
	svc := createTestService(nil, nil, nil, nil, nil)

	req := &dto.SendReportRequest{
		StartDate: "2026-06-10",
		EndDate:   "2026-06-09",
		Email:     "user@test.com",
	}

	_, err := svc.SendReport(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "end_date must be on or after start_date")
}

func TestSendReport_RangeExceeds90Days(t *testing.T) {
	svc := createTestService(nil, nil, nil, nil, nil)

	req := &dto.SendReportRequest{
		StartDate: "2026-01-01",
		EndDate:   "2026-04-01",
		Email:     "user@test.com",
	}

	_, err := svc.SendReport(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "date range must not exceed 90 days")
}

func TestSendReport_EmailFails(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{
				TotalServers: 10,
				ServersOn:    8,
				ServersOff:   2,
				AvgUptimePct: 80.0,
				TotalChecks:  1000,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return []dto.ServerUptime{}, nil
		},
	}

	var savedJob *model.ReportJob
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "fail-job-id"
			savedJob = job
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			savedJob = job
			return nil
		},
	}

	emailMock := &emailMocks.EmailSenderMock{
		SendHTMLFunc: func(ctx context.Context, to, subject, htmlBody string) error {
			return fmt.Errorf("SMTP auth failed")
		},
	}

	svc := createTestService(esMock, nil, jobMock, nil, emailMock)

	req := &dto.SendReportRequest{
		StartDate: "2026-06-01",
		EndDate:   "2026-06-10",
		Email:     "user@test.com",
	}

	_, err := svc.SendReport(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SMTP auth failed")
	assert.Equal(t, "failed", savedJob.Status)
}

func TestSendReport_SummaryFailsMarksJobFailed(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return nil, fmt.Errorf("ES unavailable")
		},
	}
	var savedJob *model.ReportJob
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "summary-fail-id"
			savedJob = job
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			savedJob = job
			return nil
		},
	}
	svc := createTestService(esMock, nil, jobMock, nil, nil)

	_, err := svc.SendReport(context.Background(), &dto.SendReportRequest{
		StartDate: "2026-06-01",
		EndDate:   "2026-06-01",
		Email:     "user@test.com",
	})

	assert.Error(t, err)
	assert.Equal(t, "failed", savedJob.Status)
	assert.Contains(t, *savedJob.ErrorMessage, "failed to get uptime summary")
}

func TestSendReport_CompletedJobUpdateFails(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{TotalServers: 10, ServersOn: 8, ServersOff: 2, AvgUptimePct: 80}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return nil, nil
		},
	}

	updateCalls := 0
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "job-id"
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			updateCalls++
			if job.Status == "completed" {
				return fmt.Errorf("db update failed")
			}
			return nil
		},
	}
	emailMock := &emailMocks.EmailSenderMock{}
	svc := createTestService(esMock, nil, jobMock, nil, emailMock)

	_, err := svc.SendReport(context.Background(), &dto.SendReportRequest{
		StartDate: "2026-06-01",
		EndDate:   "2026-06-01",
		Email:     "user@test.com",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update completed report job")
	assert.GreaterOrEqual(t, updateCalls, 2)
}

func TestSendDailyReport_Success(t *testing.T) {
	summaryCalled := false
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			summaryCalled = true
			return &dto.ReportSummaryResponse{
				TotalServers: 500,
				ServersOn:    485,
				ServersOff:   15,
				AvgUptimePct: 97.0,
				TotalChecks:  50000,
			}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return []dto.ServerUptime{}, nil
		},
	}

	emailSent := false
	emailMock := &emailMocks.EmailSenderMock{
		SendHTMLFunc: func(ctx context.Context, to, subject, htmlBody string) error {
			emailSent = true
			assert.Equal(t, "admin@test.com", to)
			assert.Contains(t, subject, "Daily Server Status Report")
			return nil
		},
	}

	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "daily-job-id"
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			return nil
		},
	}

	snapMock := &repoMocks.DailySnapshotRepoMock{
		CreateFunc: func(ctx context.Context, snapshot *model.DailySnapshot) error {
			return nil
		},
	}

	logger := zerolog.Nop()
	svc := NewReportService(
		esMock,
		nil,
		jobMock,
		snapMock,
		emailMock,
		nil,
		config.SMTPConfig{AdminEmail: "admin@test.com"},
		logger,
	)

	err := svc.SendDailyReport(context.Background())
	assert.NoError(t, err)
	assert.True(t, summaryCalled)
	assert.True(t, emailSent)
}

func TestSendDailyReport_SummaryFails(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return nil, fmt.Errorf("ES unavailable")
		},
	}

	var savedJob *model.ReportJob
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "daily-fail-id"
			savedJob = job
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			savedJob = job
			return nil
		},
	}

	logger := zerolog.Nop()
	svc := NewReportService(
		esMock,
		nil,
		jobMock,
		nil,
		nil,
		nil,
		config.SMTPConfig{AdminEmail: "admin@test.com"},
		logger,
	)

	err := svc.SendDailyReport(context.Background())
	assert.Error(t, err)
	assert.NotNil(t, savedJob)
	assert.Equal(t, "failed", savedJob.Status)
	assert.NotNil(t, savedJob.ErrorMessage)
}

func TestSendDailyReport_EmailFailsMarksJobFailed(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{TotalServers: 10, ServersOn: 8, ServersOff: 2, AvgUptimePct: 80}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return nil, nil
		},
	}
	var savedJob *model.ReportJob
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "daily-email-fail-id"
			savedJob = job
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			savedJob = job
			return nil
		},
	}
	emailMock := &emailMocks.EmailSenderMock{
		SendHTMLFunc: func(ctx context.Context, to, subject, htmlBody string) error {
			return fmt.Errorf("smtp down")
		},
	}
	svc := createTestService(esMock, nil, jobMock, nil, emailMock)

	err := svc.SendDailyReport(context.Background())

	assert.Error(t, err)
	assert.Equal(t, "failed", savedJob.Status)
	assert.Contains(t, *savedJob.ErrorMessage, "SMTP error")
}

func TestSendDailyReport_SnapshotFailsMarksJobFailed(t *testing.T) {
	esMock := &repoMocks.UptimeCalculatorMock{
		GetUptimeSummaryFunc: func(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error) {
			return &dto.ReportSummaryResponse{TotalServers: 10, ServersOn: 8, ServersOff: 2, AvgUptimePct: 80}, nil
		},
		GetLowUptimeServersFunc: func(ctx context.Context, startDate, endDate time.Time, topN int) ([]dto.ServerUptime, error) {
			return []dto.ServerUptime{{ServerID: "SRV-1", UptimePct: 10}}, nil
		},
	}
	var savedJob *model.ReportJob
	jobMock := &repoMocks.ReportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ReportJob) error {
			job.ID = "daily-snapshot-fail-id"
			savedJob = job
			return nil
		},
		UpdateFunc: func(ctx context.Context, job *model.ReportJob) error {
			savedJob = job
			return nil
		},
	}
	snapMock := &repoMocks.DailySnapshotRepoMock{
		CreateFunc: func(ctx context.Context, snapshot *model.DailySnapshot) error {
			return fmt.Errorf("unique violation")
		},
	}
	emailMock := &emailMocks.EmailSenderMock{}
	svc := createTestService(esMock, nil, jobMock, snapMock, emailMock)

	err := svc.SendDailyReport(context.Background())

	assert.Error(t, err)
	assert.Equal(t, "failed", savedJob.Status)
	assert.Contains(t, *savedJob.ErrorMessage, "snapshot save error")
}
