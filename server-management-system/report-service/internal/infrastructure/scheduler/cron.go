package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/infrastructure/snapshot"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
	"gorm.io/gorm"
)

const (
	tickInterval      = time.Minute
	heartbeatInterval = 30 * time.Second
	// staleAfter is six missed heartbeats.
	staleAfter = 3 * time.Minute

	snapshotTimeout = time.Hour
	dailyTimeout    = 10 * time.Minute
)

// job is one scheduled unit of work, claimed once per run date across the whole
// cluster. The cron expression must fire at most once a day.
type job struct {
	name      string
	schedule  cron.Schedule
	dependsOn string
	timeout   time.Duration
	run       func(ctx context.Context, date time.Time) error
}

// snapshotRunner is the slice of snapshot.Job the scheduler needs.
type snapshotRunner interface {
	Run(ctx context.Context, date time.Time) (*snapshot.Result, error)
}

// dailyJobFinder is the slice of the job repository the scheduler needs.
type dailyJobFinder interface {
	FindLatestDaily(ctx context.Context, date string) (*model.ReportJob, error)
}

// reportSender is the slice of SendService the scheduler needs.
type reportSender interface {
	Send(ctx context.Context, req dto.SendReportRequest, reportType, requesterID, idempotencyKey string) (*dto.JobResponse, error)
}

// Scheduler reconciles the daily jobs. Every replica runs one; the claim in
// cron_runs decides which of them actually does the work.
type Scheduler struct {
	runs       repository.CronRunRepository
	reportJobs dailyJobFinder
	snapshots  snapshotRunner
	sends      reportSender
	recipient  string
	owner      string
	loc        *time.Location
	log        zerolog.Logger

	jobs []job
	// now is injectable so tests do not depend on the day they run.
	now func() time.Time
}

// NewScheduler creates a Scheduler owned by owner, usually the hostname.
func NewScheduler(
	runs repository.CronRunRepository,
	reportJobs dailyJobFinder,
	snapshots snapshotRunner,
	sends reportSender,
	recipient string,
	owner string,
	loc *time.Location,
	log zerolog.Logger,
) *Scheduler {
	return &Scheduler{
		runs:       runs,
		reportJobs: reportJobs,
		snapshots:  snapshots,
		sends:      sends,
		recipient:  recipient,
		owner:      owner,
		loc:        loc,
		log:        log,
		now:        time.Now,
	}
}

// Register parses both cron expressions. The daily report depends on the
// snapshot, so it never runs against a day the snapshot has not condensed yet.
func (s *Scheduler) Register(snapshotCron, dailyCron string) error {
	snapshotSched, err := cron.ParseStandard(snapshotCron)
	if err != nil {
		return fmt.Errorf("invalid snapshot cron %q: %w", snapshotCron, err)
	}
	s.jobs = append(s.jobs, job{
		name:     model.JobSnapshot,
		schedule: snapshotSched,
		timeout:  snapshotTimeout,
		run:      s.runSnapshot,
	})

	if s.recipient == "" {
		s.log.Warn().Msg("REPORT_DAILY_RECIPIENT is empty — the daily report will not be scheduled")
		return nil
	}

	dailySched, err := cron.ParseStandard(dailyCron)
	if err != nil {
		return fmt.Errorf("invalid daily cron %q: %w", dailyCron, err)
	}
	s.jobs = append(s.jobs, job{
		name:      model.JobDailyReport,
		schedule:  dailySched,
		dependsOn: model.JobSnapshot,
		timeout:   dailyTimeout,
		run:       s.runDailyReport,
	})
	return nil
}

// Run reconciles every tick until ctx is cancelled. Ticking rather than firing
// on the cron instant is what makes a missed run recoverable: a replica that
// boots after the scheduled time still finds the job undone and picks it up.
func (s *Scheduler) Run(ctx context.Context) {
	s.log.Info().Str("owner", s.owner).Int("jobs", len(s.jobs)).Msg("Scheduler started")

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		s.tick(ctx)
		select {
		case <-ctx.Done():
			s.log.Info().Msg("Scheduler stopped")
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	now := s.now().In(s.loc)
	for _, j := range s.jobs {
		if ctx.Err() != nil {
			return
		}
		s.consider(ctx, j, now)
	}
}

// consider claims and runs one job if it is due and nobody else holds it.
func (s *Scheduler) consider(ctx context.Context, j job, now time.Time) {
	if !due(j.schedule, now) {
		return
	}

	runDate := startOfDay(now).AddDate(0, 0, -1)
	date := runDate.Format(time.DateOnly)

	if j.dependsOn != "" {
		done, err := s.runs.IsDone(ctx, j.dependsOn, date)
		if err != nil {
			s.log.Error().Err(err).Str("job", j.name).Msg("Failed to check the prerequisite job")
			return
		}
		if !done {
			return
		}
	}

	won, err := s.runs.TryClaim(ctx, j.name, date, s.owner, staleAfter)
	if err != nil {
		s.log.Error().Err(err).Str("job", j.name).Str("date", date).Msg("Failed to claim job")
		return
	}
	if !won {
		return
	}

	s.runClaimed(ctx, j, runDate, date)
}

// due reports whether today's fire time has already passed. Only today's fire
// is reconciled; a day missed entirely is re-run through the internal endpoint.
func due(schedule cron.Schedule, now time.Time) bool {
	fire := schedule.Next(startOfDay(now).Add(-time.Nanosecond))
	if fire.After(now) {
		return false
	}
	return fire.Year() == now.Year() && fire.YearDay() == now.YearDay()
}

func (s *Scheduler) runSnapshot(ctx context.Context, date time.Time) error {
	_, err := s.snapshots.Run(ctx, date)
	return err
}

// runDailyReport refuses to mail a day an earlier attempt already put on the
// wire. The claim alone cannot prevent that: an instance can die after the SMTP
// exchange but before it records the outcome.
func (s *Scheduler) runDailyReport(ctx context.Context, date time.Time) error {
	day := date.Format(time.DateOnly)

	existing, err := s.reportJobs.FindLatestDaily(ctx, day)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check for an earlier daily report: %w", err)
	}
	if existing != nil && !resendable(existing.State) {
		s.log.Warn().Str("date", day).Str("state", existing.State).
			Str("job_id", existing.ID.String()).
			Msg("Daily report was already attempted; not sending again")
		return nil
	}

	if _, err := s.sends.Send(ctx, dto.SendReportRequest{
		StartDate:      day,
		EndDate:        day,
		RecipientEmail: s.recipient,
	}, model.TypeDaily, "scheduler", ""); err != nil {
		return err
	}

	s.log.Info().Str("date", day).Msg("Daily report sent")
	return nil
}

// resendable reports whether an earlier attempt stopped before the body reached
// the wire. StateSending is set before Send is called, so it is not resendable.
func resendable(state string) bool {
	switch state {
	case model.StateProcessing, model.StateGenerated, model.StateFailed:
		return true
	default:
		return false
	}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
