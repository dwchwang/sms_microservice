package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/service"
	"github.com/vcs-sms/report-service/internal/snapshot"
)

// Scheduler runs the two daily jobs.
type Scheduler struct {
	cron      *cron.Cron
	snapshots *snapshot.Job
	sends     service.SendService
	recipient string
	loc       *time.Location
	log       zerolog.Logger
}

// NewScheduler creates a Scheduler bound to the report timezone.
func NewScheduler(
	snapshots *snapshot.Job,
	sends service.SendService,
	recipient string,
	loc *time.Location,
	log zerolog.Logger,
) *Scheduler {
	return &Scheduler{
		cron:      cron.New(cron.WithLocation(loc)),
		snapshots: snapshots,
		sends:     sends,
		recipient: recipient,
		loc:       loc,
		log:       log,
	}
}

// Register wires both jobs. The snapshot must run before the daily report:
// the report reads only what the snapshot wrote.
func (s *Scheduler) Register(snapshotCron, dailyCron string) error {
	if _, err := s.cron.AddFunc(snapshotCron, s.runSnapshot); err != nil {
		return fmt.Errorf("invalid snapshot cron %q: %w", snapshotCron, err)
	}
	if s.recipient == "" {
		s.log.Warn().Msg("REPORT_DAILY_RECIPIENT is empty — the daily report will not be scheduled")
		return nil
	}
	if _, err := s.cron.AddFunc(dailyCron, s.runDailyReport); err != nil {
		return fmt.Errorf("invalid daily cron %q: %w", dailyCron, err)
	}
	return nil
}

func (s *Scheduler) Start() { s.cron.Start() }

// Stop waits for a running job so a snapshot is never cut in half.
func (s *Scheduler) Stop() {
	<-s.cron.Stop().Done()
}

func (s *Scheduler) runSnapshot() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	if _, err := s.snapshots.RunYesterday(ctx, time.Now()); err != nil {
		s.log.Error().Err(err).Msg("Daily snapshot failed")
	}
}

func (s *Scheduler) runDailyReport() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	yesterday := time.Now().In(s.loc).AddDate(0, 0, -1).Format(time.DateOnly)
	_, err := s.sends.Send(ctx, dto.SendReportRequest{
		StartDate:      yesterday,
		EndDate:        yesterday,
		RecipientEmail: s.recipient,
	}, model.TypeDaily, "scheduler", "")
	if err != nil {
		s.log.Error().Err(err).Str("date", yesterday).Msg("Daily report failed")
		return
	}
	s.log.Info().Str("date", yesterday).Msg("Daily report sent")
}
