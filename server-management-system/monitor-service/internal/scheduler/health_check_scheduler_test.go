package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/monitor-service/internal/checker"
	checkermocks "github.com/vcs-sms/monitor-service/internal/checker/mocks"
	"github.com/vcs-sms/monitor-service/internal/model"
	"github.com/vcs-sms/monitor-service/internal/repository"
	repomocks "github.com/vcs-sms/monitor-service/internal/repository/mocks"
	"github.com/vcs-sms/monitor-service/internal/worker"
	"github.com/vcs-sms/shared/kafka"
	kafkamocks "github.com/vcs-sms/shared/kafka/mocks"
)

// helper to create a mock Redis with default success behavior
func newMockRedis() *MockRedisClient {
	store := make(map[string]string)
	return &MockRedisClient{
		SetNXFunc: func(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
			if _, exists := store[key]; exists {
				return false, nil
			}
			store[key] = fmt.Sprintf("%v", value)
			return true, nil
		},
		ReleaseLockFunc: func(ctx context.Context, key string, value string) error {
			if store[key] == value {
				delete(store, key)
			}
			return nil
		},
		GetFunc: func(ctx context.Context, key string) (string, error) {
			return store[key], nil
		},
		SetFunc: func(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
			store[key] = fmt.Sprintf("%v", value)
			return nil
		},
	}
}

func TestScheduler_RunCycle_FullWithRedis(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{
				{ServerID: "SRV-001", ServerName: "Server 1", IPv4: "10.0.0.1", Status: "on"},
				{ServerID: "SRV-002", ServerName: "Server 2", IPv4: "10.0.0.2", Status: "off"},
			}, nil
		},
		BatchUpdateStatusFunc: func(ctx context.Context, changes []repository.StatusChangeEvent) error {
			return nil
		},
	}

	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, nil
		},
	}

	esCalled := false
	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error {
			esCalled = true
			return nil
		},
	}

	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{
				ServerID: server.ServerID, ServerName: server.ServerName,
				Status: "on", CheckMethod: "tcp", CheckedAt: time.Now().UTC(),
			}
		},
	}

	kafkaCalled := false
	producer := &kafkamocks.ProducerMock{
		PublishFunc: func(ctx context.Context, topic string, key string, value interface{}) error {
			kafkaCalled = true
			return nil
		},
	}

	redisMock := newMockRedis()
	pool := worker.NewPool(2, checkerMock, zerolog.Nop())

	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, redisMock, producer,
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())

	if !esCalled {
		t.Error("Expected ES bulk index to be called")
	}
	if !kafkaCalled {
		t.Error("Expected Kafka publish to be called")
	}
}

func TestScheduler_RunCycle_LockAlreadyHeld(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{}
	configRepo := &repomocks.HealthCheckConfigRepoMock{}
	esRepo := &repomocks.ESStatusLogRepoMock{}
	checkerMock := &checkermocks.HealthCheckerMock{}
	producer := &kafkamocks.ProducerMock{}

	// Redis returns lock NOT acquired
	redisMock := &MockRedisClient{
		SetNXFunc: func(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
			return false, nil // lock already held by another instance
		},
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, redisMock, producer,
		zerolog.Nop(), 60*time.Second,
	)

	// Should skip cycle when lock is held
	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_StatusChanged(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{
				{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1", Status: "on"},
			}, nil
		},
		BatchUpdateStatusFunc: func(ctx context.Context, changes []repository.StatusChangeEvent) error {
			if len(changes) != 1 {
				t.Errorf("Expected 1 status change, got %d", len(changes))
			}
			if changes[0].ServerID != "SRV-001" {
				t.Errorf("Expected SRV-001, got %s", changes[0].ServerID)
			}
			return nil
		},
	}

	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, nil
		},
	}

	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error {
			return nil
		},
	}

	// Checker returns "off" — different from old cached "on"
	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{
				ServerID: server.ServerID, Status: "off", CheckMethod: "tcp",
				CheckedAt: time.Now().UTC(),
			}
		},
	}

	// Redis has old status "on" → detects change to "off"
	redisMock := newMockRedis()
	redisMock.SetFunc(context.Background(), "server:status:SRV-001", "on", 0) // pre-set old status

	producer := &kafkamocks.ProducerMock{
		PublishFunc: func(ctx context.Context, topic string, key string, value interface{}) error {
			return nil
		},
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, redisMock, producer,
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_CacheMissUsesDBStatus(t *testing.T) {
	batchCalled := false
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{
				{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1", Status: "off"},
			}, nil
		},
		BatchUpdateStatusFunc: func(ctx context.Context, changes []repository.StatusChangeEvent) error {
			batchCalled = true
			if len(changes) != 1 {
				t.Fatalf("expected 1 status change, got %d", len(changes))
			}
			if changes[0].OldStatus != "off" || changes[0].NewStatus != "on" {
				t.Fatalf("unexpected status change: %+v", changes[0])
			}
			return nil
		},
	}

	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, nil
		},
	}
	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error { return nil },
	}
	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{
				ServerID: server.ServerID, Status: "on", CheckMethod: "tcp",
				CheckedAt: time.Now().UTC(),
			}
		},
	}
	producer := &kafkamocks.ProducerMock{
		PublishFunc: func(ctx context.Context, topic string, key string, value interface{}) error { return nil },
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, newMockRedis(), producer,
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())

	if !batchCalled {
		t.Fatal("expected BatchUpdateStatus to be called on Redis cache miss when DB status differs")
	}
}

func TestScheduler_RunCycle_ReleaseLockDoesNotDeleteDifferentOwner(t *testing.T) {
	lockOwner := ""
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1", Status: "on"}}, nil
		},
	}
	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) { return nil, nil },
	}
	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error {
			lockOwner = "other-owner"
			return nil
		},
	}
	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{ServerID: server.ServerID, Status: "on", CheckMethod: "tcp", CheckedAt: time.Now().UTC()}
		},
	}
	redisMock := &MockRedisClient{
		SetNXFunc: func(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
			lockOwner = fmt.Sprintf("%v", value)
			return true, nil
		},
		ReleaseLockFunc: func(ctx context.Context, key string, value string) error {
			if lockOwner == value {
				lockOwner = ""
			}
			return nil
		},
		GetFunc: func(ctx context.Context, key string) (string, error) { return "on", nil },
		SetFunc: func(ctx context.Context, key string, value interface{}, expiration time.Duration) error { return nil },
	}
	producer := &kafkamocks.ProducerMock{
		PublishFunc: func(ctx context.Context, topic string, key string, value interface{}) error { return nil },
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, redisMock, producer,
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())

	if lockOwner != "other-owner" {
		t.Fatalf("expected different owner lock to remain, got %q", lockOwner)
	}
}

func TestScheduler_RunCycle_NoStatusChange(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{
				{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1"},
			}, nil
		},
		BatchUpdateStatusFunc: func(ctx context.Context, changes []repository.StatusChangeEvent) error {
			if len(changes) != 0 {
				t.Errorf("Expected 0 status changes, got %d", len(changes))
			}
			return nil
		},
	}

	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, nil
		},
	}

	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error {
			return nil
		},
	}

	// Checker also returns "on" — same as cached
	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{
				ServerID: server.ServerID, Status: "on", CheckMethod: "tcp",
				CheckedAt: time.Now().UTC(),
			}
		},
	}

	// Pre-set same status
	redisMock := newMockRedis()
	redisMock.SetFunc(context.Background(), "server:status:SRV-001", "on", 0)

	producer := &kafkamocks.ProducerMock{
		PublishFunc: func(ctx context.Context, topic string, key string, value interface{}) error {
			return nil
		},
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, redisMock, producer,
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_NilRedis(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{
				{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1"},
			}, nil
		},
	}

	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, nil
		},
	}

	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error {
			return nil
		},
	}

	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{
				ServerID: server.ServerID, Status: "on", CheckMethod: "tcp",
				CheckedAt: time.Now().UTC(),
			}
		},
	}

	producer := &kafkamocks.ProducerMock{
		PublishFunc: func(ctx context.Context, topic string, key string, value interface{}) error {
			return nil
		},
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, nil, producer,
		zerolog.Nop(), 60*time.Second,
	)

	// Nil Redis should not crash — runs without lock or status cache
	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_NoServers(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return nil, nil
		},
	}

	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, nil
		},
	}

	pool := worker.NewPool(1, &checkermocks.HealthCheckerMock{}, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, &repomocks.ESStatusLogRepoMock{},
		&MockRedisClient{}, &kafkamocks.ProducerMock{},
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_LoadError(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return nil, fmt.Errorf("db connection error")
		},
	}

	pool := worker.NewPool(1, &checkermocks.HealthCheckerMock{}, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, &repomocks.HealthCheckConfigRepoMock{},
		&repomocks.ESStatusLogRepoMock{}, &MockRedisClient{}, &kafkamocks.ProducerMock{},
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_ESBulkFail(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{
				{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1"},
			}, nil
		},
	}

	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error {
			return fmt.Errorf("ES cluster unavailable")
		},
	}

	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{
				ServerID: server.ServerID, Status: "on", CheckMethod: "tcp",
				CheckedAt: time.Now().UTC(),
			}
		},
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, &repomocks.HealthCheckConfigRepoMock{},
		esRepo, &MockRedisClient{}, &kafkamocks.ProducerMock{},
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_ConfigLoadError(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1"}}, nil
		},
	}
	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, fmt.Errorf("config db unavailable")
		},
	}

	pool := worker.NewPool(1, &checkermocks.HealthCheckerMock{}, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, &repomocks.ESStatusLogRepoMock{},
		newMockRedis(), &kafkamocks.ProducerMock{}, zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_LockError(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1"}}, nil
		},
	}
	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return []model.HealthCheckConfig{{ServerID: "SRV-001", TCPPort: 8080, TCPTimeoutMs: 10000, UptimeRate: 0.5}}, nil
		},
	}
	redisMock := &MockRedisClient{
		SetNXFunc: func(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
			return false, fmt.Errorf("redis unavailable")
		},
	}

	pool := worker.NewPool(1, &checkermocks.HealthCheckerMock{}, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, &repomocks.ESStatusLogRepoMock{},
		redisMock, &kafkamocks.ProducerMock{}, zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_RunCycle_ContextCancelledBeforeExecute(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1"}}, nil
		},
	}
	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) { return nil, nil },
	}
	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{ServerID: server.ServerID, Status: "on", CheckedAt: time.Now().UTC()}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, &repomocks.ESStatusLogRepoMock{},
		nil, &kafkamocks.ProducerMock{}, zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(ctx)
}

func TestScheduler_RunCycle_BatchAndKafkaErrors(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return []repository.ServerInfo{{ServerID: "SRV-001", ServerName: "S1", IPv4: "10.0.0.1", Status: "off"}}, nil
		},
		BatchUpdateStatusFunc: func(ctx context.Context, changes []repository.StatusChangeEvent) error {
			return fmt.Errorf("batch update failed")
		},
	}
	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) { return nil, nil },
	}
	esRepo := &repomocks.ESStatusLogRepoMock{
		BulkIndexFunc: func(ctx context.Context, results []*checker.HealthResult) error { return nil },
	}
	checkerMock := &checkermocks.HealthCheckerMock{
		CheckFunc: func(ctx context.Context, server *checker.ServerInfo) *checker.HealthResult {
			return &checker.HealthResult{ServerID: server.ServerID, Status: "on", CheckMethod: "tcp", CheckedAt: time.Now().UTC()}
		},
	}
	producer := &kafkamocks.ProducerMock{
		PublishFunc: func(ctx context.Context, topic string, key string, value interface{}) error {
			return fmt.Errorf("kafka unavailable")
		},
	}

	pool := worker.NewPool(1, checkerMock, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, esRepo, newMockRedis(), producer,
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycle(context.Background())
}

func TestScheduler_CountByStatus(t *testing.T) {
	results := []*checker.HealthResult{
		{Status: "on"}, {Status: "on"}, {Status: "off"}, {Status: "on"},
	}

	if c := countByStatus(results, "on"); c != 3 {
		t.Errorf("Expected 3 on, got %d", c)
	}
	if c := countByStatus(results, "off"); c != 1 {
		t.Errorf("Expected 1 off, got %d", c)
	}
}

func TestScheduler_CalculateLockTTLUsesEstimatedCycle(t *testing.T) {
	ttl := calculateLockTTL(10000, 100, 5000, 60*time.Second)
	if ttl < 530*time.Second {
		t.Fatalf("expected ttl to cover estimated cycle, got %v", ttl)
	}
}

func TestScheduler_CalculateLockTTLUsesMinimums(t *testing.T) {
	ttl := calculateLockTTL(0, 0, 0, 10*time.Second)
	if ttl != 40*time.Second {
		t.Fatalf("expected interval minimum TTL, got %v", ttl)
	}
}

func TestScheduler_RunCycleSafeRecoversPanic(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			panic("boom")
		},
	}
	pool := worker.NewPool(1, &checkermocks.HealthCheckerMock{}, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, &repomocks.HealthCheckConfigRepoMock{},
		&repomocks.ESStatusLogRepoMock{}, nil, &kafkamocks.ProducerMock{},
		zerolog.Nop(), 60*time.Second,
	)

	scheduler.runCycleSafe(context.Background())
}

func TestScheduler_StartStopsOnContextCancel(t *testing.T) {
	serverReader := &repomocks.ServerReaderMock{
		GetAllActiveServersFunc: func(ctx context.Context) ([]repository.ServerInfo, error) {
			return nil, nil
		},
	}
	configRepo := &repomocks.HealthCheckConfigRepoMock{
		GetAllEnabledFunc: func(ctx context.Context) ([]model.HealthCheckConfig, error) {
			return nil, nil
		},
	}
	pool := worker.NewPool(1, &checkermocks.HealthCheckerMock{}, zerolog.Nop())
	scheduler := NewHealthCheckScheduler(
		pool, serverReader, configRepo, &repomocks.ESStatusLogRepoMock{},
		newMockRedis(), &kafkamocks.ProducerMock{},
		zerolog.Nop(), time.Hour,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Start(ctx)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after context cancellation")
	}
}

func TestRealRedisClient_ErrorPaths(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer rdb.Close()

	client := NewRealRedisClient(rdb)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := client.SetNX(ctx, "lock", "owner", time.Second); err == nil {
		t.Fatal("expected SetNX error")
	}
	if err := client.ReleaseLock(ctx, "lock", "owner"); err == nil {
		t.Fatal("expected ReleaseLock error")
	}
	if _, err := client.Get(ctx, "status"); err == nil {
		t.Fatal("expected Get error")
	}
	if err := client.Set(ctx, "status", "on", time.Second); err == nil {
		t.Fatal("expected Set error")
	}
}

// Interface compliance check
var _ kafka.Producer = (*kafkamocks.ProducerMock)(nil)
var _ kafka.Consumer = (*kafkamocks.ConsumerMock)(nil)
var _ RedisClient = (*MockRedisClient)(nil)
