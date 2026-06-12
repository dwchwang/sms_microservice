package repository

import (
	"context"
	"fmt"

	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/model"
	"gorm.io/gorm"
)

// ServerWriter defines the interface for cross-schema server access.
type ServerWriter interface {
	// FindByServerIDOrName checks if a server with the given ID or name exists.
	FindByServerIDOrName(ctx context.Context, serverID, serverName string) (*model.Server, error)

	// Create inserts a new server into server_schema.servers.
	Create(ctx context.Context, server *model.Server) error

	// FindAllWithFilter retrieves servers matching the filter (for export).
	FindAllWithFilter(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error)
}

// serverWriter implements ServerWriter using GORM.
type serverWriter struct {
	db *gorm.DB
}

// NewServerWriter creates a new ServerWriter instance.
// db should be connected to the database with access to server_schema.
func NewServerWriter(db *gorm.DB) ServerWriter {
	return &serverWriter{db: db}
}

// FindByServerIDOrName checks if a server with the given ID or name already exists.
// Returns the existing server if found, or gorm.ErrRecordNotFound if not.
func (r *serverWriter) FindByServerIDOrName(ctx context.Context, serverID, serverName string) (*model.Server, error) {
	var server model.Server
	err := r.db.WithContext(ctx).
		Where("server_id = ? OR server_name = ?", serverID, serverName).
		First(&server).Error
	if err != nil {
		return nil, err
	}
	return &server, nil
}

// Create inserts a new server into server_schema.servers.
func (r *serverWriter) Create(ctx context.Context, server *model.Server) error {
	return r.db.WithContext(ctx).Create(server).Error
}

// FindAllWithFilter retrieves all servers matching the filter, sorted.
// Used for export — no pagination.
func (r *serverWriter) FindAllWithFilter(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error) {
	var servers []model.Server

	query := r.db.WithContext(ctx).Model(&model.Server{})

	// Apply filters
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.ServerName != "" {
		query = query.Where("server_name ILIKE ?", "%"+filter.ServerName+"%")
	}
	if filter.IPv4 != "" {
		query = query.Where("ipv4 LIKE ?", filter.IPv4+"%")
	}
	if filter.Location != "" {
		query = query.Where("location ILIKE ?", "%"+filter.Location+"%")
	}
	if filter.OS != "" {
		query = query.Where("os ILIKE ?", "%"+filter.OS+"%")
	}

	// Apply sorting — whitelist column names for safety
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

	// Limit export to prevent memory issues
	query = query.Limit(50000)

	err := query.Find(&servers).Error
	return servers, err
}

// Ensure interface compliance.
var _ ServerWriter = (*serverWriter)(nil)
