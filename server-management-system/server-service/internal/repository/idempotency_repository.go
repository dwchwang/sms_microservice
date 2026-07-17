package repository

import (
	"context"
	"time"

	"github.com/vcs-sms/server-service/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IdempotencyRepository stores the outcome of keyed mutations.
type IdempotencyRepository interface {
	// Claim reserves the key. claimed is false when the key already exists, in
	// which case existing holds the stored record.
	Claim(ctx context.Context, rec *model.Idempotency) (claimed bool, existing *model.Idempotency, err error)
	Complete(ctx context.Context, rec *model.Idempotency) error
	Release(ctx context.Context, actorID, endpoint, key string) error
}

type idempotencyRepository struct {
	db *gorm.DB
}

// NewIdempotencyRepository creates an IdempotencyRepository.
func NewIdempotencyRepository(db *gorm.DB) IdempotencyRepository {
	return &idempotencyRepository{db: db}
}

// Claim inserts the key, or reports the record that already holds it. The
// insert is the lock: two concurrent requests cannot both win the key.
func (r *idempotencyRepository) Claim(ctx context.Context, rec *model.Idempotency) (bool, *model.Idempotency, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(rec)
	if res.Error != nil {
		return false, nil, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil, nil
	}

	var existing model.Idempotency
	err := r.db.WithContext(ctx).
		Where("actor_id = ? AND endpoint = ? AND idempotency_key = ?",
			rec.ActorID, rec.Endpoint, rec.IdempotencyKey).
		First(&existing).Error
	if err != nil {
		return false, nil, err
	}

	// An expired record is reusable: take it over rather than reject a retry.
	if time.Now().UTC().After(existing.ExpiresAt) {
		if err := r.db.WithContext(ctx).
			Model(&model.Idempotency{}).
			Where("actor_id = ? AND endpoint = ? AND idempotency_key = ?",
				rec.ActorID, rec.Endpoint, rec.IdempotencyKey).
			Updates(map[string]any{
				"request_hash":  rec.RequestHash,
				"state":         model.IdempotencyProcessing,
				"status_code":   0,
				"response_body": nil,
				"expires_at":    rec.ExpiresAt,
			}).Error; err != nil {
			return false, nil, err
		}
		return true, nil, nil
	}

	return false, &existing, nil
}

// Complete stores the response so a retry can replay it.
func (r *idempotencyRepository) Complete(ctx context.Context, rec *model.Idempotency) error {
	return r.db.WithContext(ctx).
		Model(&model.Idempotency{}).
		Where("actor_id = ? AND endpoint = ? AND idempotency_key = ?",
			rec.ActorID, rec.Endpoint, rec.IdempotencyKey).
		Updates(map[string]any{
			"state":         model.IdempotencyCompleted,
			"status_code":   rec.StatusCode,
			"response_body": rec.ResponseBody,
		}).Error
}

// Release drops a claim whose handler never produced a storable response, so
// the key is not left wedged in processing until it expires.
func (r *idempotencyRepository) Release(ctx context.Context, actorID, endpoint, key string) error {
	return r.db.WithContext(ctx).
		Where("actor_id = ? AND endpoint = ? AND idempotency_key = ?", actorID, endpoint, key).
		Delete(&model.Idempotency{}).Error
}
