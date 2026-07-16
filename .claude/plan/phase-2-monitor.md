# Phase 2: Monitor Service + TCP Simulator

> **Mục tiêu:** TCP Simulator giả lập 10.000 server, Monitor Service health-check bằng TCP Connect thật, ghi trạng thái vào Elasticsearch.
> **Thời gian:** Tuần 3
> **Prerequisite:** Phase 1 hoàn tất (server data có trong DB, Kafka events đang publish)
> **Điểm đạt được:** 2.0 (Health-check) + 1.0 (Elasticsearch)

---

## Checklist tổng quan Phase 2

- [ ] **2.1** TCP Simulator: Math Engine + Listener Manager
- [ ] **2.2** TCP Health Checker (chỉ TCP Connect — không có Simulator mode)
- [ ] **2.3** Worker Pool pattern
- [ ] **2.4** Health-Check Scheduler (Cron)
- [ ] **2.5** Elasticsearch Repository (Bulk Index)
- [ ] **2.6** Kafka Consumer (server events) + Producer (status events)
- [ ] **2.7** Redis integration (distributed lock, status cache)
- [ ] **2.8** Config (health_check_configs table)
- [ ] **2.9** Dockerfile + main.go setup
- [ ] **2.10** Unit Tests
- [ ] **2.11** End-to-end verification

---

## 2.1. TCP Simulator Service — Math Engine + Listener Manager

> TCP Simulator đã được setup skeleton ở Phase 0 (section 0.10). Giờ cần implement đầy đủ logic.

### 2.1.1. Math Engine

**File:** `tcp-simulator/simulator/math_engine.go`

```go
type MathEngine struct{}

func (e *MathEngine) ShouldBeOnline(uptimeRate float64, serverIndex int) bool {
    // 1. Base rate (VD: 0.95 = 95%)
    baseRate := uptimeRate

    // 2. Hourly variation — sin wave tạo pattern trồi sụt theo giờ
    hour := float64(time.Now().Hour())
    hourlyVariation := math.Sin(hour * math.Pi / 12) * 0.05

    // 3. Server-specific offset — mỗi server có phase khác nhau
    serverPhase := float64(serverIndex) * 0.1
    serverVariation := math.Sin((hour+serverPhase) * math.Pi / 24) * 0.02

    // 4. Effective rate
    effectiveRate := math.Max(0, math.Min(1, baseRate + hourlyVariation + serverVariation))

    // 5. Random roll
    return rand.Float64() < effectiveRate
}
```

**Test cases (`math_engine_test.go`):**
```
✅ TestMathEngine_HighUptimeRate → uptimeRate=0.99, chạy 10000 lần → ~98-100% ON
✅ TestMathEngine_LowUptimeRate → uptimeRate=0.50, chạy 10000 lần → ~45-55% ON
✅ TestMathEngine_ZeroRate → uptimeRate=0.0 → <5% ON (hourly variation có thể tạo nhỏ)
✅ TestMathEngine_FullRate → uptimeRate=1.0 → >95% ON
✅ TestMathEngine_DifferentServers → same uptimeRate, different serverIndex → different results
✅ TestMathEngine_BoundaryClamp → effectiveRate luôn trong [0, 1]
```

### 2.1.2. Listener Manager

**File:** `tcp-simulator/simulator/manager.go`

```go
type SimulatorManager struct {
    servers      map[int]*FakeServer
    mathEngine   *MathEngine
    basePort     int
    numServers   int
    tickInterval time.Duration
}

type FakeServer struct {
    Index      int
    Port       int
    UptimeRate float64
    listener   net.Listener
    isOnline   bool
    mu         sync.Mutex
}
```

> Chi tiết code `RunControlLoop`, `evaluateAllServers`, `StartListening`, `StopListening` xem brainstorm Section 4.5.

**Test cases (`manager_test.go`):**
```
✅ TestManager_StartStop → start 10 listeners, stop all
✅ TestManager_PortReachable → start listener, TCP connect thành công
✅ TestManager_PortClosed → stop listener, TCP connect bị refused
✅ TestManager_EvaluateToggle → evaluate flips server on/off correctly
```

### 2.1.3. Config Loader

**File:** `tcp-simulator/simulator/config.go`

```go
type Config struct {
    BasePort     int           // default: 9001
    NumServers   int           // default: 10000
    TickInterval time.Duration // default: 30s
    DefaultUptime float64     // default: 0.95
}

func LoadConfig() *Config {
    // Load from env: SIMULATOR_BASE_PORT, SIMULATOR_NUM_SERVERS, etc.
}
```

---

## 2.2. TCP Health Checker

> **QUAN TRỌNG:** Monitor Service chỉ có **1 checker duy nhất** — `TCPChecker`. Không có SimulatorChecker, không có Factory toggle. Trạng thái On/Off do `tcp-simulator` service điều khiển.

### 2.2.1. HealthResult struct

**File:** `monitor-service/internal/checker/checker.go`

```go
package checker

type HealthResult struct {
    ServerID      string    `json:"server_id"`
    ServerName    string    `json:"server_name"`
    Status        string    `json:"status"`         // "on" hoặc "off"
    ResponseTimeMs int      `json:"response_time_ms"`
    CheckMethod   string    `json:"check_method"`   // luôn = "tcp"
    CheckedAt     time.Time `json:"checked_at"`
    Error         string    `json:"error,omitempty"`
}

type HealthChecker interface {
    Check(ctx context.Context, server *ServerInfo) *HealthResult
    Name() string
}

type ServerInfo struct {
    ServerID   string
    ServerName string
    IPv4       string  // = "tcp-simulator" (Docker DNS)
    TCPPort    int     // 9001..19000
    UptimeRate float64 // không dùng trong TCPChecker, chỉ lưu reference
}
```

### 2.2.2. TCP Connect Checker

**File:** `monitor-service/internal/checker/tcp_checker.go`

```go
type TCPChecker struct {
    Timeout time.Duration
}

func NewTCPChecker(timeout time.Duration) *TCPChecker {
    return &TCPChecker{Timeout: timeout}
}

func (c *TCPChecker) Check(ctx context.Context, server *ServerInfo) *HealthResult {
    start := time.Now()
    addr := fmt.Sprintf("%s:%d", server.IPv4, server.TCPPort)
    // server.IPv4 = "tcp-simulator" → Docker DNS resolve
    
    conn, err := net.DialTimeout("tcp", addr, c.Timeout)
    elapsed := time.Since(start).Milliseconds()
    
    result := &HealthResult{
        ServerID:    server.ServerID,
        ServerName:  server.ServerName,
        CheckMethod: "tcp",
        CheckedAt:   time.Now().UTC(),
    }
    
    if err != nil {
        result.Status = "off"
        result.ResponseTimeMs = 0
        result.Error = err.Error()
    } else {
        conn.Close()
        result.Status = "on"
        result.ResponseTimeMs = int(elapsed)
    }
    
    return result
}

func (c *TCPChecker) Name() string { return "tcp" }
```

**Test cases (`tcp_checker_test.go`):**
```
✅ TestTCPChecker_ServerReachable → mock TCP listener, status "on", responseTime > 0
✅ TestTCPChecker_ServerUnreachable → no listener, status "off", error not empty
✅ TestTCPChecker_Timeout → slow listener, status "off" after timeout
✅ TestTCPChecker_CheckMethod → luôn trả về "tcp"
✅ TestTCPChecker_ServerFields → ServerID, ServerName, CheckedAt populated correctly
```

---

## 2.3. Worker Pool Pattern

**File:** `monitor-service/internal/worker/pool.go`

```go
package worker

type Pool struct {
    workerCount int
    checker     checker.HealthChecker
    logger      zerolog.Logger
}

type Job struct {
    Server *checker.ServerInfo
}

func NewPool(workerCount int, chk checker.HealthChecker, log zerolog.Logger) *Pool {
    return &Pool{workerCount: workerCount, checker: chk, logger: log}
}

// Execute chạy health-check song song cho danh sách servers
// Trả về slice results (đã sort theo server_id)
func (p *Pool) Execute(ctx context.Context, servers []*checker.ServerInfo) []*checker.HealthResult {
    jobs := make(chan *checker.ServerInfo, len(servers))
    results := make(chan *checker.HealthResult, len(servers))
    
    // Spawn workers
    var wg sync.WaitGroup
    for i := 0; i < p.workerCount; i++ {
        wg.Add(1)
        go func(workerID int) {
            defer wg.Done()
            for server := range jobs {
                select {
                case <-ctx.Done():
                    return
                default:
                    result := p.checker.Check(ctx, server)
                    results <- result
                }
            }
        }(i)
    }
    
    // Fan-out: gửi jobs
    go func() {
        for _, srv := range servers {
            jobs <- srv
        }
        close(jobs)
    }()
    
    // Wait & close results
    go func() {
        wg.Wait()
        close(results)
    }()
    
    // Collect results
    var allResults []*checker.HealthResult
    for r := range results {
        allResults = append(allResults, r)
    }
    
    return allResults
}
```

**Test cases (`pool_test.go`):**
```
✅ TestPool_Execute_AllServers → 100 servers, 10 workers → 100 results
✅ TestPool_Execute_ContextCancel → cancel context mid-execution → partial results
✅ TestPool_Execute_EmptyList → 0 servers → 0 results
✅ TestPool_Execute_ConcurrencyVerify → measure time: 100 servers, 10 workers < 100 servers sequential
```

---

## 2.4. Health-Check Scheduler (Cron)

**File:** `monitor-service/internal/scheduler/health_check_scheduler.go`

```go
package scheduler

type HealthCheckScheduler struct {
    pool          *worker.Pool
    serverReader  repository.ServerReader        // cross-schema read
    configRepo    repository.HealthCheckConfigRepo
    esRepo        repository.ESStatusLogRepo
    redisClient   *redis.Client
    kafkaProducer kafka.Producer
    logger        zerolog.Logger
    interval      time.Duration
}

func NewHealthCheckScheduler(/* all dependencies */) *HealthCheckScheduler

// Start bắt đầu cron loop
func (s *HealthCheckScheduler) Start(ctx context.Context) {
    ticker := time.NewTicker(s.interval)
    defer ticker.Stop()
    
    s.logger.Info().
        Dur("interval", s.interval).
        Msg("Health-check scheduler started")
    
    // Run immediately on start
    s.runCycle(ctx)
    
    for {
        select {
        case <-ctx.Done():
            s.logger.Info().Msg("Health-check scheduler stopped")
            return
        case <-ticker.C:
            s.runCycle(ctx)
        }
    }
}

// runCycle thực hiện 1 vòng health-check
func (s *HealthCheckScheduler) runCycle(ctx context.Context) {
    cycleStart := time.Now()
    cycleID := uuid.New().String()
    
    s.logger.Info().Str("cycle_id", cycleID).Msg("Health-check cycle started")
    
    // Step 1: Acquire distributed lock
    lockKey := "health-check-lock"
    lockTTL := s.interval + 30*time.Second // TTL > interval để tránh overlap
    acquired, err := s.redisClient.SetNX(ctx, lockKey, cycleID, lockTTL).Result()
    if !acquired || err != nil {
        s.logger.Warn().Msg("Could not acquire lock, skipping cycle")
        return
    }
    defer s.redisClient.Del(ctx, lockKey)
    
    // Step 2: Load servers + configs
    servers, err := s.serverReader.GetAllActiveServers(ctx)
    if err != nil {
        s.logger.Error().Err(err).Msg("Failed to load servers")
        return
    }
    
    configs, err := s.configRepo.GetAllEnabled(ctx)
    configMap := toMap(configs) // server_id → config
    
    // Step 3: Build ServerInfo list (merge server + config)
    var serverInfos []*checker.ServerInfo
    for _, srv := range servers {
        info := &checker.ServerInfo{
            ServerID:   srv.ServerID,
            ServerName: srv.ServerName,
            IPv4:       srv.IPv4,
            TCPPort:    80,
            UptimeRate: 0.95, // default
        }
        if cfg, ok := configMap[srv.ServerID]; ok {
            info.TCPPort = cfg.TCPPort
            info.UptimeRate = cfg.UptimeRate
        }
        serverInfos = append(serverInfos, info)
    }
    
    // Step 4: Execute worker pool
    results := s.pool.Execute(ctx, serverInfos)
    
    // Step 5: Detect status changes (compare with Redis cache)
    var statusChanges []StatusChangeEvent
    for _, r := range results {
        cacheKey := fmt.Sprintf("server:status:%s", r.ServerID)
        oldStatus, _ := s.redisClient.Get(ctx, cacheKey).Result()
        
        if oldStatus != "" && oldStatus != r.Status {
            statusChanges = append(statusChanges, StatusChangeEvent{
                ServerID:  r.ServerID,
                OldStatus: oldStatus,
                NewStatus: r.Status,
                ChangedAt: r.CheckedAt,
            })
        }
        
        // Update Redis status cache
        s.redisClient.Set(ctx, cacheKey, r.Status, s.interval+30*time.Second)
    }
    
    // Step 6: Bulk write to Elasticsearch
    if err := s.esRepo.BulkIndex(ctx, results); err != nil {
        s.logger.Error().Err(err).Msg("Failed to bulk index to ES")
    }
    
    // Step 7: Batch update PostgreSQL (chỉ status thay đổi)
    if len(statusChanges) > 0 {
        if err := s.serverReader.BatchUpdateStatus(ctx, statusChanges); err != nil {
            s.logger.Error().Err(err).Msg("Failed to batch update status")
        }
    }
    
    // Step 8: Publish Kafka events
    // 8a. Batch summary
    s.kafkaProducer.Publish(ctx, "server.health.batch", &kafka.Event{
        EventType: "server.health.batch",
        Data: map[string]interface{}{
            "cycle_id":      cycleID,
            "total_servers": len(results),
            "servers_on":    countByStatus(results, "on"),
            "servers_off":   countByStatus(results, "off"),
            "changed_count": len(statusChanges),
            "duration_ms":   time.Since(cycleStart).Milliseconds(),
        },
    })
    
    // 8b. Individual status changes
    for _, change := range statusChanges {
        s.kafkaProducer.Publish(ctx, "server.status.changed", &kafka.Event{
            EventType: "server.status.changed",
            Data:      change,
        })
    }
    
    s.logger.Info().
        Str("cycle_id", cycleID).
        Int("total", len(results)).
        Int("on", countByStatus(results, "on")).
        Int("off", countByStatus(results, "off")).
        Int("changed", len(statusChanges)).
        Int64("duration_ms", time.Since(cycleStart).Milliseconds()).
        Msg("Health-check cycle completed")
}
```

---

## 2.5. Elasticsearch Repository

**File:** `monitor-service/internal/repository/es_repository.go`

```go
package repository

type ESStatusLogRepo interface {
    BulkIndex(ctx context.Context, results []*checker.HealthResult) error
}

type esStatusLogRepo struct {
    client *elasticsearch.Client
    index  string  // "server-status-logs"
}

func (r *esStatusLogRepo) BulkIndex(ctx context.Context, results []*checker.HealthResult) error {
    // Sử dụng Bulk API cho performance
    var buf bytes.Buffer
    
    for _, result := range results {
        // Action line
        meta := map[string]interface{}{
            "index": map[string]interface{}{
                "_index": r.index,
            },
        }
        metaJSON, _ := json.Marshal(meta)
        buf.Write(metaJSON)
        buf.WriteByte('\n')
        
        // Document line
        doc := map[string]interface{}{
            "server_id":       result.ServerID,
            "server_name":     result.ServerName,
            "status":          result.Status,
            "checked_at":      result.CheckedAt.Format(time.RFC3339),
            "response_time_ms": result.ResponseTimeMs,
            "check_method":    result.CheckMethod,
        }
        docJSON, _ := json.Marshal(doc)
        buf.Write(docJSON)
        buf.WriteByte('\n')
    }
    
    // Execute bulk request
    res, err := r.client.Bulk(
        bytes.NewReader(buf.Bytes()),
        r.client.Bulk.WithContext(ctx),
        r.client.Bulk.WithIndex(r.index),
    )
    if err != nil {
        return fmt.Errorf("bulk index failed: %w", err)
    }
    defer res.Body.Close()
    
    if res.IsError() {
        return fmt.Errorf("bulk index error: %s", res.String())
    }
    
    return nil
}
```

**Dependencies:**
```bash
cd monitor-service
go get github.com/elastic/go-elasticsearch/v8
go mod tidy
```

---

## 2.6. Kafka Consumer/Producer

> **Thư viện:** `github.com/segmentio/kafka-go` — pure Go, không CGo, API đơn giản, context-native.
> **Interface:** Giữ nguyên `Producer`/`Consumer` interface trong `shared/kafka/event.go`.
> **Implement:** `SegmentioProducer` (Writer) + `SegmentioConsumer` (Reader) trong `shared/kafka/`.

### Producer — Segmentio Writer

**File:** `shared/kafka/segmentio_producer.go`

```go
package kafka

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/segmentio/kafka-go"
    "github.com/rs/zerolog"
)

type SegmentioProducer struct {
    writer *kafka.Writer
    logger zerolog.Logger
}

type SegmentioProducerConfig struct {
    Brokers   []string
    Topic     string        // default topic (có thể override per message)
    BatchSize int           // default: 100
    BatchTimeout time.Duration // default: 100ms
    Async     bool          // default: false (sync)
}

func NewSegmentioProducer(cfg *SegmentioProducerConfig, logger zerolog.Logger) *SegmentioProducer {
    w := &kafka.Writer{
        Addr:         kafka.TCP(cfg.Brokers...),
        Balancer:     &kafka.LeastBytes{},
        BatchSize:    cfg.BatchSize,
        BatchTimeout: cfg.BatchTimeout,
        Async:        cfg.Async,
        RequiredAcks: kafka.RequireAll,
        Compression:  kafka.Snappy,
    }
    return &SegmentioProducer{writer: w, logger: logger}
}

func (p *SegmentioProducer) Publish(ctx context.Context, topic string, key string, value interface{}) error {
    jsonValue, _ := json.Marshal(value)
    err := p.writer.WriteMessages(ctx, kafka.Message{
        Topic: topic,
        Key:   []byte(key),
        Value: jsonValue,
        Time:  time.Now().UTC(),
    })
    if err != nil {
        p.logger.Error().Err(err).Str("topic", topic).Msg("Failed to publish")
    }
    return err
}

func (p *SegmentioProducer) Close() error {
    return p.writer.Close()
}
```

### Consumer — Segmentio Reader

**File:** `shared/kafka/segmentio_consumer.go`

```go
package kafka

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "time"

    "github.com/segmentio/kafka-go"
    "github.com/rs/zerolog"
)

type SegmentioConsumer struct {
    readers  map[string]*kafka.Reader // topic → reader
    handlers map[string]EventHandler
    mu       sync.RWMutex
    logger   zerolog.Logger
    groupID  string
    brokers  []string
    closed   bool
}

type SegmentioConsumerConfig struct {
    Brokers       []string
    GroupID       string
    MinBytes      int           // default: 10KB
    MaxBytes      int           // default: 1MB
    MaxWait       time.Duration // default: 1s
    CommitInterval time.Duration // default: 5s
}

func NewSegmentioConsumer(cfg *SegmentioConsumerConfig, logger zerolog.Logger) *SegmentioConsumer {
    return &SegmentioConsumer{
        readers:  make(map[string]*kafka.Reader),
        handlers: make(map[string]EventHandler),
        logger:   logger,
        groupID:  cfg.GroupID,
        brokers:  cfg.Brokers,
    }
}

func (c *SegmentioConsumer) Subscribe(topic, groupID string, handler EventHandler) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    r := kafka.NewReader(kafka.ReaderConfig{
        Brokers:        c.brokers,
        GroupID:        groupID,
        Topic:          topic,
        MinBytes:       10e3,  // 10KB
        MaxBytes:       10e6,  // 10MB
        MaxWait:        1 * time.Second,
        CommitInterval: 5 * time.Second,
        StartOffset:    kafka.LastOffset,
    })

    c.readers[topic] = r
    c.handlers[topic] = handler
    return nil
}

func (c *SegmentioConsumer) Start(ctx context.Context) error {
    var wg sync.WaitGroup
    for topic, reader := range c.readers {
        wg.Add(1)
        go func(t string, r *kafka.Reader) {
            defer wg.Done()
            for {
                select {
                case <-ctx.Done():
                    return
                default:
                    msg, err := r.ReadMessage(ctx)
                    if err != nil {
                        if ctx.Err() != nil { return }
                        c.logger.Error().Err(err).Str("topic", t).Msg("Read error")
                        continue
                    }
                    var event Event
                    if err := json.Unmarshal(msg.Value, &event); err != nil {
                        c.logger.Error().Err(err).Str("topic", t).Msg("Unmarshal error")
                        continue
                    }
                    if h, ok := c.handlers[t]; ok {
                        h(ctx, &event)
                    }
                }
            }
        }(topic, reader)
    }
    wg.Wait()
    return nil
}

func (c *SegmentioConsumer) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.closed = true
    for _, r := range c.readers {
        r.Close()
    }
    return nil
}
```

**Dependencies:**
```bash
cd shared
go get github.com/segmentio/kafka-go
go mod tidy
```

### Consumer business logic

**File:** `monitor-service/internal/service/event_consumer.go`

```go
// Khi Server Service tạo/xóa server → Monitor Service tự động tạo/disable health-check config

func (s *EventConsumer) HandleServerCreated(ctx context.Context, event *kafka.Event) error {
    data := event.Data.(map[string]interface{})
    serverID := data["server_id"].(string)
    
    // Tạo default health-check config cho server mới
    config := &model.HealthCheckConfig{
        ServerID:    serverID,
        CheckMethod: "tcp",
        TCPPort:     9000 + extractIndex(serverID),  // SRV-00001 → 9001
        UptimeRate:  s.cfg.DefaultUptimeRate,         // 0.95
        IsEnabled:   true,
    }
    return s.configRepo.Create(ctx, config)
}

func (s *EventConsumer) HandleServerDeleted(ctx context.Context, event *kafka.Event) error {
    data := event.Data.(map[string]interface{})
    serverID := data["server_id"].(string)
    
    // Disable health-check (không xóa, để giữ history)
    return s.configRepo.DisableByServerID(ctx, serverID)
}
```

### Consumer setup trong main.go:

```go
// Subscribe to topics
consumer.Subscribe("server.created", "monitor-group", eventConsumer.HandleServerCreated)
consumer.Subscribe("server.deleted", "monitor-group", eventConsumer.HandleServerDeleted)

---

## 2.7. Redis Integration

### Distributed Lock

```go
// Trong health_check_scheduler.go — đã implement ở Step 1 của runCycle
// Dùng Redis SET NX EX pattern
```

### Status Cache

```go
// Key:   server:status:{server_id}
// Value: "on" hoặc "off"
// TTL:   interval + 30s (đảm bảo overlap với cycle tiếp theo)
```

---

## 2.8. Health Check Config Repository

**File:** `monitor-service/internal/repository/config_repository.go`

```go
type HealthCheckConfigRepo interface {
    Create(ctx context.Context, config *model.HealthCheckConfig) error
    GetByServerID(ctx context.Context, serverID string) (*model.HealthCheckConfig, error)
    GetAllEnabled(ctx context.Context) ([]model.HealthCheckConfig, error)
    Update(ctx context.Context, config *model.HealthCheckConfig) error
    DisableByServerID(ctx context.Context, serverID string) error
}
```

---

## 2.9. Entry Point (main.go)

**File:** `monitor-service/cmd/main.go`

```go
func main() {
    // 1. Load config
    cfg := config.LoadConfig()
    log := logger.NewLogger("monitor-service", &cfg.Log)
    
    // 2. Connect DB (GORM — 2 connections: monitor_schema + cross-schema read)
    monitorDB := database.Connect(cfg.MonitorDB)  // monitor_user
    serverDB := database.Connect(cfg.ServerDB)     // monitor_user (có GRANT SELECT trên server_schema)
    
    // 3. Connect Redis
    rdb := redis.NewClient(cfg.Redis)
    
    // 4. Connect Elasticsearch
    esClient := elasticsearch.NewClient(cfg.ES)
    
    // 5. Connect Kafka
    producer := kafka.NewSegmentioProducer(&kafka.SegmentioProducerConfig{
        Brokers:   strings.Split(cfg.Kafka.Brokers, ","),
        BatchSize: 100,
        Async:     false,
    }, log)
    defer producer.Close()
    
    consumer := kafka.NewSegmentioConsumer(&kafka.SegmentioConsumerConfig{
        Brokers: strings.Split(cfg.Kafka.Brokers, ","),
        GroupID: "monitor-group",
    }, log)
    defer consumer.Close()
    
    // 6. Init repos
    configRepo := repository.NewConfigRepo(monitorDB)
    serverReader := repository.NewServerReader(serverDB)
    esRepo := repository.NewESStatusLogRepo(esClient, cfg.ES.IndexName)
    
    // 7. Init checker (TCP only — no factory needed)
    healthChecker := checker.NewTCPChecker(time.Duration(cfg.Monitor.TCPTimeout) * time.Millisecond)
    
    // 8. Init worker pool
    pool := worker.NewPool(cfg.Monitor.WorkerCount, healthChecker, log)
    
    // 9. Init scheduler
    scheduler := scheduler.NewHealthCheckScheduler(
        pool, serverReader, configRepo, esRepo, rdb, producer, log,
        time.Duration(cfg.Monitor.CheckInterval) * time.Second,
    )
    
    // 10. Init event consumer
    eventConsumer := service.NewEventConsumer(configRepo, cfg.Monitor, log)
    consumer.Subscribe("server.created", "monitor-group", eventConsumer.HandleServerCreated)
    consumer.Subscribe("server.deleted", "monitor-group", eventConsumer.HandleServerDeleted)
    
    // 11. Start scheduler in goroutine
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    go scheduler.Start(ctx)
    go consumer.Start(ctx)
    
    // 12. Optional: HTTP server (manual trigger, health endpoint)
    r := gin.Default()
    r.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok", "service": "monitor-service"})
    })
    
    // Graceful shutdown
    srv := &http.Server{Addr: ":8083", Handler: r}
    go srv.ListenAndServe()
    
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    
    cancel() // Stop scheduler + consumer
    srv.Shutdown(context.Background())
}
```

---

## 2.10. Unit Tests

### Test files & cases:

**`tcp-simulator/simulator/math_engine_test.go`** — 6 test cases (đã liệt kê ở 2.1.1)

**`tcp-simulator/simulator/manager_test.go`** — 4 test cases (đã liệt kê ở 2.1.2)

**`checker/tcp_checker_test.go`** — 5 test cases (đã liệt kê ở 2.2.2)

**`worker/pool_test.go`** — 4 test cases (đã liệt kê ở 2.3)

**`scheduler/health_check_scheduler_test.go`:**
```
✅ TestRunCycle_Success → full cycle: load → check → ES → PG → Kafka
✅ TestRunCycle_LockAlreadyHeld → skip cycle
✅ TestRunCycle_NoServers → empty list, no errors
✅ TestRunCycle_ESBulkFail → log error, continue
✅ TestRunCycle_StatusChanged → detect + publish events
✅ TestRunCycle_NoStatusChange → no events published
```

**`repository/es_repository_test.go`:**
```
✅ TestBulkIndex_Success → mock ES client
✅ TestBulkIndex_EmptyResults → no error
✅ TestBulkIndex_ESError → return error
```

**`service/event_consumer_test.go`:**
```
✅ TestHandleServerCreated → config created with defaults
✅ TestHandleServerDeleted → config disabled
```

---

## 2.11. End-to-End Verification

### Bước verify:

```bash
# 1. Đảm bảo có server data (từ Phase 1)
curl http://localhost:8080/api/v1/servers -H "Authorization: Bearer $TOKEN"

# 2. Start Monitor Service
cd monitor-service && go run cmd/main.go

# 3. Chờ 60s cho 1 cycle hoàn tất

# 4. Kiểm tra logs
tail -f logs/monitor/monitor-service.log
# Expected: "Health-check cycle completed" với total, on, off, changed

# 5. Kiểm tra Elasticsearch
curl "http://localhost:9200/server-status-logs/_count?pretty"
# Expected: count > 0

curl "http://localhost:9200/server-status-logs/_search?size=3&pretty"
# Expected: documents với server_id, status, checked_at

# 6. Kiểm tra server status đã cập nhật trong PG
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -c "SELECT server_id, status, updated_at FROM server_schema.servers LIMIT 10"

# 7. Kiểm tra Redis status cache
docker exec -it vcs-sms-redis redis-cli -a redis_secret KEYS "server:status:*"

# 8. Chờ thêm vài cycle → verify ES có nhiều documents hơn
```

---

## Deliverables Phase 2

| # | Deliverable | Verify |
|---|------------|--------|
| 1 | TCP Simulator (Math Engine + Manager) | Unit tests pass, ports open/close |
| 2 | TCPChecker (đúng 1 implementation, không có SimulatorChecker) | Unit tests pass |
| 3 | Worker Pool (100 goroutines) | Benchmark test |
| 4 | Cron scheduler (60s interval) | Logs show cycle completion |
| 5 | ES bulk index | `curl localhost:9200/server-status-logs/_count` > 0 |
| 6 | Status change detection | Kafka consumer logs show events |
| 7 | Distributed lock (Redis) | Only 1 cycle runs at a time |
| 8 | health_check_configs auto-create | Kafka consumer creates config on server.created |
| 9 | Unit tests | Coverage ≥ 90% |

---

> **Tiếp theo:** [Phase 3: Report Service →](./phase-3-report.md)
