package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/infrastructure/projection"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/server-service/internal/repository"
	"github.com/vcs-sms/server-service/internal/validator"
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
	countErr               error
}

type fakeServerCache struct {
	values  map[string]string
	getErr  error
	incrErr error
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

func (c *fakeServerCache) Incr(ctx context.Context, key string) error {
	if c.incrErr != nil {
		return c.incrErr
	}
	// fake increment
	c.values[key] = "1"
	return nil
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

func (m *mockServerRepo) FindActiveTargets(ctx context.Context, cursor string, limit int) ([]model.Server, error) {
	return nil, nil
}

func (m *mockServerRepo) ApplyStatusEvent(ctx context.Context, u repository.StatusUpdate) (int64, error) {
	return 0, nil
}

func (m *mockServerRepo) FindExistingNames(ctx context.Context, names []string) ([]string, error) {
	return nil, nil
}

func (m *mockServerRepo) InsertBatch(ctx context.Context, servers []model.Server) ([]string, error) {
	return nil, nil
}

func (m *mockServerRepo) InsertOne(ctx context.Context, server *model.Server) error {
	return nil
}

func (m *mockServerRepo) CountByStatus(ctx context.Context) (map[string]int64, error) {
	if m.countErr != nil {
		return nil, m.countErr
	}
	out := make(map[string]int64)
	for _, s := range m.servers {
		out[s.Status]++
	}
	return out, nil
}

func (m *mockServerRepo) FindPopulation(ctx context.Context, q repository.PopulationQuery) ([]model.Server, error) {
	return nil, nil
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

func testCIDRValidator() *validator.CIDRValidator {
	v, err := validator.NewCIDRValidator("10.0.0.0/8,192.168.0.0/16")
	if err != nil {
		panic(err)
	}
	return v
}

// fakeTargetProjection records projection calls made by CRUD operations.
type fakeTargetProjection struct {
	synced  map[string]string // server_id -> "ipv4:tcp_port"
	names   map[string]string // server_id -> server_name
	deleted []string
	err     error
}

func newFakeTargetProjection() *fakeTargetProjection {
	return &fakeTargetProjection{synced: make(map[string]string), names: make(map[string]string)}
}

func (f *fakeTargetProjection) Sync(ctx context.Context, t projection.Target) error {
	if f.err != nil {
		return f.err
	}
	f.synced[t.ServerID] = fmt.Sprintf("%s:%d", t.IPv4, t.TCPPort)
	f.names[t.ServerID] = t.ServerName
	return nil
}

func (f *fakeTargetProjection) Delete(ctx context.Context, serverID string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, serverID)
	return nil
}

func (f *fakeTargetProjection) Rebuild(ctx context.Context, src projection.TargetSource) (int, error) {
	return 0, nil
}

// fakeLastCheckReader returns canned last_checked_at values.
type fakeLastCheckReader struct {
	values map[string]time.Time
	calls  int
}

func (f *fakeLastCheckReader) LastCheckedAt(ctx context.Context, serverIDs []string) map[string]time.Time {
	f.calls++
	out := make(map[string]time.Time)
	for _, id := range serverIDs {
		if ts, ok := f.values[id]; ok {
			out[id] = ts
		}
	}
	return out
}

func newTestServerService() (ServerService, *mockServerRepo) {
	svc, repo, _ := newTestServerServiceWithProjection()
	return svc, repo
}

func newTestServerServiceWithProjection() (ServerService, *mockServerRepo, *fakeTargetProjection) {
	repo := newMockServerRepo()
	targets := newFakeTargetProjection()
	svc := &serverServiceImpl{repo: repo, cidr: testCIDRValidator(), targets: targets}
	return svc, repo, targets
}

func newTestServerServiceWithCache(cache serverCache) (ServerService, *mockServerRepo) {
	repo := newMockServerRepo()
	svc := &serverServiceImpl{
		repo:    repo,
		cache:   cache,
		cidr:    testCIDRValidator(),
		targets: newFakeTargetProjection(),
	}
	return svc, repo
}

func makeServer(id, name, ip string) *model.Server {
	now := time.Now().UTC()
	return &model.Server{
		ServerID:   id,
		ServerName: name,
		Status:     "UNKNOWN",
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
	if resp.Status != "UNKNOWN" {
		t.Errorf("expected Status 'UNKNOWN', got '%s'", resp.Status)
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
	cache.values[buildDetailCacheKey("SRV-001", "0")] = string(data)
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
	key := buildDetailCacheKey("SRV-001", "0")
	cache.values[key] = "{bad json"
	svc, repo := newTestServerServiceWithCache(cache)
	repo.addServer(makeServer("SRV-001", "from-db", "10.0.0.1"))

	resp, err := svc.GetServer(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if resp.ServerName != "from-db" {
		t.Fatalf("expected DB response, got %#v", resp)
	}
	if cache.values[key] == "{bad json" {
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
	cache.values[buildListCacheKey(filter, "0")] = string(data)
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
	cache.values[buildListCacheKey(filter, "0")] = "{bad json"
	svc, repo := newTestServerServiceWithCache(cache)
	repo.addServer(makeServer("SRV-001", "from-db", "10.0.0.1"))

	resp, err := svc.ListServers(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if resp.Total != 1 || resp.Servers[0].ServerName != "from-db" {
		t.Fatalf("expected DB list, got %#v", resp)
	}
	if cache.values[buildListCacheKey(filter, "0")] == "{bad json" {
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
	ram := 32
	disk := 512
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
	if resp.IPv4 != ipv4 || resp.OS != osName || resp.CPUCores != cpu || resp.RAMGB != ram || resp.DiskGB != disk || resp.Location != location || resp.Description != description {
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

func TestBumpListVersion(t *testing.T) {
	cache := newFakeServerCache()
	svc, _ := newTestServerServiceWithCache(cache)

	svc.(*serverServiceImpl).bumpListVersion(context.Background())

	if cache.values["server:list:version"] != "1" {
		t.Fatalf("expected list version to be bumped")
	}
}

// A bumped version must strand the previous detail cache entry rather than
// serving it, since nothing deletes the old key.
func TestDeleteServer_StrandsPreviousDetailCache(t *testing.T) {
	cache := newFakeServerCache()
	svc, repo := newTestServerServiceWithCache(cache)
	repo.addServer(makeServer("SRV-001", "to-delete", "10.0.0.1"))
	ctx := context.Background()

	if _, err := svc.GetServer(ctx, "SRV-001"); err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	staleKey := buildDetailCacheKey("SRV-001", "0")
	if _, ok := cache.values[staleKey]; !ok {
		t.Fatalf("expected detail cached under %q", staleKey)
	}

	if err := svc.DeleteServer(ctx, "SRV-001"); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	if _, err := svc.GetServer(ctx, "SRV-001"); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("expected ErrServerNotFound after delete, got %v", err)
	}
}

func TestCreateServer_SyncsTargetProjection(t *testing.T) {
	svc, _, targets := newTestServerServiceWithProjection()

	_, err := svc.CreateServer(context.Background(), &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "web-01",
		IPv4:       "10.0.0.1",
		TCPPort:    8080,
	})
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	if got := targets.synced["SRV-001"]; got != "10.0.0.1:8080" {
		t.Fatalf("target = %q, want 10.0.0.1:8080", got)
	}
	// Monitoring denormalises server_name onto every health fact, so the
	// projection has to carry it.
	if got := targets.names["SRV-001"]; got != "web-01" {
		t.Errorf("projected server_name = %q, want web-01", got)
	}
}

func TestUpdateServer_ResyncsTargetProjection(t *testing.T) {
	svc, repo, targets := newTestServerServiceWithProjection()
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))
	newIP := "10.0.0.9"
	newPort := 9090

	_, err := svc.UpdateServer(context.Background(), "SRV-001", &dto.UpdateServerRequest{
		IPv4:    &newIP,
		TCPPort: &newPort,
	})
	if err != nil {
		t.Fatalf("UpdateServer failed: %v", err)
	}

	if got := targets.synced["SRV-001"]; got != "10.0.0.9:9090" {
		t.Fatalf("target = %q, want 10.0.0.9:9090", got)
	}
}

func TestDeleteServer_RemovesTargetProjection(t *testing.T) {
	svc, repo, targets := newTestServerServiceWithProjection()
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))

	if err := svc.DeleteServer(context.Background(), "SRV-001"); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	if len(targets.deleted) != 1 || targets.deleted[0] != "SRV-001" {
		t.Fatalf("deleted = %v, want [SRV-001]", targets.deleted)
	}
}

// PostgreSQL is the source of truth, so projection failures must not fail the
// request — reconciliation repairs the drift later.
func TestCreateServer_SucceedsWhenProjectionFails(t *testing.T) {
	svc, _, targets := newTestServerServiceWithProjection()
	targets.err = errors.New("redis down")

	resp, err := svc.CreateServer(context.Background(), &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "web-01",
		IPv4:       "10.0.0.1",
	})

	if err != nil {
		t.Fatalf("expected create to succeed despite projection failure, got %v", err)
	}
	if resp.ServerID != "SRV-001" {
		t.Errorf("unexpected response %#v", resp)
	}
}

func TestDeleteServer_SucceedsWhenProjectionFails(t *testing.T) {
	svc, repo, targets := newTestServerServiceWithProjection()
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))
	targets.err = errors.New("redis down")

	if err := svc.DeleteServer(context.Background(), "SRV-001"); err != nil {
		t.Fatalf("expected delete to succeed despite projection failure, got %v", err)
	}
}

func TestCreateServer_RejectsIPOutsideAllowlist(t *testing.T) {
	svc, _ := newTestServerService()

	_, err := svc.CreateServer(context.Background(), &dto.CreateServerRequest{
		ServerID:   "SRV-001",
		ServerName: "web-01",
		IPv4:       "127.0.0.1",
	})

	if !errors.Is(err, validator.ErrIPNotAllowed) {
		t.Fatalf("expected ErrIPNotAllowed, got %v", err)
	}
}

func TestUpdateServer_RejectsIPOutsideAllowlist(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))
	bad := "169.254.169.254"

	_, err := svc.UpdateServer(context.Background(), "SRV-001", &dto.UpdateServerRequest{IPv4: &bad})

	if !errors.Is(err, validator.ErrIPNotAllowed) {
		t.Fatalf("expected ErrIPNotAllowed, got %v", err)
	}
}

func TestBuildListCacheKey_DiffersByFilter(t *testing.T) {
	a := buildListCacheKey(&dto.ServerFilter{Status: "on", Page: 1, PageSize: 20}, "0")
	b := buildListCacheKey(&dto.ServerFilter{Status: "off", Page: 1, PageSize: 20}, "0")
	if a == b {
		t.Fatal("expected different cache keys for different filters")
	}
}

func TestBuildListCacheKey_DiffersByPort(t *testing.T) {
	a := buildListCacheKey(&dto.ServerFilter{TCPPort: 80, Page: 1, PageSize: 20}, "0")
	b := buildListCacheKey(&dto.ServerFilter{TCPPort: 8080, Page: 1, PageSize: 20}, "0")
	if a == b {
		t.Fatal("expected different cache keys for different ports")
	}
}

// ── last_status_check enrichment ──

var testCheckedAt = time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)

func newTestServiceWithLastCheck(cache serverCache) (ServerService, *mockServerRepo, *fakeLastCheckReader) {
	repo := newMockServerRepo()
	reader := &fakeLastCheckReader{values: map[string]time.Time{"SRV-001": testCheckedAt}}
	svc := &serverServiceImpl{
		repo:      repo,
		cache:     cache,
		cidr:      testCIDRValidator(),
		targets:   newFakeTargetProjection(),
		lastCheck: reader,
	}
	return svc, repo, reader
}

func TestGetServer_EnrichesLastStatusCheck(t *testing.T) {
	svc, repo, _ := newTestServiceWithLastCheck(nil)
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))

	resp, err := svc.GetServer(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}

	if resp.LastStatusCheck == nil || !resp.LastStatusCheck.Equal(testCheckedAt) {
		t.Fatalf("LastStatusCheck = %v, want %v", resp.LastStatusCheck, testCheckedAt)
	}
}

func TestListServers_EnrichesLastStatusCheck(t *testing.T) {
	svc, repo, _ := newTestServiceWithLastCheck(nil)
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))

	resp, err := svc.ListServers(context.Background(), &dto.ServerFilter{})
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}

	if len(resp.Servers) != 1 {
		t.Fatalf("got %d servers, want 1", len(resp.Servers))
	}
	if resp.Servers[0].LastStatusCheck == nil {
		t.Fatal("expected LastStatusCheck to be enriched")
	}
}

// A server Monitoring has never checked has no Redis value, so the field stays null.
func TestGetServer_LastStatusCheckNullWhenNeverChecked(t *testing.T) {
	svc, repo, _ := newTestServiceWithLastCheck(nil)
	repo.addServer(makeServer("SRV-999", "never-checked", "10.0.0.9"))

	resp, err := svc.GetServer(context.Background(), "SRV-999")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}

	if resp.LastStatusCheck != nil {
		t.Errorf("LastStatusCheck = %v, want nil", resp.LastStatusCheck)
	}
}

// Redis being down must not fail the request; only the field goes null.
func TestGetServer_SucceedsWithoutLastCheckReader(t *testing.T) {
	svc, repo := newTestServerService()
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))

	resp, err := svc.GetServer(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if resp.LastStatusCheck != nil {
		t.Errorf("LastStatusCheck = %v, want nil", resp.LastStatusCheck)
	}
}

// last_status_check must never be written into the cache: it changes every
// round, so a cached copy would go stale within a minute.
func TestGetServer_DoesNotCacheLastStatusCheck(t *testing.T) {
	cache := newFakeServerCache()
	svc, repo, _ := newTestServiceWithLastCheck(cache)
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))

	if _, err := svc.GetServer(context.Background(), "SRV-001"); err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}

	raw := cache.values[buildDetailCacheKey("SRV-001", "0")]
	var cached dto.ServerResponse
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("cached entry is not valid JSON: %v", err)
	}
	if cached.LastStatusCheck != nil {
		t.Errorf("cached LastStatusCheck = %v, want nil", cached.LastStatusCheck)
	}
}

// A cache hit still has to read last_status_check fresh from Redis.
func TestGetServer_EnrichesOnCacheHit(t *testing.T) {
	cache := newFakeServerCache()
	svc, repo, reader := newTestServiceWithLastCheck(cache)
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))
	ctx := context.Background()

	if _, err := svc.GetServer(ctx, "SRV-001"); err != nil {
		t.Fatalf("first GetServer failed: %v", err)
	}
	repo.findShouldFail = true // force the second read to come from cache

	resp, err := svc.GetServer(ctx, "SRV-001")
	if err != nil {
		t.Fatalf("cached GetServer failed: %v", err)
	}

	if resp.LastStatusCheck == nil || !resp.LastStatusCheck.Equal(testCheckedAt) {
		t.Fatalf("LastStatusCheck = %v, want %v on a cache hit", resp.LastStatusCheck, testCheckedAt)
	}
	if reader.calls != 2 {
		t.Errorf("reader called %d times, want 2 (once per request)", reader.calls)
	}
}

func TestListServers_EnrichesOnCacheHit(t *testing.T) {
	cache := newFakeServerCache()
	svc, repo, reader := newTestServiceWithLastCheck(cache)
	repo.addServer(makeServer("SRV-001", "web-01", "10.0.0.1"))
	ctx := context.Background()

	if _, err := svc.ListServers(ctx, &dto.ServerFilter{}); err != nil {
		t.Fatalf("first ListServers failed: %v", err)
	}
	repo.findAllShouldFail = true

	resp, err := svc.ListServers(ctx, &dto.ServerFilter{})
	if err != nil {
		t.Fatalf("cached ListServers failed: %v", err)
	}

	if resp.Servers[0].LastStatusCheck == nil {
		t.Fatal("expected LastStatusCheck on a cache hit")
	}
	if reader.calls != 2 {
		t.Errorf("reader called %d times, want 2 (once per request)", reader.calls)
	}
}

// ── Stats ──

func TestGetStats_CountsByStatus(t *testing.T) {
	svc, repo := newTestServerService()
	on1 := makeServer("SRV-001", "a", "10.0.0.1")
	on1.Status = "ON"
	on2 := makeServer("SRV-002", "b", "10.0.0.2")
	on2.Status = "ON"
	off := makeServer("SRV-003", "c", "10.0.0.3")
	off.Status = "OFF"
	repo.addServer(on1)
	repo.addServer(on2)
	repo.addServer(off)
	repo.addServer(makeServer("SRV-004", "d", "10.0.0.4")) // UNKNOWN

	resp, err := svc.GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if resp.On != 2 || resp.Off != 1 || resp.Unknown != 1 {
		t.Errorf("on/off/unknown = %d/%d/%d, want 2/1/1", resp.On, resp.Off, resp.Unknown)
	}
	if resp.Total != 4 {
		t.Errorf("Total = %d, want 4", resp.Total)
	}
}

func TestGetStats_EmptyCatalog(t *testing.T) {
	svc, _ := newTestServerService()

	resp, err := svc.GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
}

func TestGetStats_RepositoryError(t *testing.T) {
	svc, repo := newTestServerService()
	repo.countErr = errors.New("db down")

	if _, err := svc.GetStats(context.Background()); err == nil {
		t.Fatal("expected an error when the repository fails")
	}
}

func TestGetStats_UsesCache(t *testing.T) {
	cache := newFakeServerCache()
	svc, repo := newTestServerServiceWithCache(cache)
	on := makeServer("SRV-001", "a", "10.0.0.1")
	on.Status = "ON"
	repo.addServer(on)
	ctx := context.Background()

	if _, err := svc.GetStats(ctx); err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	repo.countErr = errors.New("db down") // a cache hit must not reach the DB

	resp, err := svc.GetStats(ctx)
	if err != nil {
		t.Fatalf("cached GetStats failed: %v", err)
	}
	if resp.On != 1 {
		t.Errorf("On = %d, want 1 from cache", resp.On)
	}
	if _, ok := cache.values[statsCacheKey]; !ok {
		t.Errorf("expected stats cached under %q", statsCacheKey)
	}
}
