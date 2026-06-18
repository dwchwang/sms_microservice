package repository

import (
	"context"
	"fmt"

	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/model"
	"gorm.io/gorm"
)

// ServerRepository defines the interface for server data access.
type ServerRepository interface {
	Create(ctx context.Context, server *model.Server) error
	FindByServerID(ctx context.Context, serverID string) (*model.Server, error)
	FindAll(ctx context.Context, filter *dto.ServerFilter) ([]model.Server, int64, error)
	Update(ctx context.Context, server *model.Server) error
	Delete(ctx context.Context, serverID string) error
	ExistsByServerID(ctx context.Context, serverID string) (bool, error)
	ExistsByServerName(ctx context.Context, serverName string) (bool, error)
	ExistsByServerNameExclude(ctx context.Context, serverName string, excludeID string) (bool, error)
}

// serverRepository implements ServerRepository using GORM.
type serverRepository struct {
	db *gorm.DB
}

// NewServerRepository creates a new ServerRepository instance.
func NewServerRepository(db *gorm.DB) ServerRepository {
	return &serverRepository{db: db}
}

// Create inserts a new server into the database.
func (r *serverRepository) Create(ctx context.Context, server *model.Server) error {
	return r.db.WithContext(ctx).Create(server).Error
}

// FindByServerID retrieves a server by its business ID (not UUID).
func (r *serverRepository) FindByServerID(ctx context.Context, serverID string) (*model.Server, error) {
	var server model.Server
	err := r.db.WithContext(ctx).
		Where("server_id = ?", serverID).
		First(&server).Error
	if err != nil {
		return nil, err
	}
	return &server, nil
}

// FindAll retrieves servers with filtering, sorting, and pagination.
// All WHERE values are parameterized to prevent SQL injection.
func (r *serverRepository) FindAll(ctx context.Context, filter *dto.ServerFilter) ([]model.Server, int64, error) {
	var servers []model.Server
	var total int64

	query := r.db.WithContext(ctx).Model(&model.Server{})

	// Apply filters — GORM parameterizes automatically
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.ServerID != "" {
		query = query.Where("server_id ILIKE ?", "%"+filter.ServerID+"%")
	}
	if filter.ServerName != "" {
		query = query.Where("server_name ILIKE ?", "%"+filter.ServerName+"%")
	}
	if filter.IPv4 != "" {
		query = query.Where("ipv4 LIKE ?", filter.IPv4+"%")
	}
	if filter.OS != "" {
		query = query.Where("os ILIKE ?", "%"+filter.OS+"%")
	}
	if filter.Location != "" {
		query = query.Where("location ILIKE ?", "%"+filter.Location+"%")
	}

	// Count total (before pagination)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting — column name is whitelisted, safe from injection
	sortBy := "created_at"
	sortOrder := "DESC"
	allowedSortFields := map[string]bool{
		"server_id": true, "server_name": true, "status": true,
		"ipv4": true, "location": true, "created_at": true, "updated_at": true,
	}
	if filter.SortBy != "" && allowedSortFields[filter.SortBy] {
		sortBy = filter.SortBy
	}
	if filter.SortOrder == "asc" {
		sortOrder = "ASC"
	}
	query = query.Order(fmt.Sprintf("%s %s", sortBy, sortOrder))

	// Apply pagination
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize
	query = query.Offset(offset).Limit(pageSize)

	// Execute
	err := query.Find(&servers).Error
	return servers, total, err
}

// Update saves changes to an existing server.
func (r *serverRepository) Update(ctx context.Context, server *model.Server) error {
	return r.db.WithContext(ctx).Save(server).Error
}

// Delete performs a soft delete on a server by its business server_id.
func (r *serverRepository) Delete(ctx context.Context, serverID string) error {
	return r.db.WithContext(ctx).
		Where("server_id = ?", serverID).
		Delete(&model.Server{}).Error
}

// ExistsByServerID checks if a server with the given server_id already exists.
func (r *serverRepository) ExistsByServerID(ctx context.Context, serverID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Server{}).
		Where("server_id = ?", serverID).
		Count(&count).Error
	return count > 0, err
}

// ExistsByServerName checks if a server with the given name already exists.
func (r *serverRepository) ExistsByServerName(ctx context.Context, serverName string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Server{}).
		Where("server_name = ?", serverName).
		Count(&count).Error
	return count > 0, err
}

// ExistsByServerNameExclude checks if a server with the given name exists, excluding a specific server_id.
func (r *serverRepository) ExistsByServerNameExclude(ctx context.Context, serverName string, excludeID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Server{}).
		Where("server_name = ? AND server_id != ?", serverName, excludeID).
		Count(&count).Error
	return count > 0, err
}
