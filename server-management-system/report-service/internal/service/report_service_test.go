package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vcs-sms/report-service/internal/repository"
)

var (
	loc, _  = time.LoadLocation("Asia/Ho_Chi_Minh")
	errBoom = errors.New("boom")
)

type fakeSnapshots struct {
	repository.SnapshotRepository
	missing    []time.Time
	totals     *repository.Totals
	lastStatus map[string]int64
	top        []repository.LowUptimeServer
	gotStart   time.Time
	gotEnd     time.Time
	gotLastDay time.Time
	uptimeRows []repository.ServerUptimeRow
	missingErr error
	totalsErr  error
	statusErr  error
	topErr     error
	uptimeErr  error
}

func (f *fakeSnapshots) AllServerUptime(ctx context.Context, start, end time.Time) ([]repository.ServerUptimeRow, error) {
	return f.uptimeRows, f.uptimeErr
}

func (f *fakeSnapshots) MissingDates(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	f.gotStart, f.gotEnd = start, end
	return f.missing, f.missingErr
}

func (f *fakeSnapshots) Totals(ctx context.Context, start, end time.Time) (*repository.Totals, error) {
	if f.totalsErr != nil {
		return nil, f.totalsErr
	}
	if f.totals == nil {
		return &repository.Totals{}, nil
	}
	return f.totals, nil
}

func (f *fakeSnapshots) CountByLastStatus(ctx context.Context, date time.Time) (map[string]int64, error) {
	f.gotLastDay = date
	return f.lastStatus, f.statusErr
}

func (f *fakeSnapshots) LowestUptime(ctx context.Context, start, end time.Time, limit int) ([]repository.LowUptimeServer, error) {
	return f.top, f.topErr
}

func newTestService(snaps *fakeSnapshots) ReportService {
	svc := NewReportService(snaps, loc, 31, 95.0).(*reportService)
	svc.now = func() time.Time { return now }
	return svc
}

func ptr(v float64) *float64 { return &v }

// now is fixed so "a day that has finished" is deterministic.
var now = time.Date(2026, 7, 17, 9, 0, 0, 0, loc)

func TestParseRange_Accepts(t *testing.T) {
	svc := newTestService(&fakeSnapshots{})

	start, end, err := svc.ParseRange("2026-07-15", "2026-07-16", now)
	if err != nil {
		t.Fatalf("ParseRange failed: %v", err)
	}
	if start.Format(time.DateOnly) != "2026-07-15" || end.Format(time.DateOnly) != "2026-07-16" {
		t.Errorf("range = %v..%v", start, end)
	}
}

func TestParseRange_Rejects(t *testing.T) {
	svc := newTestService(&fakeSnapshots{})

	cases := map[string][2]string{
		"start not a date":  {"yesterday", "2026-07-16"},
		"end not a date":    {"2026-07-15", "tomorrow"},
		"wrong format":      {"15/07/2026", "16/07/2026"},
		"start after end":   {"2026-07-16", "2026-07-15"},
		"end is today":      {"2026-07-16", "2026-07-17"},
		"end in the future": {"2026-07-18", "2026-07-19"},
		"range too wide":    {"2026-05-01", "2026-07-16"},
	}

	for name, c := range cases {
		if _, _, err := svc.ParseRange(c[0], c[1], now); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("%s: err = %v, want ErrInvalidRange", name, err)
		}
	}
}

// A day that has not ended has no snapshot, so uptime for it is meaningless.
func TestParseRange_RejectsToday(t *testing.T) {
	svc := newTestService(&fakeSnapshots{})

	_, _, err := svc.ParseRange("2026-07-17", "2026-07-17", now)

	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("err = %v, want ErrInvalidRange for today", err)
	}
}

func TestParseRange_AcceptsExactlyMaxDays(t *testing.T) {
	svc := newTestService(&fakeSnapshots{})

	// 2026-06-16..2026-07-16 inclusive is 31 days.
	if _, _, err := svc.ParseRange("2026-06-16", "2026-07-16", now); err != nil {
		t.Fatalf("31 days should be accepted: %v", err)
	}
}

// A missing snapshot must fail loudly rather than average over a hole.
func TestSummary_MissingSnapshotIsRefused(t *testing.T) {
	snaps := &fakeSnapshots{missing: []time.Time{
		time.Date(2026, 7, 15, 0, 0, 0, 0, loc),
	}}
	svc := newTestService(snaps)

	_, err := svc.Summary(context.Background(), "2026-07-15", "2026-07-16")

	if !errors.Is(err, ErrDataUnavailable) {
		t.Fatalf("err = %v, want ErrDataUnavailable", err)
	}
	var dataErr *DataUnavailableError
	if !errors.As(err, &dataErr) {
		t.Fatal("expected the error to name the missing dates")
	}
	if len(dataErr.MissingDates) != 1 || dataErr.MissingDates[0] != "2026-07-15" {
		t.Errorf("MissingDates = %v", dataErr.MissingDates)
	}
}

func TestSummary_ComputesDistributionAndCoverage(t *testing.T) {
	snaps := &fakeSnapshots{
		totals: &repository.Totals{
			TotalServers:      10,
			AvgUptimePct:      ptr(99.2),
			ServersUptime100:  6,
			ServersUptime0:    1,
			ServersNoData:     2,
			SumActualChecks:   800,
			SumExpectedChecks: 1000,
		},
		lastStatus: map[string]int64{"ON": 7, "OFF": 1},
	}
	svc := newTestService(snaps)

	resp, err := svc.Summary(context.Background(), "2026-07-16", "2026-07-16")
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}

	// Partial is the remainder, so the four buckets always sum to the total.
	if resp.ServersUptimePartial != 1 {
		t.Errorf("ServersUptimePartial = %d, want 1", resp.ServersUptimePartial)
	}
	sum := resp.ServersUptime100 + resp.ServersUptimePartial + resp.ServersUptime0 + resp.ServersNoData
	if sum != resp.TotalServers {
		t.Errorf("buckets sum to %d, want total_servers %d", sum, resp.TotalServers)
	}
	if resp.CoveragePct != 80 {
		t.Errorf("CoveragePct = %v, want 80", resp.CoveragePct)
	}
	if !resp.Degraded {
		t.Error("80%% coverage is below the 95%% threshold and must be degraded")
	}
	if resp.ServersOnAtEndAt != 7 || resp.ServersOffAtEndAt != 1 {
		t.Errorf("on/off at end = %d/%d, want 7/1", resp.ServersOnAtEndAt, resp.ServersOffAtEndAt)
	}
}

func TestSummary_HealthyCoverageIsNotDegraded(t *testing.T) {
	snaps := &fakeSnapshots{
		totals: &repository.Totals{
			TotalServers: 10, SumActualChecks: 998, SumExpectedChecks: 1000,
		},
	}
	svc := newTestService(snaps)

	resp, err := svc.Summary(context.Background(), "2026-07-16", "2026-07-16")
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	if resp.Degraded {
		t.Errorf("coverage %v should not be degraded", resp.CoveragePct)
	}
}

// The closing status is read from the last day of the window, not the first.
func TestSummary_ClosingStatusUsesEndDate(t *testing.T) {
	snaps := &fakeSnapshots{lastStatus: map[string]int64{"ON": 1}}
	svc := newTestService(snaps)

	if _, err := svc.Summary(context.Background(), "2026-07-14", "2026-07-16"); err != nil {
		t.Fatalf("Summary failed: %v", err)
	}

	if got := snaps.gotLastDay.Format(time.DateOnly); got != "2026-07-16" {
		t.Errorf("closing status read from %s, want the end date", got)
	}
}

// Everything having no data leaves the average unknown rather than 0.
func TestSummary_AvgIsNullWhenNothingHasData(t *testing.T) {
	snaps := &fakeSnapshots{
		totals: &repository.Totals{TotalServers: 3, ServersNoData: 3, AvgUptimePct: nil},
	}
	svc := newTestService(snaps)

	resp, err := svc.Summary(context.Background(), "2026-07-16", "2026-07-16")
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	if resp.AvgUptimePct != nil {
		t.Errorf("AvgUptimePct = %v, want nil", *resp.AvgUptimePct)
	}
}

func TestSummary_ZeroExpectedChecksDoesNotDivideByZero(t *testing.T) {
	svc := newTestService(&fakeSnapshots{totals: &repository.Totals{}})

	resp, err := svc.Summary(context.Background(), "2026-07-16", "2026-07-16")
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	if resp.CoveragePct != 0 {
		t.Errorf("CoveragePct = %v, want 0", resp.CoveragePct)
	}
}

func TestSummary_EmptyTopIsAnArray(t *testing.T) {
	svc := newTestService(&fakeSnapshots{})

	resp, err := svc.Summary(context.Background(), "2026-07-16", "2026-07-16")
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	// Serialising nil would emit null instead of [].
	if resp.Top10LowestUptime == nil {
		t.Error("expected an empty array rather than nil")
	}
}

func TestSummary_PropagatesRepositoryFailures(t *testing.T) {
	cases := map[string]*fakeSnapshots{
		"missing": {missingErr: errBoom},
		"totals":  {totalsErr: errBoom},
		"status":  {statusErr: errBoom},
		"top":     {topErr: errBoom},
	}

	for name, snaps := range cases {
		svc := newTestService(snaps)
		if _, err := svc.Summary(context.Background(), "2026-07-16", "2026-07-16"); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSummary_RejectsInvalidRangeBeforeQuerying(t *testing.T) {
	snaps := &fakeSnapshots{}
	svc := newTestService(snaps)

	_, err := svc.Summary(context.Background(), "2026-07-17", "2026-07-17")

	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("err = %v, want ErrInvalidRange", err)
	}
	if !snaps.gotStart.IsZero() {
		t.Error("queried the database despite an invalid range")
	}
}
