# Phase 3: Report Service

> **Mục tiêu:** Tính toán uptime từ Elasticsearch, tạo báo cáo, gửi email qua Gmail SMTP.
> **Thời gian:** Tuần 4
> **Prerequisite:** Phase 2 hoàn tất (ES có dữ liệu health-check, Kafka events đang chạy)
> **Điểm đạt được:** 0.5 (Báo cáo định kỳ) + 0.5 (API Báo cáo chủ động)

---

## Checklist tổng quan Phase 3

- [ ] **3.1** Elasticsearch Uptime Calculator (aggregation queries)
- [ ] **3.2** Gmail SMTP Email Sender
- [ ] **3.3** HTML Email Template
- [ ] **3.4** Report Service (business logic)
- [ ] **3.5** Report Handler (HTTP endpoints)
- [ ] **3.6** Daily Report Scheduler (Cron)
- [ ] **3.7** Kafka Consumer (health batch events)
- [ ] **3.8** Redis Cache (report summary)
- [ ] **3.9** Entry Point (main.go)
- [ ] **3.10** Unit Tests
- [ ] **3.11** End-to-End Verification

---

## 3.1. Elasticsearch Uptime Calculator

**File:** `report-service/internal/repository/es_uptime_repository.go`

### 3.1.1. Interface

```go
type UptimeCalculator interface {
    // GetUptimeSummary tính toán uptime trung bình trong khoảng thời gian
    GetUptimeSummary(ctx context.Context, startDate, endDate time.Time) (*UptimeSummary, error)
    
    // GetLowUptimeServers trả về top N server có uptime thấp nhất
    GetLowUptimeServers(ctx context.Context, startDate, endDate time.Time, topN int) ([]ServerUptime, error)
}

type UptimeSummary struct {
    TotalServers    int     `json:"total_servers"`
    ServersOn       int     `json:"servers_on"`        // tại thời điểm check cuối
    ServersOff      int     `json:"servers_off"`
    AvgUptimePct    float64 `json:"avg_uptime_pct"`    // %
    TotalChecks     int64   `json:"total_checks"`
}

type ServerUptime struct {
    ServerID    string  `json:"server_id"`
    ServerName  string  `json:"server_name"`
    UptimePct   float64 `json:"uptime_pct"`
    TotalChecks int64   `json:"total_checks"`
    OnChecks    int64   `json:"on_checks"`
}
```

### 3.1.2. Implementation — ES Aggregation Query

```go
func (r *esUptimeRepo) GetUptimeSummary(ctx context.Context, startDate, endDate time.Time) (*UptimeSummary, error) {
    // Build aggregation query
    query := map[string]interface{}{
        "size": 0,
        "query": map[string]interface{}{
            "bool": map[string]interface{}{
                "filter": []interface{}{
                    map[string]interface{}{
                        "range": map[string]interface{}{
                            "checked_at": map[string]interface{}{
                                "gte": startDate.Format(time.RFC3339),
                                "lt":  endDate.Format(time.RFC3339),
                            },
                        },
                    },
                },
            },
        },
        "aggs": map[string]interface{}{
            "per_server": map[string]interface{}{
                "terms": map[string]interface{}{
                    "field": "server_id",
                    "size":  10000,
                },
                "aggs": map[string]interface{}{
                    "total_checks": map[string]interface{}{
                        "value_count": map[string]interface{}{
                            "field": "status",
                        },
                    },
                    "on_checks": map[string]interface{}{
                        "filter": map[string]interface{}{
                            "term": map[string]interface{}{
                                "status": "on",
                            },
                        },
                    },
                    "uptime_rate": map[string]interface{}{
                        "bucket_script": map[string]interface{}{
                            "buckets_path": map[string]interface{}{
                                "on":    "on_checks._count",
                                "total": "total_checks",
                            },
                            "script": "params.on / params.total * 100",
                        },
                    },
                },
            },
            "avg_uptime": map[string]interface{}{
                "avg_bucket": map[string]interface{}{
                    "buckets_path": "per_server>uptime_rate",
                },
            },
        },
    }
    
    // Execute query
    var buf bytes.Buffer
    json.NewEncoder(&buf).Encode(query)
    
    res, err := r.client.Search(
        r.client.Search.WithContext(ctx),
        r.client.Search.WithIndex(r.index),
        r.client.Search.WithBody(&buf),
    )
    // ... parse response ...
    
    return &UptimeSummary{
        TotalServers: len(perServerBuckets),
        ServersOn:    onCount,
        ServersOff:   offCount,
        AvgUptimePct: avgUptime,
        TotalChecks:  totalChecks,
    }, nil
}
```

### 3.1.3. GetLowUptimeServers — trả top N uptime thấp nhất

```go
func (r *esUptimeRepo) GetLowUptimeServers(ctx context.Context, startDate, endDate time.Time, topN int) ([]ServerUptime, error) {
    // Tương tự query trên, nhưng:
    // - Thêm sort trên uptime_rate ASC
    // - Limit size = topN
    // - Hoặc: lấy all → sort in-memory → take first N
    
    // Parse response → build []ServerUptime → sort → return topN
}
```

**Test cases (`es_uptime_repository_test.go`):**
```
✅ TestGetUptimeSummary_WithData → parse ES response correctly
✅ TestGetUptimeSummary_NoData → return zeros
✅ TestGetUptimeSummary_ESError → return error
✅ TestGetLowUptimeServers_Top10 → return sorted, 10 items
✅ TestGetLowUptimeServers_LessThanN → return all if < topN
```

---

## 3.2. Gmail SMTP Email Sender

**File:** `report-service/internal/email/smtp_sender.go`

### 3.2.1. Interface

```go
type EmailSender interface {
    SendHTML(ctx context.Context, to string, subject string, htmlBody string) error
}
```

### 3.2.2. Implementation

```go
type GmailSender struct {
    host     string // smtp.gmail.com
    port     int    // 587
    username string // your-email@gmail.com
    password string // App Password (16 chars)
    from     string // "VCS-SMS <your-email@gmail.com>"
}

func NewGmailSender(cfg *config.SMTPConfig) *GmailSender {
    return &GmailSender{
        host:     cfg.Host,
        port:     cfg.Port,
        username: cfg.Username,
        password: cfg.Password,
        from:     cfg.From,
    }
}

func (s *GmailSender) SendHTML(ctx context.Context, to, subject, htmlBody string) error {
    m := gomail.NewMessage()
    m.SetHeader("From", s.from)
    m.SetHeader("To", to)
    m.SetHeader("Subject", subject)
    m.SetBody("text/html", htmlBody)
    
    d := gomail.NewDialer(s.host, s.port, s.username, s.password)
    d.TLSConfig = &tls.Config{ServerName: s.host}
    
    if err := d.DialAndSend(m); err != nil {
        return fmt.Errorf("failed to send email: %w", err)
    }
    
    return nil
}
```

**Dependencies:**
```bash
cd report-service
go get gopkg.in/gomail.v2
```

**Test cases (`smtp_sender_test.go`):**
```
✅ TestSendHTML_Success → mock dialer
✅ TestSendHTML_ConnectionError → expect error
✅ TestSendHTML_AuthError → expect error
```

> **Lưu ý:** Unit test mock SMTP connection. Integration test gửi email thật chỉ chạy manual.

---

## 3.3. HTML Email Template

**File:** `report-service/internal/email/templates/daily_report.html`

```html
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 600px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 30px; text-align: center; }
        .header h1 { margin: 0; font-size: 24px; }
        .header p { margin: 10px 0 0; opacity: 0.9; }
        .content { padding: 30px; }
        .stats { display: flex; justify-content: space-around; margin: 20px 0; }
        .stat-card { text-align: center; padding: 15px; }
        .stat-value { font-size: 28px; font-weight: bold; }
        .stat-label { font-size: 12px; color: #666; margin-top: 5px; }
        .on { color: #27ae60; }
        .off { color: #e74c3c; }
        .uptime { color: #2980b9; }
        table { width: 100%; border-collapse: collapse; margin: 20px 0; }
        th { background: #f8f9fa; padding: 10px; text-align: left; border-bottom: 2px solid #dee2e6; }
        td { padding: 10px; border-bottom: 1px solid #dee2e6; }
        .footer { background: #f8f9fa; padding: 20px; text-align: center; font-size: 12px; color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>📊 Báo cáo trạng thái Server</h1>
            <p>{{.ReportDate}}</p>
        </div>
        <div class="content">
            <div class="stats">
                <div class="stat-card">
                    <div class="stat-value">{{.TotalServers}}</div>
                    <div class="stat-label">Tổng Server</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value on">{{.ServersOn}}</div>
                    <div class="stat-label">🟢 Online</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value off">{{.ServersOff}}</div>
                    <div class="stat-label">🔴 Offline</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value uptime">{{printf "%.2f" .AvgUptimePct}}%</div>
                    <div class="stat-label">📈 Avg Uptime</div>
                </div>
            </div>
            
            <h3>⚠️ Top {{len .LowUptimeServers}} Server có uptime thấp nhất</h3>
            <table>
                <thead>
                    <tr>
                        <th>Server ID</th>
                        <th>Server Name</th>
                        <th>Uptime</th>
                    </tr>
                </thead>
                <tbody>
                    {{range .LowUptimeServers}}
                    <tr>
                        <td>{{.ServerID}}</td>
                        <td>{{.ServerName}}</td>
                        <td>{{printf "%.2f" .UptimePct}}%</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
        <div class="footer">
            VCS Server Management System — Automated Report
        </div>
    </div>
</body>
</html>
```

### Template renderer:

**File:** `report-service/internal/email/template_renderer.go`

```go
func RenderDailyReport(data *ReportData) (string, error) {
    tmpl, err := template.ParseFiles("internal/email/templates/daily_report.html")
    if err != nil {
        return "", err
    }
    
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, data); err != nil {
        return "", err
    }
    
    return buf.String(), nil
}

type ReportData struct {
    ReportDate       string
    TotalServers     int
    ServersOn        int
    ServersOff       int
    AvgUptimePct     float64
    LowUptimeServers []ServerUptime
}
```

---

## 3.4. Report Service (Business Logic)

**File:** `report-service/internal/service/report_service.go`

### 3.4.1. Interface

```go
type ReportService interface {
    // GetSummary lấy summary cho khoảng thời gian (không gửi email)
    GetSummary(ctx context.Context, startDate, endDate time.Time) (*dto.ReportSummaryResponse, error)
    
    // SendReport tạo và gửi báo cáo email
    SendReport(ctx context.Context, req *dto.SendReportRequest) (*dto.SendReportResponse, error)
    
    // SendDailyReport gửi báo cáo cho ngày hôm trước (cron)
    SendDailyReport(ctx context.Context) error
}
```

### 3.4.2. GetSummary Logic

```
1. Hash (startDate + endDate) → check Redis cache
2. If cache hit → return cached data
3. If cache miss:
   a. Query ES: GetUptimeSummary(start, end)
   b. Query ES: GetLowUptimeServers(start, end, 10)
   c. Query PG: count total servers
   d. Build ReportSummaryResponse
   e. Cache in Redis (TTL=1h)
   f. Return
```

### 3.4.3. SendReport Logic (On-Demand API)

```
1. Validate input (dates, email format)
2. Create report_job in PG (status='processing')
3. Call GetSummary(startDate, endDate)
4. Render HTML email template
5. Send email via GmailSender
6. If success:
   a. Update report_job (status='completed', sent_at=now)
   b. Return success response with summary data
7. If fail:
   a. Update report_job (status='failed', error_message)
   b. Return error
```

### 3.4.4. SendDailyReport Logic (Cron)

```
1. Calculate yesterday (00:00 → 23:59:59)
2. Get admin email from config (SMTP_ADMIN_EMAIL)
3. Call SendReport with yesterday dates + admin email
4. Save daily_snapshot to PG for quick future access
5. Log result
```

---

## 3.5. Report Handler

**File:** `report-service/internal/handler/report_handler.go`

```go
type ReportHandler struct {
    service service.ReportService
}

// GET /api/v1/reports/summary?start_date=2026-06-01&end_date=2026-06-07
func (h *ReportHandler) GetSummary(c *gin.Context) {
    startDate, err := time.Parse("2006-01-02", c.Query("start_date"))
    endDate, err := time.Parse("2006-01-02", c.Query("end_date"))
    // Validate: end >= start, range <= 90 days
    
    summary, err := h.service.GetSummary(ctx, startDate, endDate)
    response.Success(c, 200, "Report summary retrieved", summary)
}

// POST /api/v1/reports
// Body: { "start_date": "2026-06-01", "end_date": "2026-06-07", "email": "admin@company.com" }
func (h *ReportHandler) SendReport(c *gin.Context) {
    var req dto.SendReportRequest
    c.ShouldBindJSON(&req)
    
    result, err := h.service.SendReport(ctx, &req)
    response.Success(c, 200, "Report sent successfully", result)
}
```

### DTOs

```go
// dto/request.go
type SendReportRequest struct {
    StartDate string `json:"start_date" binding:"required"` // "2006-01-02"
    EndDate   string `json:"end_date" binding:"required"`
    Email     string `json:"email" binding:"required,email"`
}

// dto/response.go
type ReportSummaryResponse struct {
    StartDate        string          `json:"start_date"`
    EndDate          string          `json:"end_date"`
    TotalServers     int             `json:"total_servers"`
    ServersOn        int             `json:"servers_on"`
    ServersOff       int             `json:"servers_off"`
    AvgUptimePct     float64         `json:"avg_uptime_pct"`
    LowUptimeServers []ServerUptime  `json:"low_uptime_servers"`
}

type SendReportResponse struct {
    ReportID string                `json:"report_id"`
    Status   string                `json:"status"`
    Message  string                `json:"message"`
    Summary  *ReportSummaryResponse `json:"summary"`
}
```

---

## 3.6. Daily Report Scheduler (Cron)

**File:** `report-service/internal/scheduler/daily_report_cron.go`

```go
type DailyReportCron struct {
    service  service.ReportService
    schedule string  // cron expression: "0 8 * * *"
    logger   zerolog.Logger
}

func (c *DailyReportCron) Start(ctx context.Context) {
    // Parse cron expression
    cronParser := cron.New(cron.WithSeconds())
    cronParser.AddFunc(c.schedule, func() {
        c.logger.Info().Msg("Daily report cron triggered")
        if err := c.service.SendDailyReport(ctx); err != nil {
            c.logger.Error().Err(err).Msg("Daily report failed")
        }
    })
    cronParser.Start()
    
    <-ctx.Done()
    cronParser.Stop()
}
```

**Dependency:**
```bash
go get github.com/robfig/cron/v3
```

---

## 3.7. — 3.8. Kafka Consumer + Redis Cache

### Kafka Consumer (optional tăng cường)

```go
// Consume server.health.batch → có thể dùng để real-time update dashboard (nếu cần)
// Trong scope hiện tại: Report Service chủ yếu query trực tiếp ES khi cần
```

### Redis Cache

```go
// Key:   report:summary:{startDate}:{endDate}
// Value: JSON của ReportSummaryResponse
// TTL:   1 hour

func (s *reportService) getCachedSummary(ctx context.Context, start, end time.Time) (*dto.ReportSummaryResponse, error) {
    key := fmt.Sprintf("report:summary:%s:%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
    cached, err := s.redis.Get(ctx, key).Result()
    if err == nil {
        var resp dto.ReportSummaryResponse
        json.Unmarshal([]byte(cached), &resp)
        return &resp, nil
    }
    return nil, err // cache miss
}
```

---

## 3.9. — 3.10. Unit Tests

### Test cases:

**`service/report_service_test.go`:**
```
✅ TestGetSummary_CacheHit → return cached data, no ES query
✅ TestGetSummary_CacheMiss → query ES, cache result, return
✅ TestGetSummary_ESError → return error
✅ TestSendReport_Success → report created, email sent, status=completed
✅ TestSendReport_EmailFail → report status=failed, error returned
✅ TestSendReport_InvalidDates → validation error
✅ TestSendDailyReport_Success → yesterday dates, admin email
```

**`handler/report_handler_test.go`:**
```
✅ TestGetSummaryHandler_ValidDates → 200
✅ TestGetSummaryHandler_MissingDates → 400
✅ TestGetSummaryHandler_InvalidDateFormat → 400
✅ TestSendReportHandler_ValidRequest → 200
✅ TestSendReportHandler_InvalidEmail → 400
```

**`email/smtp_sender_test.go`** — 3 test cases (đã liệt kê ở 3.2)

**`email/template_renderer_test.go`:**
```
✅ TestRenderDailyReport_Success → valid HTML output
✅ TestRenderDailyReport_EmptyServers → no table rows, no error
```

---

## 3.11. End-to-End Verification

```bash
# 1. Đảm bảo ES có dữ liệu (Monitor Service đã chạy vài giờ)
curl "http://localhost:9200/server-status-logs/_count?pretty"

# 2. Start Report Service
cd report-service && go run cmd/main.go

# 3. Test GET summary (qua Gateway)
curl "http://localhost:8080/api/v1/reports/summary?start_date=2026-06-07&end_date=2026-06-08" \
  -H "Authorization: Bearer $TOKEN"
# Expected: JSON với total_servers, servers_on, avg_uptime_pct, low_uptime_servers

# 4. Test POST send report
curl -X POST http://localhost:8080/api/v1/reports \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"start_date":"2026-06-07","end_date":"2026-06-08","email":"your-real-email@gmail.com"}'
# Expected: 200 OK + email arrives in inbox

# 5. Kiểm tra inbox email → verify nội dung báo cáo HTML

# 6. Kiểm tra report_jobs trong DB
docker exec -it vcs-sms-postgres psql -U report_user -d vcs_sms \
  -c "SELECT id, report_type, status, sent_at FROM report_schema.report_jobs"
```

---

## Deliverables Phase 3

| # | Deliverable | Verify |
|---|------------|--------|
| 1 | ES Uptime Calculator | Unit tests with mock ES |
| 2 | Gmail SMTP Sender | Email arrives in real inbox |
| 3 | HTML Email Template | Professional-looking email |
| 4 | GET /reports/summary | Returns correct uptime data |
| 5 | POST /reports (on-demand) | Sends email + returns summary |
| 6 | Daily cron job | Logs show daily trigger, email sent |
| 7 | Redis cache for summaries | Cache hit on repeated queries |
| 8 | report_jobs tracking | DB records for each report |
| 9 | Unit tests | Coverage ≥ 90% |

---

> **Tiếp theo:** [Phase 4: File I/O Service →](./phase-4-fileio.md)
