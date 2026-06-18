package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/server-service/internal/repository"
	"github.com/vcs-sms/shared/kafka"
	"gorm.io/gorm"
)

// ServerService defines the server management business logic interface.
type ServerService interface {
	CreateServer(ctx context.Context, req *dto.CreateServerRequest) (*dto.ServerResponse, error)
	GetServer(ctx context.Context, serverID string) (*dto.ServerResponse, error)
	ListServers(ctx context.Context, filter *dto.ServerFilter) (*dto.ListServerResponse, error)
	UpdateServer(ctx context.Context, serverID string, req *dto.UpdateServerRequest) (*dto.ServerResponse, error)
	DeleteServer(ctx context.Context, serverID string) error
}

// serverServiceImpl implements ServerService.
type serverServiceImpl struct {
	repo     repository.ServerRepository
	cache    serverCache
	producer kafka.Producer
}

type serverCache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
	ScanKeys(ctx context.Context, pattern string, count int64) ([]string, error)
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

func (r *redisServerCache) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

func (r *redisServerCache) ScanKeys(ctx context.Context, pattern string, count int64) ([]string, error) {
	iter := r.client.Scan(ctx, 0, pattern, count).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	return keys, iter.Err()
}

// NewServerService creates a new ServerService instance.
func NewServerService(repo repository.ServerRepository, rdb *redis.Client, prod kafka.Producer) ServerService {
	var cache serverCache
	if rdb != nil {
		cache = &redisServerCache{client: rdb}
	}
	return &serverServiceImpl{
		repo:     repo,
		cache:    cache,
		producer: prod,
	}
}

// CreateServer creates a new server and publishes a kafka event.
func (s *serverServiceImpl) CreateServer(ctx context.Context, req *dto.CreateServerRequest) (*dto.ServerResponse, error) {
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
	server := &model.Server{
		ServerID:    req.ServerID,
		ServerName:  req.ServerName,
		Status:      "off",
		IPv4:        req.IPv4,
		OS:          req.OS,
		CPUCores:    req.CPUCores,
		RAMGB:       req.RAMGB,
		DiskGB:      req.DiskGB,
		Location:    req.Location,
		Description: req.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// 3. Create in DB
	if err := s.repo.Create(ctx, server); err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	// 4. Invalidate cache
	s.invalidateCache(ctx, req.ServerID)

	// 5. Publish Kafka event (fire-and-forget)
	s.publishEvent("server.created", req.ServerID, server)

	return mapServerToResponse(server), nil
}

// GetServer retrieves a server by ID, with Redis cache.
func (s *serverServiceImpl) GetServer(ctx context.Context, serverID string) (*dto.ServerResponse, error) {
	// 1. Check Redis cache
	cacheKey := fmt.Sprintf("server:detail:%s", serverID)
	if s.cache != nil {
		cached, err := s.cache.Get(ctx, cacheKey)
		if err == nil {
			var resp dto.ServerResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
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

	// 3. Cache result
	resp := mapServerToResponse(server)
	if s.cache != nil {
		data, _ := json.Marshal(resp)
		_ = s.cache.Set(ctx, cacheKey, data, 5*time.Minute)
	}

	return resp, nil
}

// ListServers retrieves servers with filtering and pagination, with Redis cache.
func (s *serverServiceImpl) ListServers(ctx context.Context, filter *dto.ServerFilter) (*dto.ListServerResponse, error) {
	// 1. Build cache key from filter params
	cacheKey := buildListCacheKey(filter)

	// 2. Check Redis cache
	if s.cache != nil {
		cached, err := s.cache.Get(ctx, cacheKey)
		if err == nil {
			var resp dto.ListServerResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
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

	// 5. Cache result
	if s.cache != nil {
		data, _ := json.Marshal(resp)
		_ = s.cache.Set(ctx, cacheKey, data, 2*time.Minute)
	}

	return resp, nil
}

// UpdateServer modifies an existing server.
func (s *serverServiceImpl) UpdateServer(ctx context.Context, serverID string, req *dto.UpdateServerRequest) (*dto.ServerResponse, error) {
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

	// 4. Invalidate cache
	s.invalidateCache(ctx, serverID)

	// 5. Publish Kafka event
	s.publishEvent("server.updated", serverID, server)

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

	// 3. Invalidate cache
	s.invalidateCache(ctx, serverID)

	// 4. Publish Kafka event
	s.publishEvent("server.deleted", serverID, map[string]string{"server_id": serverID})

	return nil
}

// invalidateCache removes related Redis cache entries.
func (s *serverServiceImpl) invalidateCache(ctx context.Context, serverID string) {
	if s.cache == nil {
		return
	}
	_ = s.cache.Del(ctx, fmt.Sprintf("server:detail:%s", serverID))

	// Delete all list caches (SCAN pattern)
	keys, err := s.cache.ScanKeys(ctx, "servers:list:*", 100)
	if err != nil {
		return
	}
	for _, key := range keys {
		_ = s.cache.Del(ctx, key)
	}
}

// publishEvent sends an event to Kafka (non-blocking, errors logged).
func (s *serverServiceImpl) publishEvent(eventType, serverID string, data interface{}) {
	event := &kafka.Event{
		EventID:   uuid.New().String(),
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "server-service",
		Data:      data,
	}
	if err := s.producer.Publish(context.Background(), eventType, serverID, event); err != nil {
		// Log but don't block the request
		fmt.Printf("[Kafka] Failed to publish %s: %v\n", eventType, err)
	}
}

// mapServerToResponse converts a model.Server to a dto.ServerResponse.
func mapServerToResponse(s *model.Server) *dto.ServerResponse {
	return &dto.ServerResponse{
		ServerID:    s.ServerID,
		ServerName:  s.ServerName,
		Status:      s.Status,
		IPv4:        s.IPv4,
		OS:          s.OS,
		CPUCores:    s.CPUCores,
		RAMGB:       s.RAMGB,
		DiskGB:      s.DiskGB,
		Location:    s.Location,
		Description: s.Description,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

// buildListCacheKey creates a deterministic cache key from filter parameters.
func buildListCacheKey(f *dto.ServerFilter) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%d|%d",
		f.Status, f.ServerID, f.ServerName, f.IPv4, f.OS, f.Location,
		f.SortBy, f.SortOrder, f.Page, f.PageSize,
	)
	return fmt.Sprintf("servers:list:%x", sha256.Sum256([]byte(data)))
}
