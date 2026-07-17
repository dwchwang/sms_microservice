package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/cache"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/server-service/internal/projection"
	"github.com/vcs-sms/server-service/internal/repository"
	"github.com/vcs-sms/server-service/internal/status"
	"github.com/vcs-sms/server-service/internal/validator"
	"gorm.io/gorm"
)

// ServerService defines the server management business logic interface.
type ServerService interface {
	CreateServer(ctx context.Context, req *dto.CreateServerRequest) (*dto.ServerResponse, error)
	GetServer(ctx context.Context, serverID string) (*dto.ServerResponse, error)
	ListServers(ctx context.Context, filter *dto.ServerFilter) (*dto.ListServerResponse, error)
	UpdateServer(ctx context.Context, serverID string, req *dto.UpdateServerRequest) (*dto.ServerResponse, error)
	DeleteServer(ctx context.Context, serverID string) error
	GetStats(ctx context.Context) (*dto.StatsResponse, error)
	GetUptime(ctx context.Context) (*dto.UptimeResponse, error)
}

const (
	// cacheTTL is a second layer behind list_version invalidation.
	cacheTTL = 30 * time.Second

	statsCacheKey = "server:stats:cache"
	statsCacheTTL = 10 * time.Second

	uptimeCacheKey  = "server:uptime:cache"
	uptimeCacheTTL  = 10 * time.Second
	topLowestUptime = 10
)

// serverServiceImpl implements ServerService.
type serverServiceImpl struct {
	repo      repository.ServerRepository
	cache     serverCache
	cidr      *validator.CIDRValidator
	targets   projection.TargetProjection
	lastCheck status.LastCheckReader
	uptime    status.UptimeReader
	log       zerolog.Logger
}

type serverCache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Incr(ctx context.Context, key string) error
}

type redisServerCache struct {
	client *redis.Client
}

func (r *redisServerCache) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *redisServerCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *redisServerCache) Incr(ctx context.Context, key string) error {
	return r.client.Incr(ctx, key).Err()
}

// NewServerService creates a new ServerService instance.
func NewServerService(
	repo repository.ServerRepository,
	rdb *redis.Client,
	cidr *validator.CIDRValidator,
	targets projection.TargetProjection,
	lastCheck status.LastCheckReader,
	uptime status.UptimeReader,
	log zerolog.Logger,
) ServerService {
	var cache serverCache
	if rdb != nil {
		cache = &redisServerCache{client: rdb}
	}
	return &serverServiceImpl{
		repo:      repo,
		cache:     cache,
		cidr:      cidr,
		targets:   targets,
		lastCheck: lastCheck,
		uptime:    uptime,
		log:       log,
	}
}

// CreateServer creates a new server.
func (s *serverServiceImpl) CreateServer(ctx context.Context, req *dto.CreateServerRequest) (*dto.ServerResponse, error) {
	if err := s.cidr.Validate(req.IPv4); err != nil {
		return nil, err
	}

	// 1. Validate — check uniqueness
	exists, err := s.repo.ExistsByServerID(ctx, req.ServerID)
	if err != nil {
		return nil, fmt.Errorf("failed to check server_id: %w", err)
	}
	if exists {
		return nil, ErrDuplicateServerID
	}

	exists, err = s.repo.ExistsByServerName(ctx, req.ServerName)
	if err != nil {
		return nil, fmt.Errorf("failed to check server_name: %w", err)
	}
	if exists {
		return nil, ErrDuplicateServerName
	}

	// 2. Build model
	now := time.Now().UTC()
	tcpPort := req.TCPPort
	if tcpPort == 0 {
		tcpPort = 80 // Default port
	}

	server := &model.Server{
		ServerID:    req.ServerID,
		ServerName:  req.ServerName,
		Status:      "UNKNOWN",
		IPv4:        req.IPv4,
		TCPPort:     tcpPort,
		OS:          req.OS,
		CPUCores:    optionalInt(req.CPUCores),
		RAMGB:       optionalInt(req.RAMGB),
		DiskGB:      optionalInt(req.DiskGB),
		Location:    req.Location,
		Description: req.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// 3. Create in DB
	if err := s.repo.Create(ctx, server); err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	// 4. Sync monitoring projection and invalidate cache
	s.syncTarget(ctx, server)
	s.bumpListVersion(ctx)

	return mapServerToResponse(server), nil
}

// GetServer retrieves a server by ID, with Redis cache.
func (s *serverServiceImpl) GetServer(ctx context.Context, serverID string) (*dto.ServerResponse, error) {
	// 1. Check Redis cache
	cacheKey := buildDetailCacheKey(serverID, s.getListVersion(ctx))
	if s.cache != nil {
		cached, err := s.cache.Get(ctx, cacheKey)
		if err == nil {
			var resp dto.ServerResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				single := []dto.ServerResponse{resp}
				s.enrichLastCheck(ctx, single)
				return &single[0], nil
			}
		}
	}

	// 2. Query DB
	server, err := s.repo.FindByServerID(ctx, serverID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("failed to get server: %w", err)
	}

	// 3. Cache result, then enrich so last_status_check stays out of the cache
	resp := mapServerToResponse(server)
	if s.cache != nil {
		data, _ := json.Marshal(resp)
		_ = s.cache.Set(ctx, cacheKey, data, cacheTTL)
	}

	single := []dto.ServerResponse{*resp}
	s.enrichLastCheck(ctx, single)
	return &single[0], nil
}

// ListServers retrieves servers with filtering and pagination, with Redis cache.
func (s *serverServiceImpl) ListServers(ctx context.Context, filter *dto.ServerFilter) (*dto.ListServerResponse, error) {
	// 1. Get list version and build cache key
	version := s.getListVersion(ctx)
	cacheKey := buildListCacheKey(filter, version)

	// 2. Check Redis cache
	if s.cache != nil {
		cached, err := s.cache.Get(ctx, cacheKey)
		if err == nil {
			var resp dto.ListServerResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				s.enrichLastCheck(ctx, resp.Servers)
				return &resp, nil
			}
		}
	}

	// 3. Query DB
	servers, total, err := s.repo.FindAll(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}

	// 4. Build response
	serverResponses := make([]dto.ServerResponse, 0, len(servers))
	for i := range servers {
		serverResponses = append(serverResponses, *mapServerToResponse(&servers[i]))
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	resp := &dto.ListServerResponse{
		Servers:    serverResponses,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	// 5. Cache result, then enrich so last_status_check stays out of the cache
	if s.cache != nil {
		data, _ := json.Marshal(resp)
		_ = s.cache.Set(ctx, cacheKey, data, cacheTTL)
	}

	s.enrichLastCheck(ctx, resp.Servers)
	return resp, nil
}

// UpdateServer modifies an existing server.
func (s *serverServiceImpl) UpdateServer(ctx context.Context, serverID string, req *dto.UpdateServerRequest) (*dto.ServerResponse, error) {
	if req.IPv4 != nil {
		if err := s.cidr.Validate(*req.IPv4); err != nil {
			return nil, err
		}
	}

	// 1. Find existing server
	server, err := s.repo.FindByServerID(ctx, serverID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("failed to find server: %w", err)
	}

	// 2. Apply partial updates (only update non-nil fields)
	if req.ServerName != nil {
		if *req.ServerName != server.ServerName {
			exists, _ := s.repo.ExistsByServerNameExclude(ctx, *req.ServerName, serverID)
			if exists {
				return nil, ErrDuplicateServerName
			}
			server.ServerName = *req.ServerName
		}
	}
	if req.IPv4 != nil {
		server.IPv4 = *req.IPv4
	}
	if req.TCPPort != nil {
		server.TCPPort = *req.TCPPort
	}
	if req.OS != nil {
		server.OS = *req.OS
	}
	if req.CPUCores != nil {
		server.CPUCores = req.CPUCores
	}
	if req.RAMGB != nil {
		server.RAMGB = req.RAMGB
	}
	if req.DiskGB != nil {
		server.DiskGB = req.DiskGB
	}
	if req.Location != nil {
		server.Location = *req.Location
	}
	if req.Description != nil {
		server.Description = *req.Description
	}
	server.UpdatedAt = time.Now().UTC()

	// 3. Save to DB
	if err := s.repo.Update(ctx, server); err != nil {
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	// 4. Sync monitoring projection and invalidate cache
	s.syncTarget(ctx, server)
	s.bumpListVersion(ctx)

	return mapServerToResponse(server), nil
}

// DeleteServer soft-deletes a server.
func (s *serverServiceImpl) DeleteServer(ctx context.Context, serverID string) error {
	// 1. Find server (verify existence)
	_, err := s.repo.FindByServerID(ctx, serverID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrServerNotFound
		}
		return fmt.Errorf("failed to find server: %w", err)
	}

	// 2. Soft delete
	if err := s.repo.Delete(ctx, serverID); err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}

	// 3. Remove from monitoring projection and invalidate cache
	s.removeTarget(ctx, serverID)
	s.bumpListVersion(ctx)

	return nil
}

// bumpListVersion increments the cache version for list queries.
func (s *serverServiceImpl) bumpListVersion(ctx context.Context) {
	if s.cache != nil {
		_ = s.cache.Incr(ctx, cache.ListVersionKey)
	}
}

// getListVersion gets the current cache version for list queries.
func (s *serverServiceImpl) getListVersion(ctx context.Context) string {
	if s.cache != nil {
		ver, err := s.cache.Get(ctx, cache.ListVersionKey)
		if err == nil {
			return ver
		}
	}
	return "0"
}

// GetStats returns the live status breakdown, cached briefly since a dashboard
// polls it far more often than statuses actually change.
func (s *serverServiceImpl) GetStats(ctx context.Context) (*dto.StatsResponse, error) {
	if s.cache != nil {
		if cached, err := s.cache.Get(ctx, statsCacheKey); err == nil {
			var resp dto.StatsResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	counts, err := s.repo.CountByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count servers by status: %w", err)
	}

	resp := &dto.StatsResponse{
		On:      counts["ON"],
		Off:     counts["OFF"],
		Unknown: counts["UNKNOWN"],
	}
	resp.Total = resp.On + resp.Off + resp.Unknown

	if s.cache != nil {
		data, _ := json.Marshal(resp)
		_ = s.cache.Set(ctx, statsCacheKey, data, statsCacheTTL)
	}
	return resp, nil
}

// GetUptime builds the dashboard's uptime picture from the lifetime counters
// Monitoring keeps in Redis. It reads no snapshot and no Elasticsearch, so it
// answers at any moment, including for a fleet that started five minutes ago.
func (s *serverServiceImpl) GetUptime(ctx context.Context) (*dto.UptimeResponse, error) {
	if s.cache != nil {
		if cached, err := s.cache.Get(ctx, uptimeCacheKey); err == nil {
			var resp dto.UptimeResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	stats, err := s.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	resp := &dto.UptimeResponse{
		TotalServers:      stats.Total,
		ServersOn:         stats.On,
		ServersOff:        stats.Off,
		ServersUnknown:    stats.Unknown,
		Top10LowestUptime: []status.ServerUptime{},
	}

	if s.uptime == nil {
		return resp, nil
	}

	up, err := s.uptime.Stats(ctx, topLowestUptime)
	if err != nil {
		return nil, fmt.Errorf("failed to read uptime counters: %w", err)
	}

	resp.ServersUptime100 = up.Full
	resp.ServersUptimePartial = up.Partial
	resp.ServersUptime0 = up.Zero
	resp.AvgUptimePct = up.AvgPct
	// Servers the index has never scored: counted, never guessed at.
	resp.ServersNoData = max(stats.Total-up.Measured, 0)
	if len(up.Worst) > 0 {
		resp.Top10LowestUptime = up.Worst
		s.fillWorstNames(ctx, resp.Top10LowestUptime)
	}

	if s.cache != nil {
		data, _ := json.Marshal(resp)
		_ = s.cache.Set(ctx, uptimeCacheKey, data, uptimeCacheTTL)
	}
	return resp, nil
}

// fillWorstNames reads names from PostgreSQL, the owner of server_name.
func (s *serverServiceImpl) fillWorstNames(ctx context.Context, worst []status.ServerUptime) {
	for i := range worst {
		srv, err := s.repo.FindByServerID(ctx, worst[i].ServerID)
		if err != nil || srv == nil {
			continue
		}
		worst[i].ServerName = srv.ServerName
	}
}

// enrichLastCheck fills last_status_check from Redis. It runs outside the cache
// on purpose: the value changes every round, so caching it would either serve
// stale data or force the cache to be invalidated every 60 seconds.
func (s *serverServiceImpl) enrichLastCheck(ctx context.Context, servers []dto.ServerResponse) {
	if s.lastCheck == nil || len(servers) == 0 {
		return
	}
	ids := make([]string, len(servers))
	for i := range servers {
		ids[i] = servers[i].ServerID
	}
	checked := s.lastCheck.LastCheckedAt(ctx, ids)
	for i := range servers {
		if ts, ok := checked[servers[i].ServerID]; ok {
			servers[i].LastStatusCheck = &ts
		}
	}
}

// syncTarget updates the monitoring projection. PostgreSQL is the source of
// truth, so a Redis failure is logged for reconciliation, not returned.
func (s *serverServiceImpl) syncTarget(ctx context.Context, server *model.Server) {
	if s.targets == nil {
		return
	}
	target := projection.Target{
		ServerID:   server.ServerID,
		ServerName: server.ServerName,
		IPv4:       server.IPv4,
		TCPPort:    server.TCPPort,
	}
	if err := s.targets.Sync(ctx, target); err != nil {
		s.log.Error().Err(err).Str("server_id", server.ServerID).
			Msg("Failed to sync monitor target projection")
	}
}

// removeTarget drops the server from the monitoring projection.
func (s *serverServiceImpl) removeTarget(ctx context.Context, serverID string) {
	if s.targets == nil {
		return
	}
	if err := s.targets.Delete(ctx, serverID); err != nil {
		s.log.Error().Err(err).Str("server_id", serverID).
			Msg("Failed to remove monitor target projection")
	}
}

// optionalInt maps an absent (zero) request value onto a NULL column.
func optionalInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// derefInt renders a NULL column as 0, which the response omits.
func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// buildDetailCacheKey creates a versioned cache key for a single server.
func buildDetailCacheKey(serverID, version string) string {
	return fmt.Sprintf("server:detail:cache:%s:%s", serverID, version)
}

// mapServerToResponse converts a model.Server to a dto.ServerResponse.
func mapServerToResponse(s *model.Server) *dto.ServerResponse {
	return &dto.ServerResponse{
		ServerID:        s.ServerID,
		ServerName:      s.ServerName,
		Status:          s.Status,
		StatusChangedAt: s.StatusChangedAt,
		IPv4:            s.IPv4,
		TCPPort:         s.TCPPort,
		OS:              s.OS,
		CPUCores:        derefInt(s.CPUCores),
		RAMGB:           derefInt(s.RAMGB),
		DiskGB:          derefInt(s.DiskGB),
		Location:        s.Location,
		Description:     s.Description,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

// buildListCacheKey creates a deterministic cache key from filter parameters and cache version.
func buildListCacheKey(f *dto.ServerFilter, version string) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%d|%d",
		f.Status, f.ServerID, f.ServerName, f.IPv4, f.OS, f.Location,
		f.SortBy, f.SortOrder, f.Page, f.PageSize,
	)
	return fmt.Sprintf("server:list:cache:%x:%s", sha256.Sum256([]byte(data)), version)
}
