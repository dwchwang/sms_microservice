package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/shared/kafka"
	"gorm.io/gorm"
)

// ── Mock Repository ──

type mockServerRepo struct {
	servers                map[string]*model.Server // key = server_id
	createShouldFail       bool
	findShouldFail         bool
	findAllShouldFail      bool
	updateShouldFail       bool
	deleteShouldFail       bool
	existsByServerIDErr    error
	existsByServerNameErr  error
	existsByServerID       map[string]bool
	existsByServerName     map[string]bool
	existsByNameExcludeErr error
}

type fakeServerCache struct {
	values      map[string]string
	deletedKeys []string
	scanKeys    []string
	getErr      error
	scanErr     error
}

func newFakeServerCache() *fakeServerCache {
	return &fakeServerCache{values: make(map[string]string)}
}

func (c *fakeServerCache) Get(ctx context.Context, key string) (string, error) {
	if c.getErr != nil {
		return "", c.getErr
	}
	v, ok := c.values[key]
	if !ok {
		return "", errors.New("cache miss")
	}
	return v, nil
}

func (c *fakeServerCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	c.values[key] = string(value.([]byte))
	return nil
}

func (c *fakeServerCache) Del(ctx context.Context, keys ...string) error {
	c.deletedKeys = append(c.deletedKeys, keys...)
	for _, key := range keys {
		delete(c.values, key)
	}
	return nil
}

func (c *fakeServerCache) ScanKeys(ctx context.Context, pattern string, count int64) ([]string, error) {
	if c.scanErr != nil {
		return nil, c.scanErr
	}
	return c.scanKeys, nil
}

func newMockServerRepo() *mockServerRepo {
	return &mockServerRepo{
		servers:            make(map[string]*model.Server),
		existsByServerID:   make(map[string]bool),
		existsByServerName: make(map[string]bool),
	}
}

func (m *mockServerRepo) addServer(s *model.Server) {
	m.servers[s.ServerID] = s
	m.existsByServerID[s.ServerID] = true
	m.existsByServerName[s.ServerName] = true
}

func (m *mockServerRepo) Create(ctx context.Context, s *model.Server) error {
	if m.createShouldFail {
		return errors.New("db error")
	}
	m.addServer(s)
	return nil
}

func (m *mockServerRepo) FindByServerID(ctx context.Context, serverID string) (*model.Server, error) {
	if m.findShouldFail {
		return nil, errors.New("find failed")
	}
	s, ok := m.servers[serverID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return s, nil
}

func (m *mockServerRepo) FindAll(ctx context.Context, filter *dto.ServerFilter) ([]model.Server, int64, error) {
	if m.findAllShouldFail {
		return nil, 0, errors.New("list failed")
	}
	var result []model.Server
	for _, s := range m.servers {
		if filter.Status != "" && s.Status != filter.Status {
			continue
		}
		result = append(result, *s)
	}
	return result, int64(len(result)), nil
}

func (m *mockServerRepo) Update(ctx context.Context, s *model.Server) error {
	if m.updateShouldFail {
		return errors.New("update failed")
	}
	m.servers[s.ServerID] = s
	return nil
}

func (m *mockServerRepo) Delete(ctx context.Context, serverID string) error {
	if m.deleteShouldFail {
		return errors.New("delete failed")
	}
	_, ok := m.servers[serverID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	delete(m.servers, serverID)
	delete(m.existsByServerID, serverID)
	return nil
}

func (m *mockServerRepo) ExistsByServerID(ctx context.Context, serverID string) (bool, error) {
	if m.existsByServerIDErr != nil {
		return false, m.existsByServerIDErr
	}
	return m.existsByServerID[serverID], nil
}

func (m *mockServerRepo) ExistsByServerName(ctx context.Context, serverName string) (bool, error) {
	if m.existsByServerNameErr != nil {
		return false, m.existsByServerNameErr
	}
	return m.existsByServerName[serverName], nil
}

func (m *mockServerRepo) ExistsByServerNameExclude(ctx context.Context, serverName string, excludeID string) (bool, error) {
	if m.existsByNameExcludeErr != nil {
		return false, m.existsByNameExcludeErr
	}
	for id, s := range m.servers {
		if s.ServerName == serverName && id != excludeID {
			return true, nil
		}
	}
	return false, nil
}

// ── Test Helper ──

func newTestServerService() (ServerService, *mockServerRepo) {
	repo := newMockServerRepo()
	producer := kafka.NewDummyProducer(zerolog.Logger{})
	svc := NewServerService(repo, nil, producer)
	return svc, repo
}

func newTestServerServiceWithCache(cache serverCache) (ServerService, *mockServerRepo) {
	repo := newMockServerRepo()
	producer := kafka.NewDummyProducer(zerolog.Logger{})
	svc := &serverServiceImpl{repo: repo, cache: cache, producer: producer}
	return svc, repo
}

func makeServer(id, name, ip string) *model.Server {
	now := time.Now().UTC()
	return &model.Server{
		ServerID:   id,
		ServerName: name,
		Status:     "off",
		IPv4:       ip,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// ── Create Tests ──

func TestCreateServer_Success(t *testing.T) {
	svc, _ := newTestServerService()

	req := &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "web-server-01",
		IPv4:       "192.168.1.100",
		OS:         "Ubuntu 22.04",
	}

	resp, err := svc.CreateServer(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if resp.ServerID != "SRV-001" {
		t.Errorf("expected ServerID 'SRV-001', got '%s'", resp.ServerID)
	}
	if resp.Status != "off" {
		t.Errorf("expected Status 'off', got '%s'", resp.Status)
	}
}

func TestCreateServer_DuplicateServerID(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "existing", "10.0.0.1"))

	req := &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "new-server",
		IPv4:       "10.0.0.2",
	}

	_, err := svc.CreateServer(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for duplicate server_id")
	}
}

func TestCreateServer_DuplicateServerName(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "my-server", "10.0.0.1"))

	req := &dto.CreateServerRequest{
		ServerID:   "SRV-002",
		ServerName: "my-server",
		IPv4:       "10.0.0.2",
	}

	_, err := svc.CreateServer(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for duplicate server_name")
	}
}

func TestCreateServer_ExistsByServerIDError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.existsByServerIDErr = errors.New("lookup failed")

	_, err := svc.CreateServer(context.Background(), &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "server-a",
		IPv4:       "10.0.0.1",
	})
	if err == nil {
		t.Fatal("expected server_id lookup error")
	}
}

func TestCreateServer_ExistsByServerNameError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.existsByServerNameErr = errors.New("lookup failed")

	_, err := svc.CreateServer(context.Background(), &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "server-a",
		IPv4:       "10.0.0.1",
	})
	if err == nil {
		t.Fatal("expected server_name lookup error")
	}
}

func TestCreateServer_CreateError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.createShouldFail = true

	_, err := svc.CreateServer(context.Background(), &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "server-a",
		IPv4:       "10.0.0.1",
	})
	if err == nil {
		t.Fatal("expected create error")
	}
}

// ── Get Tests ──

func TestGetServer_Success(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "test-server", "10.0.0.1"))

	resp, err := svc.GetServer(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if resp.ServerName != "test-server" {
		t.Errorf("expected 'test-server', got '%s'", resp.ServerName)
	}
}

func TestGetServer_ReturnsCachedResponse(t *testing.T) {
	cache := newFakeServerCache()
	cached := dto.ServerResponse{ServerID: "SRV-001", ServerName: "cached", Status: "on", IPv4: "10.0.0.1"}
	data, _ := json.Marshal(cached)
	cache.values["server:detail:SRV-001"] = string(data)
	svc, repo := newTestServerServiceWithCache(cache)
	repo.findShouldFail = true

	resp, err := svc.GetServer(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if resp.ServerName != "cached" {
		t.Fatalf("expected cached response, got %#v", resp)
	}
}

func TestGetServer_IgnoresMalformedCacheAndStoresResult(t *testing.T) {
	cache := newFakeServerCache()
	cache.values["server:detail:SRV-001"] = "{bad json"
	svc, repo := newTestServerServiceWithCache(cache)
	repo.addServer(makeServer("SRV-001", "from-db", "10.0.0.1"))

	resp, err := svc.GetServer(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if resp.ServerName != "from-db" {
		t.Fatalf("expected DB response, got %#v", resp)
	}
	if cache.values["server:detail:SRV-001"] == "{bad json" {
		t.Fatal("expected malformed cache to be replaced")
	}
}

func TestGetServer_NotFound(t *testing.T) {
	svc, _ := newTestServerService()

	_, err := svc.GetServer(context.Background(), "NONEXIST")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestGetServer_RepositoryError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.findShouldFail = true

	_, err := svc.GetServer(context.Background(), "SRV-001")
	if err == nil {
		t.Fatal("expected repository error")
	}
}

// ── List Tests ──

func TestListServers_NoFilter(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "server-a", "10.0.0.1"))
	repo.addServer(makeServer("SRV-002", "server-b", "10.0.0.2"))

	resp, err := svc.ListServers(context.Background(), &dto.ServerFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 servers, got %d", resp.Total)
	}
}

func TestListServers_ReturnsCachedResponse(t *testing.T) {
	cache := newFakeServerCache()
	filter := &dto.ServerFilter{Page: 1, PageSize: 20}
	cached := dto.ListServerResponse{Servers: []dto.ServerResponse{{ServerID: "SRV-001", ServerName: "cached"}}, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}
	data, _ := json.Marshal(cached)
	cache.values[buildListCacheKey(filter)] = string(data)
	svc, repo := newTestServerServiceWithCache(cache)
	repo.findAllShouldFail = true

	resp, err := svc.ListServers(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if resp.Servers[0].ServerName != "cached" {
		t.Fatalf("expected cached list, got %#v", resp)
	}
}

func TestListServers_IgnoresMalformedCacheAndStoresResult(t *testing.T) {
	cache := newFakeServerCache()
	filter := &dto.ServerFilter{Page: 1, PageSize: 20}
	cache.values[buildListCacheKey(filter)] = "{bad json"
	svc, repo := newTestServerServiceWithCache(cache)
	repo.addServer(makeServer("SRV-001", "from-db", "10.0.0.1"))

	resp, err := svc.ListServers(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if resp.Total != 1 || resp.Servers[0].ServerName != "from-db" {
		t.Fatalf("expected DB list, got %#v", resp)
	}
	if cache.values[buildListCacheKey(filter)] == "{bad json" {
		t.Fatal("expected malformed cache to be replaced")
	}
}

func TestListServers_Pagination(t *testing.T) {
	svc, repo := newTestServerService()
	for i := 0; i < 15; i++ {
		repo.addServer(makeServer("SRV-"+string(rune('A'+i)), "server-"+string(rune('A'+i)), "10.0.0."+string(rune('1'+i))))
	}

	resp, err := svc.ListServers(context.Background(), &dto.ServerFilter{Page: 1, PageSize: 5})
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if resp.Page != 1 {
		t.Errorf("expected page 1, got %d", resp.Page)
	}
	if resp.Total != 15 {
		t.Errorf("expected 15 total, got %d", resp.Total)
	}
	// Note: actual pagination truncation is handled at DB layer (repository),
	// not in the service. Mock repository returns all records.
}

func TestListServers_RepositoryError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.findAllShouldFail = true

	_, err := svc.ListServers(context.Background(), &dto.ServerFilter{Page: 1, PageSize: 20})
	if err == nil {
		t.Fatal("expected repository error")
	}
}

func TestListServers_DefaultPaginationValues(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "server-a", "10.0.0.1"))

	resp, err := svc.ListServers(context.Background(), &dto.ServerFilter{})
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if resp.Page != 1 || resp.PageSize != 20 || resp.TotalPages != 1 {
		t.Fatalf("unexpected default pagination: %#v", resp)
	}
}

// ── Update Tests ──

func TestUpdateServer_Success(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "old-name", "10.0.0.1"))

	newOS := "Ubuntu 24.04"
	req := &dto.UpdateServerRequest{OS: &newOS}

	resp, err := svc.UpdateServer(context.Background(), "SRV-001", req)
	if err != nil {
		t.Fatalf("UpdateServer failed: %v", err)
	}
	if resp.OS != "Ubuntu 24.04" {
		t.Errorf("expected OS 'Ubuntu 24.04', got '%s'", resp.OS)
	}
}

func TestUpdateServer_NotFound(t *testing.T) {
	svc, _ := newTestServerService()
	req := &dto.UpdateServerRequest{}

	_, err := svc.UpdateServer(context.Background(), "NONEXIST", req)
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestUpdateServer_DuplicateServerName(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "server-a", "10.0.0.1"))
	repo.addServer(makeServer("SRV-002", "server-b", "10.0.0.2"))

	newName := "server-b"
	req := &dto.UpdateServerRequest{ServerName: &newName}

	_, err := svc.UpdateServer(context.Background(), "SRV-001", req)
	if err == nil {
		t.Fatal("expected error for duplicate server_name")
	}
}

func TestUpdateServer_AllFieldsAndSameName(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "server-a", "10.0.0.1"))

	name := "server-a"
	ipv4 := "10.0.0.2"
	osName := "Debian"
	cpu := 8
	ram := 32.0
	disk := 512.0
	location := "DC-HN"
	description := "updated"
	resp, err := svc.UpdateServer(context.Background(), "SRV-001", &dto.UpdateServerRequest{
		ServerName:  &name,
		IPv4:        &ipv4,
		OS:          &osName,
		CPUCores:    &cpu,
		RAMGB:       &ram,
		DiskGB:      &disk,
		Location:    &location,
		Description: &description,
	})
	if err != nil {
		t.Fatalf("UpdateServer failed: %v", err)
	}
	if resp.IPv4 != ipv4 || resp.OS != osName || *resp.CPUCores != cpu || *resp.RAMGB != ram || *resp.DiskGB != disk || resp.Location != location || resp.Description != description {
		t.Fatalf("fields were not updated: %#v", resp)
	}
}

func TestUpdateServer_FindRepositoryError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.findShouldFail = true

	_, err := svc.UpdateServer(context.Background(), "SRV-001", &dto.UpdateServerRequest{})
	if err == nil {
		t.Fatal("expected find repository error")
	}
}

func TestUpdateServer_UpdateError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "server-a", "10.0.0.1"))
	repo.updateShouldFail = true

	_, err := svc.UpdateServer(context.Background(), "SRV-001", &dto.UpdateServerRequest{})
	if err == nil {
		t.Fatal("expected update error")
	}
}

// ── Delete Tests ──

func TestDeleteServer_Success(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "to-delete", "10.0.0.1"))

	err := svc.DeleteServer(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	// Verify it's gone
	_, err = svc.GetServer(context.Background(), "SRV-001")
	if err == nil {
		t.Fatal("expected server to be deleted")
	}
}

func TestDeleteServer_NotFound(t *testing.T) {
	svc, _ := newTestServerService()

	err := svc.DeleteServer(context.Background(), "NONEXIST")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestDeleteServer_FindRepositoryError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.findShouldFail = true

	err := svc.DeleteServer(context.Background(), "SRV-001")
	if err == nil {
		t.Fatal("expected find repository error")
	}
}

func TestDeleteServer_DeleteError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "to-delete", "10.0.0.1"))
	repo.deleteShouldFail = true

	err := svc.DeleteServer(context.Background(), "SRV-001")
	if err == nil {
		t.Fatal("expected delete error")
	}
}

func TestInvalidateCacheDeletesDetailAndListKeys(t *testing.T) {
	cache := newFakeServerCache()
	cache.values["server:detail:SRV-001"] = "{}"
	cache.values["servers:list:a"] = "{}"
	cache.values["servers:list:b"] = "{}"
	cache.scanKeys = []string{"servers:list:a", "servers:list:b"}
	svc, _ := newTestServerServiceWithCache(cache)

	svc.(*serverServiceImpl).invalidateCache(context.Background(), "SRV-001")

	if len(cache.deletedKeys) != 3 {
		t.Fatalf("expected detail plus two list keys deleted, got %#v", cache.deletedKeys)
	}
}

func TestInvalidateCacheStopsWhenScanFails(t *testing.T) {
	cache := newFakeServerCache()
	cache.scanErr = errors.New("scan failed")
	svc, _ := newTestServerServiceWithCache(cache)

	svc.(*serverServiceImpl).invalidateCache(context.Background(), "SRV-001")

	if len(cache.deletedKeys) != 1 || cache.deletedKeys[0] != "server:detail:SRV-001" {
		t.Fatalf("expected only detail key deleted, got %#v", cache.deletedKeys)
	}
}

func TestBuildListCacheKey_DiffersByFilter(t *testing.T) {
	a := buildListCacheKey(&dto.ServerFilter{Status: "on", Page: 1, PageSize: 20})
	b := buildListCacheKey(&dto.ServerFilter{Status: "off", Page: 1, PageSize: 20})
	if a == b {
		t.Fatal("expected different cache keys for different filters")
	}
}
