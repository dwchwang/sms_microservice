package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/internal/client"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
)

// RoundSeconds mirrors Monitoring's check interval; expected_checks is derived
// from it, so the two must agree.
const RoundSeconds = 60

// Job condenses one day of raw health facts into per-server snapshots.
type Job struct {
	population client.ServerClient
	facts      repository.UptimeAggregator
	snapshots  repository.SnapshotRepository
	loc        *time.Location
	log        zerolog.Logger
}

// NewJob creates a snapshot Job.
func NewJob(
	population client.ServerClient,
	facts repository.UptimeAggregator,
	snapshots repository.SnapshotRepository,
	loc *time.Location,
	log zerolog.Logger,
) *Job {
	return &Job{population: population, facts: facts, snapshots: snapshots, loc: loc, log: log}
}

// Result summarises one snapshot run.
type Result struct {
	Date          time.Time
	Servers       int
	ServersNoData int
	CoveragePct   float64
}

// RunYesterday snapshots the day before now, in the report timezone.
func (j *Job) RunYesterday(ctx context.Context, now time.Time) (*Result, error) {
	today := startOfDay(now.In(j.loc))
	return j.Run(ctx, today.AddDate(0, 0, -1))
}

// Run snapshots one whole day. The window is half-open: [00:00, next 00:00).
func (j *Job) Run(ctx context.Context, date time.Time) (*Result, error) {
	start := startOfDay(date.In(j.loc))
	end := start.AddDate(0, 0, 1)
	started := time.Now()

	// The population is read from Server Service, never inferred from the
	// facts: a server nobody pinged has to still appear in the report, and it
	// can only do that if its existence is known independently of the measuring.
	servers, err := j.population.Population(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to read population: %w", err)
	}

	measured, err := j.facts.AggregateDay(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate facts: %w", err)
	}

	snapshots, result := j.merge(servers, measured, start, end)

	if err := j.snapshots.Upsert(ctx, snapshots); err != nil {
		return nil, fmt.Errorf("failed to write snapshots: %w", err)
	}

	j.log.Info().
		Str("date", start.Format(time.DateOnly)).
		Int("servers", result.Servers).
		Int("servers_no_data", result.ServersNoData).
		Float64("coverage_pct", result.CoveragePct).
		Dur("took", time.Since(started)).
		Msg("Daily snapshot written")

	return result, nil
}

// merge left-joins the population onto the measured facts.
func (j *Job) merge(
	servers []client.PopulationServer,
	measured map[string]repository.ServerFacts,
	start, end time.Time,
) ([]model.DailySnapshot, *Result) {
	out := make([]model.DailySnapshot, 0, len(servers))
	result := &Result{Date: start, Servers: len(servers)}
	var sumActual, sumExpected int

	for _, srv := range servers {
		expected := expectedChecks(srv, start, end)
		snap := model.DailySnapshot{
			ServerID:       srv.ServerID,
			Date:           start,
			ServerName:     srv.ServerName,
			ExpectedChecks: expected,
		}

		facts, ok := measured[srv.ServerID]
		if !ok || facts.ActualChecks == 0 {
			// No facts: uptime is unknown, not zero. Leaving uptime_pct NULL
			// keeps it out of AVG and counts it as no_data instead.
			result.ServersNoData++
			out = append(out, snap)
			sumExpected += expected
			continue
		}

		// The name from the last fact is the name as of that day, which is what
		// a historical report should show even if the server was renamed since.
		if facts.ServerName != "" {
			snap.ServerName = facts.ServerName
		}
		snap.OnChecks = facts.OnChecks
		snap.ActualChecks = facts.ActualChecks
		uptime := float64(facts.OnChecks) / float64(facts.ActualChecks) * 100
		snap.UptimePct = &uptime
		if facts.LastStatus != "" {
			status := facts.LastStatus
			snap.LastStatus = &status
		}

		out = append(out, snap)
		sumActual += facts.ActualChecks
		sumExpected += expected
	}

	if sumExpected > 0 {
		result.CoveragePct = float64(sumActual) / float64(sumExpected) * 100
	}
	return out, result
}

// expectedChecks counts the rounds a server was alive for during the window.
// A server created at 18:00 expects 360 checks, not 1.440 — without this,
// creating a server would look like a monitoring outage.
func expectedChecks(srv client.PopulationServer, start, end time.Time) int {
	from := start
	if srv.CreatedAt.After(from) {
		from = srv.CreatedAt
	}
	to := end
	if srv.DeletedAt != nil && srv.DeletedAt.Before(to) {
		to = *srv.DeletedAt
	}
	if !to.After(from) {
		return 0
	}
	return int(to.Sub(from).Seconds()) / RoundSeconds
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
