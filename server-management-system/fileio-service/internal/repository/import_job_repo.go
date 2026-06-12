package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/vcs-sms/fileio-service/internal/model"
	"gorm.io/gorm"
)

// ImportJobRepo defines the interface for import job data access.
type ImportJobRepo interface {
	Create(ctx context.Context, job *model.ImportJob) error
	FindByID(ctx context.Context, jobID string) (*model.ImportJob, error)
	UpdateStatus(ctx context.Context, jobID string, status string) error
	UpdateCompleted(ctx context.Context, jobID string, totalRows, successCount, failedCount int) error
	UpdateFailed(ctx context.Context, jobID string, errMsg string) error
	SaveDetail(ctx context.Context, detail *model.ImportJobDetail) error
	CreateServerWithDetail(ctx context.Context, server *model.Server, detail *model.ImportJobDetail) error
	SaveDetailsBatch(ctx context.Context, details []model.ImportJobDetail) error
	GetDetailsByJobID(ctx context.Context, jobID string) ([]model.ImportJobDetail, error)
}

// importJobRepo implements ImportJobRepo using GORM.
type importJobRepo struct {
	db *gorm.DB
}

// NewImportJobRepo creates a new ImportJobRepo instance.
func NewImportJobRepo(db *gorm.DB) ImportJobRepo {
	return &importJobRepo{db: db}
}

// Create inserts a new import job.
func (r *importJobRepo) Create(ctx context.Context, job *model.ImportJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

// FindByID retrieves an import job by its ID.
func (r *importJobRepo) FindByID(ctx context.Context, jobID string) (*model.ImportJob, error) {
	uid, err := uuid.Parse(jobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job_id: %w", err)
	}

	var job model.ImportJob
	err = r.db.WithContext(ctx).Where("id = ?", uid).First(&job).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// UpdateStatus updates only the status field of an import job.
func (r *importJobRepo) UpdateStatus(ctx context.Context, jobID string, status string) error {
	uid, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job_id: %w", err)
	}

	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now().UTC(),
	}
	if status == "processing" {
		now := time.Now().UTC()
		updates["started_at"] = now
	}

	return r.db.WithContext(ctx).
		Model(&model.ImportJob{}).
		Where("id = ?", uid).
		Updates(updates).Error
}

// UpdateCompleted marks a job as completed with counts.
func (r *importJobRepo) UpdateCompleted(ctx context.Context, jobID string, totalRows, successCount, failedCount int) error {
	uid, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job_id: %w", err)
	}

	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&model.ImportJob{}).
		Where("id = ?", uid).
		Updates(map[string]interface{}{
			"status":        "completed",
			"total_rows":    totalRows,
			"success_count": successCount,
			"failed_count":  failedCount,
			"completed_at":  now,
			"updated_at":    now,
		}).Error
}

// UpdateFailed marks a job as failed with an error message.
func (r *importJobRepo) UpdateFailed(ctx context.Context, jobID string, errMsg string) error {
	uid, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("invalid job_id: %w", err)
	}

	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&model.ImportJob{}).
		Where("id = ?", uid).
		Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": errMsg,
			"completed_at":  now,
			"updated_at":    now,
		}).Error
}

// SaveDetail inserts a single import job detail row.
func (r *importJobRepo) SaveDetail(ctx context.Context, detail *model.ImportJobDetail) error {
	return r.db.WithContext(ctx).Create(detail).Error
}

// CreateServerWithDetail inserts a server and its success detail atomically.
func (r *importJobRepo) CreateServerWithDetail(ctx context.Context, server *model.Server, detail *model.ImportJobDetail) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(server).Error; err != nil {
			return err
		}
		if err := tx.Create(detail).Error; err != nil {
			return err
		}
		return nil
	})
}

// SaveDetailsBatch inserts multiple import job detail rows in a batch.
func (r *importJobRepo) SaveDetailsBatch(ctx context.Context, details []model.ImportJobDetail) error {
	if len(details) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(details, 100).Error
}

// GetDetailsByJobID retrieves all detail rows for a given job.
func (r *importJobRepo) GetDetailsByJobID(ctx context.Context, jobID string) ([]model.ImportJobDetail, error) {
	uid, err := uuid.Parse(jobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job_id: %w", err)
	}

	var details []model.ImportJobDetail
	err = r.db.WithContext(ctx).
		Where("import_job_id = ?", uid).
		Order("row_number ASC").
		Find(&details).Error
	return details, err
}

// Ensure interface compliance.
var _ ImportJobRepo = (*importJobRepo)(nil)
