package repository

import (
	"context"
	"time"

	"github.com/vcs-sms/report-service/internal/model"
	"gorm.io/gorm"
)

// CronRunRepository arbitrates which replica runs a scheduled job.
// Dates are passed as YYYY-MM-DD so Postgres compares them against the DATE
// column without a timezone round-trip.
type CronRunRepository interface {
	// TryClaim reports whether this owner may run the job for date.
	TryClaim(ctx context.Context, job, date, owner string, staleAfter time.Duration) (bool, error)
	// Heartbeat refreshes the claim. It returns false once the claim is lost,
	// which is the caller's signal to abandon the work in flight.
	Heartbeat(ctx context.Context, job, date, owner string) (bool, error)
	MarkDone(ctx context.Context, job, date, owner string) error
	MarkFailed(ctx context.Context, job, date, owner, errMsg string) error
	IsDone(ctx context.Context, job, date string) (bool, error)
}

type cronRunRepository struct {
	db *gorm.DB
}

// NewCronRunRepository creates a CronRunRepository.
func NewCronRunRepository(db *gorm.DB) CronRunRepository {
	return &cronRunRepository{db: db}
}

// A claim is granted when the row is new, when the previous attempt failed, or
// when the previous owner stopped heartbeating. A 'done' row never matches, so
// a finished job is never run twice.
const claimQuery = `
INSERT INTO cron_runs (job_name, run_date, state, owner, started_at, heartbeat_at)
VALUES (?, ?, 'running', ?, NOW(), NOW())
ON CONFLICT (job_name, run_date) DO UPDATE
   SET state         = 'running',
       owner         = EXCLUDED.owner,
       started_at    = NOW(),
       heartbeat_at  = NOW(),
       finished_at   = NULL,
       error_message = NULL
 WHERE cron_runs.state = 'failed'
    OR (cron_runs.state = 'running'
        AND cron_runs.heartbeat_at < NOW() - make_interval(secs => ?))
RETURNING job_name`

func (r *cronRunRepository) TryClaim(ctx context.Context, job, date, owner string, staleAfter time.Duration) (bool, error) {
	var won []string
	err := r.db.WithContext(ctx).
		Raw(claimQuery, job, date, owner, staleAfter.Seconds()).
		Scan(&won).Error
	if err != nil {
		return false, err
	}
	return len(won) == 1, nil
}

func (r *cronRunRepository) Heartbeat(ctx context.Context, job, date, owner string) (bool, error) {
	res := r.db.WithContext(ctx).
		Model(&model.CronRun{}).
		Where("job_name = ? AND run_date = ? AND owner = ? AND state = ?",
			job, date, owner, model.CronRunning).
		Update("heartbeat_at", gorm.Expr("NOW()"))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *cronRunRepository) MarkDone(ctx context.Context, job, date, owner string) error {
	return r.finish(ctx, job, date, owner, model.CronDone, "")
}

func (r *cronRunRepository) MarkFailed(ctx context.Context, job, date, owner, errMsg string) error {
	return r.finish(ctx, job, date, owner, model.CronFailed, errMsg)
}

// The owner guard matters: a claim already stolen must not be overwritten by
// the loser finishing late.
func (r *cronRunRepository) finish(ctx context.Context, job, date, owner, state, errMsg string) error {
	return r.db.WithContext(ctx).
		Model(&model.CronRun{}).
		Where("job_name = ? AND run_date = ? AND owner = ?", job, date, owner).
		Updates(map[string]any{
			"state":         state,
			"finished_at":   gorm.Expr("NOW()"),
			"error_message": errMsg,
		}).Error
}

func (r *cronRunRepository) IsDone(ctx context.Context, job, date string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.CronRun{}).
		Where("job_name = ? AND run_date = ? AND state = ?", job, date, model.CronDone).
		Count(&count).Error
	return count > 0, err
}
