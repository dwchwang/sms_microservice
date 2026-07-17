package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/vcs-sms/report-service/internal/model"
	"gorm.io/gorm"
)

// JobRepository owns the report_jobs table.
type JobRepository interface {
	Create(ctx context.Context, job *model.ReportJob) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.ReportJob, error)
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

func (r *jobRepository) Create(ctx context.Context, job *model.ReportJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *jobRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.ReportJob, error) {
	var job model.ReportJob
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&job).Error; err != nil {
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
