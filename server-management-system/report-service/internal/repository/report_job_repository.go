package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/vcs-sms/report-service/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JobRepository owns the report_jobs table.
type JobRepository interface {
	// Create inserts the job. inserted is false when the idempotency key is
	// already taken, in which case the caller replays the stored job.
	Create(ctx context.Context, job *model.ReportJob) (inserted bool, err error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.ReportJob, error)
	FindByIdempotency(ctx context.Context, requesterID, key string) (*model.ReportJob, error)
	// FindLatestDaily returns the newest daily job covering exactly date.
	FindLatestDaily(ctx context.Context, date string) (*model.ReportJob, error)
	SetState(ctx context.Context, id uuid.UUID, state string) error
	SetGenerated(ctx context.Context, id uuid.UUID, responseJSON []byte) error
	SetSent(ctx context.Context, id uuid.UUID, messageID string) error
	SetFailed(ctx context.Context, id uuid.UUID, state, errMsg, messageID string) error
}

type jobRepository struct {
	db *gorm.DB
}

// NewJobRepository creates a JobRepository.
func NewJobRepository(db *gorm.DB) JobRepository {
	return &jobRepository{db: db}
}

// Create relies on ux_report_jobs_idem to arbitrate concurrent requests: the
// insert is the lock, so two replicas cannot both win the same key.
func (r *jobRepository) Create(ctx context.Context, job *model.ReportJob) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(job)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *jobRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.ReportJob, error) {
	var job model.ReportJob
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) FindByIdempotency(ctx context.Context, requesterID, key string) (*model.ReportJob, error) {
	var job model.ReportJob
	if err := r.db.WithContext(ctx).
		Where("requester_id = ? AND idempotency_key = ?", requesterID, key).
		First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

// date is YYYY-MM-DD so Postgres compares it against the DATE columns directly.
func (r *jobRepository) FindLatestDaily(ctx context.Context, date string) (*model.ReportJob, error) {
	var job model.ReportJob
	if err := r.db.WithContext(ctx).
		Where("report_type = ? AND start_at = ? AND end_at = ?", model.TypeDaily, date, date).
		Order("created_at DESC").
		First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) SetState(ctx context.Context, id uuid.UUID, state string) error {
	return r.update(ctx, id, map[string]any{"state": state})
}

func (r *jobRepository) SetGenerated(ctx context.Context, id uuid.UUID, responseJSON []byte) error {
	return r.update(ctx, id, map[string]any{
		"state":         model.StateGenerated,
		"response_json": responseJSON,
	})
}

func (r *jobRepository) SetSent(ctx context.Context, id uuid.UUID, messageID string) error {
	return r.update(ctx, id, map[string]any{
		"state":           model.StateSent,
		"smtp_message_id": messageID,
		"sent_at":         time.Now().UTC(),
	})
}

// SetFailed records both failed and delivery_unknown. The message ID is kept
// even when delivery is unknown: it is how an operator checks the Sent folder.
func (r *jobRepository) SetFailed(ctx context.Context, id uuid.UUID, state, errMsg, messageID string) error {
	return r.update(ctx, id, map[string]any{
		"state":           state,
		"error_message":   errMsg,
		"smtp_message_id": messageID,
	})
}

func (r *jobRepository) update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	fields["updated_at"] = time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&model.ReportJob{}).
		Where("id = ?", id).
		Updates(fields).Error
}
