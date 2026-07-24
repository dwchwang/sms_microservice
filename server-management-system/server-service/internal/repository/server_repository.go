package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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
	FindActiveTargets(ctx context.Context, cursor string, limit int) ([]model.Server, error)
	ApplyStatusEvent(ctx context.Context, u StatusUpdate) (int64, error)
	FindExistingNames(ctx context.Context, names []string) ([]string, error)
	InsertBatch(ctx context.Context, servers []model.Server) ([]string, error)
	InsertOne(ctx context.Context, server *model.Server) error
	CountByStatus(ctx context.Context) (map[string]int64, error)
	FindPopulation(ctx context.Context, q PopulationQuery) ([]model.Server, error)
}

// PopulationQuery selects the servers that existed during a report window.
type PopulationQuery struct {
	CreatedBefore time.Time
	DeletedAfter  time.Time
	Cursor        string
	Limit         int
}

// ActiveNameConstraint is the partial unique index on active server_name.
const ActiveNameConstraint = "ux_servers_active_name"

// IsUniqueViolation reports a PostgreSQL 23505 on the given constraint.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// StatusUpdate is a status change delivered by the monitoring stream.
type StatusUpdate struct {
	ServerID      string
	Status        string
	ChangedAt     time.Time
	StatusVersion int64
	StreamID      string
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
		// ipv4 is an inet column; LIKE needs a text form. host() drops any netmask.
		query = query.Where("host(ipv4) LIKE ?", filter.IPv4+"%")
	}
	if filter.TCPPort > 0 {
		query = query.Where("tcp_port = ?", filter.TCPPort)
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
		"ipv4": true, "tcp_port": true, "location": true,
		"created_at": true, "updated_at": true,
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

// CountByStatus returns how many active servers sit in each status.
func (r *serverRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	err := r.db.WithContext(ctx).
		Model(&model.Server{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Status] = row.Count
	}
	return out, nil
}

// FindPopulation lists servers alive during a window, cursored on server_id.
// Unscoped is required: the deleted_at test is explicit here, and GORM's soft
// delete filter would otherwise drop every server deleted during the window.
func (r *serverRepository) FindPopulation(ctx context.Context, q PopulationQuery) ([]model.Server, error) {
	var servers []model.Server
	err := r.db.WithContext(ctx).
		Unscoped().
		Model(&model.Server{}).
		Select("server_id", "server_name", "created_at", "deleted_at").
		Where("created_at < ?", q.CreatedBefore).
		Where("deleted_at IS NULL OR deleted_at > ?", q.DeletedAfter).
		Where("server_id > ?", q.Cursor).
		Order("server_id ASC").
		Limit(q.Limit).
		Find(&servers).Error
	return servers, err
}

// FindExistingNames returns which of the given names are taken by an active
// server. ON CONFLICT accepts only one target, so name clashes are filtered
// here and server_id clashes are left to ON CONFLICT (server_id).
func (r *serverRepository) FindExistingNames(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var existing []string
	err := r.db.WithContext(ctx).
		Model(&model.Server{}).
		Where("server_name IN ?", names).
		Pluck("server_name", &existing).Error
	return existing, err
}

const insertColumns = `(server_id, server_name, ipv4, tcp_port, os, cpu_cores, ram_gb, disk_gb, location, description, status)`

// server_id is globally unique (including soft-deleted rows), so an import cannot
// insert a second row for a known id. On conflict a soft-deleted row is revived
// with the imported values; an active row is left untouched — the WHERE makes it
// absent from RETURNING, i.e. reported as a duplicate.
const onConflictResurrect = `ON CONFLICT (server_id) DO UPDATE SET
	server_name = EXCLUDED.server_name,
	ipv4 = EXCLUDED.ipv4,
	tcp_port = EXCLUDED.tcp_port,
	os = EXCLUDED.os,
	cpu_cores = EXCLUDED.cpu_cores,
	ram_gb = EXCLUDED.ram_gb,
	disk_gb = EXCLUDED.disk_gb,
	location = EXCLUDED.location,
	description = EXCLUDED.description,
	status = EXCLUDED.status,
	status_version = 0,
	status_changed_at = NULL,
	last_status_event_id = NULL,
	deleted_at = NULL,
	updated_at = now()
WHERE servers.deleted_at IS NOT NULL
RETURNING server_id`

// insertArgs flattens one server into bind parameters. Zero-valued optional
// numbers become NULL, since the table only allows NULL or a positive value.
func insertArgs(s model.Server) []any {
	return []any{
		s.ServerID, s.ServerName, s.IPv4, s.TCPPort, nullIfEmpty(s.OS),
		s.CPUCores, s.RAMGB, s.DiskGB,
		nullIfEmpty(s.Location), nullIfEmpty(s.Description), s.Status,
	}
}

// InsertBatch inserts servers in one statement and returns the server_ids that
// were actually written. An id missing from the result was a duplicate.
func (r *serverRepository) InsertBatch(ctx context.Context, servers []model.Server) ([]string, error) {
	if len(servers) == 0 {
		return nil, nil
	}

	const colsPerRow = 11
	placeholders := make([]string, 0, len(servers))
	args := make([]any, 0, len(servers)*colsPerRow)
	for i, s := range servers {
		base := i * colsPerRow
		marks := make([]string, colsPerRow)
		for j := range marks {
			marks[j] = fmt.Sprintf("$%d", base+j+1)
		}
		placeholders = append(placeholders, "("+strings.Join(marks, ",")+")")
		args = append(args, insertArgs(s)...)
	}

	query := fmt.Sprintf(
		`INSERT INTO servers %s VALUES %s %s`,
		insertColumns, strings.Join(placeholders, ","), onConflictResurrect,
	)

	var inserted []string
	err := r.db.WithContext(ctx).Raw(query, args...).Scan(&inserted).Error
	return inserted, err
}

// InsertOne inserts a single server, used to retry a batch that hit a name clash.
// Returns gorm.ErrRecordNotFound when the server_id belongs to an active server
// (a soft-deleted one is revived instead).
func (r *serverRepository) InsertOne(ctx context.Context, server *model.Server) error {
	query := fmt.Sprintf(
		`INSERT INTO servers %s VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) %s`,
		insertColumns, onConflictResurrect,
	)

	var inserted []string
	if err := r.db.WithContext(ctx).Raw(query, insertArgs(*server)...).Scan(&inserted).Error; err != nil {
		return err
	}
	if len(inserted) == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// ApplyStatusEvent writes a status change, guarding on status_version so a
// duplicate or out-of-order event cannot overwrite newer data. Returns the
// number of rows changed; 0 means the event was stale or the server is gone.
func (r *serverRepository) ApplyStatusEvent(ctx context.Context, u StatusUpdate) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Server{}).
		Where("server_id = ? AND status_version < ?", u.ServerID, u.StatusVersion).
		Updates(map[string]any{
			"status":               u.Status,
			"status_changed_at":    u.ChangedAt,
			"status_version":       u.StatusVersion,
			"last_status_event_id": u.StreamID,
			"updated_at":           time.Now().UTC(),
		})
	return result.RowsAffected, result.Error
}

// FindActiveTargets pages through active servers ordered by server_id, using
// server_id as the cursor. An empty cursor starts from the beginning.
func (r *serverRepository) FindActiveTargets(ctx context.Context, cursor string, limit int) ([]model.Server, error) {
	var servers []model.Server
	query := r.db.WithContext(ctx).
		Select("server_id", "server_name", "ipv4", "tcp_port").
		Where("server_id > ?", cursor).
		Order("server_id ASC").
		Limit(limit)
	err := query.Find(&servers).Error
	return servers, err
}

// ExistsByServerID checks if a server with the given server_id already exists (including deleted).
func (r *serverRepository) ExistsByServerID(ctx context.Context, serverID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Unscoped().
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
