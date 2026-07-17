package snapshot

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/internal/client"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
)

var (
	loc, _  = time.LoadLocation("Asia/Ho_Chi_Minh")
	testDay = time.Date(2026, 7, 15, 0, 0, 0, 0, loc)
	errBoom = errors.New("boom")
)

type fakePopulation struct {
	servers []client.PopulationServer
	err     error
	gotFrom time.Time
	gotTo   time.Time
}

func (f *fakePopulation) Population(ctx context.Context, start, end time.Time) ([]client.PopulationServer, error) {
	f.gotFrom, f.gotTo = start, end
	return f.servers, f.err
}

type fakeAggregator struct {
	facts map[string]repository.ServerFacts
	err   error
}

func (f *fakeAggregator) AggregateDay(ctx context.Context, start, end time.Time) (map[string]repository.ServerFacts, error) {
	return f.facts, f.err
}

type fakeSnapshots struct {
	repository.SnapshotRepository
	written []model.DailySnapshot
	err     error
}

func (f *fakeSnapshots) Upsert(ctx context.Context, s []model.DailySnapshot) error {
	if f.err != nil {
		return f.err
	}
	f.written = append(f.written, s...)
	return nil
}

func newTestJob(pop []client.PopulationServer, facts map[string]repository.ServerFacts) (*Job, *fakeSnapshots) {
	snaps := &fakeSnapshots{}
	job := NewJob(
		&fakePopulation{servers: pop},
		&fakeAggregator{facts: facts},
		snaps, loc, zerolog.New(io.Discard),
	)
	return job, snaps
}

// A server alive all day expects a check every round.
func liveAllDay(id, name string) client.PopulationServer {
	return client.PopulationServer{
		ServerID:   id,
		ServerName: name,
		CreatedAt:  testDay.AddDate(0, 0, -30),
	}
}

func byID(snaps []model.DailySnapshot, id string) *model.DailySnapshot {
	for i := range snaps {
		if snaps[i].ServerID == id {
			return &snaps[i]
		}
	}
	return nil
}

func TestRun_ComputesUptime(t *testing.T) {
	job, snaps := newTestJob(
		[]client.PopulationServer{liveAllDay("SRV-001", "web-01")},
		map[string]repository.ServerFacts{
			"SRV-001": {ServerID: "SRV-001", ServerName: "web-01", OnChecks: 720, ActualChecks: 1440, LastStatus: "ON"},
		},
	)

	if _, err := job.Run(context.Background(), testDay); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	got := byID(snaps.written, "SRV-001")
	if got == nil {
		t.Fatal("no snapshot written")
	}
	if got.UptimePct == nil || *got.UptimePct != 50 {
		t.Errorf("UptimePct = %v, want 50", got.UptimePct)
	}
	if got.OnChecks != 720 || got.ActualChecks != 1440 {
		t.Errorf("checks = %d/%d, want 720/1440", got.OnChecks, got.ActualChecks)
	}
	if got.LastStatus == nil || *got.LastStatus != "ON" {
		t.Errorf("LastStatus = %v, want ON", got.LastStatus)
	}
}

// The whole reason population comes from Server Service: a server nobody
// pinged must still appear, or the gap disappears from the report.
func TestRun_ServerWithNoFactsBecomesNoData(t *testing.T) {
	job, snaps := newTestJob(
		[]client.PopulationServer{liveAllDay("SRV-404", "never-pinged")},
		map[string]repository.ServerFacts{},
	)

	result, err := job.Run(context.Background(), testDay)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	got := byID(snaps.written, "SRV-404")
	if got == nil {
		t.Fatal("a server with no facts was dropped from the snapshot")
	}
	// Zero would say the server was down all day; NULL says nobody looked.
	if got.UptimePct != nil {
		t.Errorf("UptimePct = %v, want nil", *got.UptimePct)
	}
	if got.ActualChecks != 0 {
		t.Errorf("ActualChecks = %d, want 0", got.ActualChecks)
	}
	if got.LastStatus != nil {
		t.Errorf("LastStatus = %v, want nil", *got.LastStatus)
	}
	// It still contributes expected_checks, which is what drags coverage down.
	if got.ExpectedChecks != 1440 {
		t.Errorf("ExpectedChecks = %d, want 1440", got.ExpectedChecks)
	}
	if result.ServersNoData != 1 {
		t.Errorf("ServersNoData = %d, want 1", result.ServersNoData)
	}
	if result.CoveragePct != 0 {
		t.Errorf("CoveragePct = %v, want 0", result.CoveragePct)
	}
}

// A server created mid-day expects fewer checks; otherwise creating a server
// would look like a monitoring outage.
func TestRun_ExpectedChecksFollowLifecycle(t *testing.T) {
	createdAt18 := time.Date(2026, 7, 15, 18, 0, 0, 0, loc)
	job, snaps := newTestJob(
		[]client.PopulationServer{
			{ServerID: "SRV-NEW", ServerName: "new", CreatedAt: createdAt18},
		},
		map[string]repository.ServerFacts{
			"SRV-NEW": {ServerID: "SRV-NEW", OnChecks: 360, ActualChecks: 360, LastStatus: "ON"},
		},
	)

	result, err := job.Run(context.Background(), testDay)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	got := byID(snaps.written, "SRV-NEW")
	if got.ExpectedChecks != 360 {
		t.Errorf("ExpectedChecks = %d, want 360 for a server created at 18:00", got.ExpectedChecks)
	}
	// 360 of 360 expected: full coverage, not 25%.
	if result.CoveragePct != 100 {
		t.Errorf("CoveragePct = %v, want 100", result.CoveragePct)
	}
}

func TestRun_ExpectedChecksStopAtDeletion(t *testing.T) {
	deletedAt06 := time.Date(2026, 7, 15, 6, 0, 0, 0, loc)
	job, snaps := newTestJob(
		[]client.PopulationServer{
			{ServerID: "SRV-DEL", ServerName: "deleted", CreatedAt: testDay.AddDate(0, 0, -5), DeletedAt: &deletedAt06},
		},
		map[string]repository.ServerFacts{},
	)

	if _, err := job.Run(context.Background(), testDay); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if got := byID(snaps.written, "SRV-DEL"); got.ExpectedChecks != 360 {
		t.Errorf("ExpectedChecks = %d, want 360 for a server deleted at 06:00", got.ExpectedChecks)
	}
}

// The name from the last fact is the name as of that day, so a later rename
// does not rewrite history.
func TestRun_PrefersServerNameFromTheFact(t *testing.T) {
	job, snaps := newTestJob(
		[]client.PopulationServer{liveAllDay("SRV-001", "renamed-since")},
		map[string]repository.ServerFacts{
			"SRV-001": {ServerID: "SRV-001", ServerName: "name-that-day", OnChecks: 10, ActualChecks: 10},
		},
	)

	if _, err := job.Run(context.Background(), testDay); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if got := byID(snaps.written, "SRV-001"); got.ServerName != "name-that-day" {
		t.Errorf("ServerName = %q, want the name from the fact", got.ServerName)
	}
}

// Facts for a server outside the population are ignored: population defines
// who is in the report.
func TestRun_IgnoresFactsOutsidePopulation(t *testing.T) {
	job, snaps := newTestJob(
		[]client.PopulationServer{liveAllDay("SRV-001", "web-01")},
		map[string]repository.ServerFacts{
			"SRV-001":   {ServerID: "SRV-001", OnChecks: 10, ActualChecks: 10},
			"SRV-GHOST": {ServerID: "SRV-GHOST", OnChecks: 10, ActualChecks: 10},
		},
	)

	if _, err := job.Run(context.Background(), testDay); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(snaps.written) != 1 {
		t.Fatalf("wrote %d snapshots, want 1", len(snaps.written))
	}
	if byID(snaps.written, "SRV-GHOST") != nil {
		t.Error("a server outside the population was snapshotted")
	}
}

func TestRun_CoverageMixesMeasuredAndMissing(t *testing.T) {
	job, _ := newTestJob(
		[]client.PopulationServer{
			liveAllDay("SRV-001", "measured"),
			liveAllDay("SRV-002", "missing"),
		},
		map[string]repository.ServerFacts{
			"SRV-001": {ServerID: "SRV-001", OnChecks: 1440, ActualChecks: 1440},
		},
	)

	result, err := job.Run(context.Background(), testDay)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// 1440 measured of 2880 expected. The missing server drags coverage to 50%
	// rather than vanishing.
	if result.CoveragePct != 50 {
		t.Errorf("CoveragePct = %v, want 50", result.CoveragePct)
	}
	if result.Servers != 2 || result.ServersNoData != 1 {
		t.Errorf("servers = %d, no_data = %d, want 2 and 1", result.Servers, result.ServersNoData)
	}
}

// The day window is half-open in the report timezone, not UTC.
func TestRun_UsesReportTimezoneDayBoundaries(t *testing.T) {
	pop := &fakePopulation{}
	job := NewJob(pop, &fakeAggregator{}, &fakeSnapshots{}, loc, zerolog.New(io.Discard))

	if _, err := job.Run(context.Background(), testDay); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	wantFrom := time.Date(2026, 7, 15, 0, 0, 0, 0, loc)
	wantTo := time.Date(2026, 7, 16, 0, 0, 0, 0, loc)
	if !pop.gotFrom.Equal(wantFrom) || !pop.gotTo.Equal(wantTo) {
		t.Errorf("window = [%v, %v), want [%v, %v)", pop.gotFrom, pop.gotTo, wantFrom, wantTo)
	}
}

func TestRunYesterday_PicksTheDayBefore(t *testing.T) {
	pop := &fakePopulation{}
	job := NewJob(pop, &fakeAggregator{}, &fakeSnapshots{}, loc, zerolog.New(io.Discard))
	now := time.Date(2026, 7, 16, 0, 30, 0, 0, loc)

	result, err := job.RunYesterday(context.Background(), now)
	if err != nil {
		t.Fatalf("RunYesterday failed: %v", err)
	}

	if got := result.Date.Format(time.DateOnly); got != "2026-07-15" {
		t.Errorf("snapshotted %s, want 2026-07-15", got)
	}
}

func TestRun_PropagatesFailures(t *testing.T) {
	cases := map[string]func(*Job){
		"population": func(j *Job) { j.population = &fakePopulation{err: errBoom} },
		"aggregate":  func(j *Job) { j.facts = &fakeAggregator{err: errBoom} },
		"upsert":     func(j *Job) { j.snapshots = &fakeSnapshots{err: errBoom} },
	}

	for name, brk := range cases {
		job, _ := newTestJob([]client.PopulationServer{liveAllDay("SRV-001", "web")}, nil)
		brk(job)

		if _, err := job.Run(context.Background(), testDay); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
