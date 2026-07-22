package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/repository"
)

const topLowestUptime = 10

// Sentinel errors for report validation.
var (
	ErrInvalidRange    = errors.New("invalid report range")
	ErrDataUnavailable = errors.New("report data unavailable")
)

// DataUnavailableError names the days whose snapshot never ran, rather than
// silently reporting over a hole.
type DataUnavailableError struct {
	MissingDates []string
}

func (e *DataUnavailableError) Error() string {
	return fmt.Sprintf("missing snapshots for: %s", strings.Join(e.MissingDates, ", "))
}

func (e *DataUnavailableError) Is(target error) bool { return target == ErrDataUnavailable }

// ReportService builds reports from daily_snapshots.
type ReportService interface {
	// Summary reports over [startDate, endDate], both inclusive whole days.
	Summary(ctx context.Context, startDate, endDate string) (*dto.SummaryResponse, error)
	ParseRange(startDate, endDate string, now time.Time) (time.Time, time.Time, error)
	// ServerUptimeRows lists every server the window covers, with per-server
	// uptime, for the email attachment.
	ServerUptimeRows(ctx context.Context, start, end time.Time) ([]repository.ServerUptimeRow, error)
}

type reportService struct {
	snapshots         repository.SnapshotRepository
	loc               *time.Location
	maxRangeDays      int
	coverageThreshold float64
	// now is injectable so "a day that has finished" is testable without the
	// tests expiring tomorrow.
	now func() time.Time
}

// NewReportService creates a ReportService.
func NewReportService(
	snapshots repository.SnapshotRepository,
	loc *time.Location,
	maxRangeDays int,
	coverageThreshold float64,
) ReportService {
	return &reportService{
		snapshots:         snapshots,
		loc:               loc,
		maxRangeDays:      maxRangeDays,
		coverageThreshold: coverageThreshold,
		now:               time.Now,
	}
}

// ParseRange validates the dates. Every rule here exists because a report over
// a day that has not finished has no meaning yet.
func (s *reportService) ParseRange(startDate, endDate string, now time.Time) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation(time.DateOnly, startDate, s.loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: start_date must be YYYY-MM-DD", ErrInvalidRange)
	}
	end, err := time.ParseInLocation(time.DateOnly, endDate, s.loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end_date must be YYYY-MM-DD", ErrInvalidRange)
	}

	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: start_date must not be after end_date", ErrInvalidRange)
	}

	// The snapshot for a day only exists after that day has ended.
	today := startOfDay(now.In(s.loc))
	if !end.Before(today) {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"%w: end_date must be a day that has already finished", ErrInvalidRange)
	}

	if days := int(end.Sub(start).Hours()/24) + 1; days > s.maxRangeDays {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"%w: range of %d days exceeds the %d day maximum", ErrInvalidRange, days, s.maxRangeDays)
	}

	return start, end, nil
}

// Summary reads only daily_snapshots. Elasticsearch is never touched here: the
// snapshot job already condensed it, so an ES outage cannot break a report of
// days that were already snapshotted.
func (s *reportService) Summary(ctx context.Context, startDate, endDate string) (*dto.SummaryResponse, error) {
	start, end, err := s.ParseRange(startDate, endDate, s.now())
	if err != nil {
		return nil, err
	}

	missing, err := s.snapshots.MissingDates(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to check snapshot coverage: %w", err)
	}
	if len(missing) > 0 {
		dates := make([]string, len(missing))
		for i, d := range missing {
			dates[i] = d.Format(time.DateOnly)
		}
		return nil, &DataUnavailableError{MissingDates: dates}
	}

	totals, err := s.snapshots.Totals(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate snapshots: %w", err)
	}

	// The closing status comes from the last day of the window only.
	lastStatus, err := s.snapshots.CountByLastStatus(ctx, end)
	if err != nil {
		return nil, fmt.Errorf("failed to count closing status: %w", err)
	}

	top, err := s.snapshots.LowestUptime(ctx, start, end, topLowestUptime)
	if err != nil {
		return nil, fmt.Errorf("failed to rank uptime: %w", err)
	}
	if top == nil {
		top = []repository.LowUptimeServer{}
	}

	resp := &dto.SummaryResponse{
		StartDate:         start.Format(time.DateOnly),
		EndDate:           end.Format(time.DateOnly),
		TotalServers:      totals.TotalServers,
		ServersOnAtEndAt:  lastStatus["ON"],
		ServersOffAtEndAt: lastStatus["OFF"],
		ServersUptime100:  totals.ServersUptime100,
		ServersUptime0:    totals.ServersUptime0,
		ServersNoData:     totals.ServersNoData,
		AvgUptimePct:      totals.AvgUptimePct,
		ExpectedChecks:    totals.SumExpectedChecks,
		ActualChecks:      totals.SumActualChecks,
		Top10LowestUptime: top,
	}

	// Partial is whatever is neither 100%, 0%, nor missing.
	resp.ServersUptimePartial = totals.TotalServers -
		totals.ServersUptime100 - totals.ServersUptime0 - totals.ServersNoData

	if totals.SumExpectedChecks > 0 {
		resp.CoveragePct = float64(totals.SumActualChecks) / float64(totals.SumExpectedChecks) * 100
	}
	resp.Degraded = resp.CoveragePct < s.coverageThreshold

	return resp, nil
}

// ServerUptimeRows returns the window's whole population with per-server uptime.
// The caller sends this only after Summary succeeds, so coverage is already
// vetted; this is a plain read.
func (s *reportService) ServerUptimeRows(ctx context.Context, start, end time.Time) ([]repository.ServerUptimeRow, error) {
	return s.snapshots.AllServerUptime(ctx, start, end)
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
