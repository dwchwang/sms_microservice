package repository

import (
	"context"

	"github.com/vcs-sms/monitor-service/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HealthCheckConfigRepo defines the interface for health check config persistence.
type HealthCheckConfigRepo interface {
	Create(ctx context.Context, config *model.HealthCheckConfig) error
	GetByServerID(ctx context.Context, serverID string) (*model.HealthCheckConfig, error)
	GetAllEnabled(ctx context.Context) ([]model.HealthCheckConfig, error)
	Update(ctx context.Context, config *model.HealthCheckConfig) error
	DisableByServerID(ctx context.Context, serverID string) error
}

type healthCheckConfigRepo struct {
	db *gorm.DB
}

// NewConfigRepo creates a new HealthCheckConfigRepo.
func NewConfigRepo(db *gorm.DB) HealthCheckConfigRepo {
	return &healthCheckConfigRepo{db: db}
}

// Create inserts a health-check config, or re-enables an existing one for the
// same server_id (e.g. when a soft-deleted server is re-imported). On conflict
// only is_enabled is restored so the previously seeded tcp_port / uptime_rate
// are preserved and the server keeps mapping to its TCP simulator port.
func (r *healthCheckConfigRepo) Create(ctx context.Context, config *model.HealthCheckConfig) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "server_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"is_enabled": true,
			"updated_at": gorm.Expr("NOW()"),
		}),
	}).Create(config).Error
}

func (r *healthCheckConfigRepo) GetByServerID(ctx context.Context, serverID string) (*model.HealthCheckConfig, error) {
	var cfg model.HealthCheckConfig
	err := r.db.WithContext(ctx).
		Where("server_id = ?", serverID).
		First(&cfg).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *healthCheckConfigRepo) GetAllEnabled(ctx context.Context) ([]model.HealthCheckConfig, error) {
	var configs []model.HealthCheckConfig
	err := r.db.WithContext(ctx).
		Where("is_enabled = ?", true).
		Find(&configs).Error
	return configs, err
}

func (r *healthCheckConfigRepo) Update(ctx context.Context, config *model.HealthCheckConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

func (r *healthCheckConfigRepo) DisableByServerID(ctx context.Context, serverID string) error {
	return r.db.WithContext(ctx).
		Model(&model.HealthCheckConfig{}).
		Where("server_id = ?", serverID).
		Update("is_enabled", false).Error
}
