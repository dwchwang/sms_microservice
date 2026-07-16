# Phase 4: File I/O Service

> **Mục tiêu:** Import server từ Excel (async via Kafka), Export server ra Excel.
> **Thời gian:** Tuần 5
> **Prerequisite:** Phase 1 hoàn tất (server_schema.servers table có data)
> **Điểm đạt được:** 0.5 (Import) + 0.5 (Export)

---

## Checklist tổng quan Phase 4

- [ ] **4.1** Excel Parser (import .xlsx → []Server)
- [ ] **4.2** Excel Generator ([]Server → .xlsx)
- [ ] **4.3** Import Service — Async Flow (Kafka)
- [ ] **4.4** Export Service — Sync Stream
- [ ] **4.5** Import/Export Handlers
- [ ] **4.6** Import Job Repository (tracking)
- [ ] **4.7** Kafka Integration (produce/consume import jobs)
- [ ] **4.8** Entry Point (main.go)
- [ ] **4.9** Unit Tests
- [ ] **4.10** End-to-End Verification

---

## 4.1. Excel Parser (Import)

**File:** `fileio-service/internal/excel/parser.go`

### 4.1.1. Interface

```go
type ExcelParser interface {
    // Parse đọc file .xlsx và trả về danh sách server records
    Parse(filePath string) ([]ParsedRow, error)
    // ValidateHeaders kiểm tra header row có đúng format
    ValidateHeaders(filePath string) error
}

type ParsedRow struct {
    RowNumber   int
    ServerID    string
    ServerName  string
    IPv4        string
    OS          string
    CPUCores    *int
    RAMGB       *float64
    DiskGB      *float64
    Location    string
    Description string
    IsValid     bool
    ErrorMsg    string  // lý do nếu invalid
}
```

### 4.1.2. Implementation

```go
type excelParser struct{}

func NewExcelParser() ExcelParser {
    return &excelParser{}
}

// Expected headers (row 1):
// A: server_id | B: server_name | C: ipv4 | D: os | E: cpu_cores | F: ram_gb | G: disk_gb | H: location | I: description

var expectedHeaders = []string{
    "server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description",
}

func (p *excelParser) Parse(filePath string) ([]ParsedRow, error) {
    f, err := excelize.OpenFile(filePath)
    if err != nil {
        return nil, fmt.Errorf("cannot open excel file: %w", err)
    }
    defer f.Close()
    
    sheet := f.GetSheetName(0) // First sheet
    rows, err := f.GetRows(sheet)
    if err != nil {
        return nil, err
    }
    
    if len(rows) < 2 {
        return nil, fmt.Errorf("file must have at least a header row and one data row")
    }
    
    // Validate headers
    if err := p.validateHeaders(rows[0]); err != nil {
        return nil, err
    }
    
    // Parse data rows (skip header)
    var parsed []ParsedRow
    for i := 1; i < len(rows); i++ {
        row := rows[i]
        pr := ParsedRow{
            RowNumber: i + 1, // 1-indexed for user display
            IsValid:   true,
        }
        
        // Extract & validate fields
        if len(row) > 0 { pr.ServerID = strings.TrimSpace(row[0]) }
        if len(row) > 1 { pr.ServerName = strings.TrimSpace(row[1]) }
        if len(row) > 2 { pr.IPv4 = strings.TrimSpace(row[2]) }
        if len(row) > 3 { pr.OS = strings.TrimSpace(row[3]) }
        if len(row) > 4 { pr.CPUCores = parseIntPtr(row[4]) }
        if len(row) > 5 { pr.RAMGB = parseFloatPtr(row[5]) }
        if len(row) > 6 { pr.DiskGB = parseFloatPtr(row[6]) }
        if len(row) > 7 { pr.Location = strings.TrimSpace(row[7]) }
        if len(row) > 8 { pr.Description = strings.TrimSpace(row[8]) }
        
        // Validate required fields
        if pr.ServerID == "" {
            pr.IsValid = false
            pr.ErrorMsg = "server_id is required"
        } else if pr.ServerName == "" {
            pr.IsValid = false
            pr.ErrorMsg = "server_name is required"
        } else if pr.IPv4 == "" || !isValidIPv4(pr.IPv4) {
            pr.IsValid = false
            pr.ErrorMsg = "invalid or missing ipv4"
        }
        
        parsed = append(parsed, pr)
    }
    
    return parsed, nil
}
```

**Dependencies:**
```bash
cd fileio-service
go get github.com/xuri/excelize/v2
go mod tidy
```

**Test cases (`parser_test.go`):**
```
✅ TestParse_ValidFile → all rows parsed correctly
✅ TestParse_InvalidHeaders → error returned
✅ TestParse_EmptyFile → error "must have at least..."
✅ TestParse_MissingRequiredFields → rows marked invalid
✅ TestParse_InvalidIPv4 → row marked invalid
✅ TestParse_OptionalFieldsMissing → row still valid
✅ TestParse_ExtraColumns → ignored gracefully
✅ TestParse_FileNotFound → error returned
```

---

## 4.2. Excel Generator (Export)

**File:** `fileio-service/internal/excel/generator.go`

### 4.2.1. Interface

```go
type ExcelGenerator interface {
    // Generate tạo file .xlsx từ danh sách servers
    Generate(servers []ServerExportRow) (*bytes.Buffer, error)
}

type ServerExportRow struct {
    ServerID    string
    ServerName  string
    Status      string
    IPv4        string
    OS          string
    CPUCores    *int
    RAMGB       *float64
    DiskGB      *float64
    Location    string
    Description string
    CreatedAt   string
    UpdatedAt   string
}
```

### 4.2.2. Implementation

```go
func (g *excelGenerator) Generate(servers []ServerExportRow) (*bytes.Buffer, error) {
    f := excelize.NewFile()
    sheet := "Servers"
    f.SetSheetName("Sheet1", sheet)
    
    // Headers with styling
    headers := []string{
        "Server ID", "Server Name", "Status", "IPv4", "OS",
        "CPU Cores", "RAM (GB)", "Disk (GB)", "Location", "Description",
        "Created At", "Updated At",
    }
    
    // Write header row with style
    headerStyle, _ := f.NewStyle(&excelize.Style{
        Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
        Fill:      excelize.Fill{Type: "pattern", Color: []string{"4472C4"}, Pattern: 1},
        Alignment: &excelize.Alignment{Horizontal: "center"},
        Border:    []excelize.Border{{Type: "bottom", Color: "000000", Style: 2}},
    })
    
    for col, header := range headers {
        cell, _ := excelize.CoordinatesToCellName(col+1, 1)
        f.SetCellValue(sheet, cell, header)
        f.SetCellStyle(sheet, cell, cell, headerStyle)
    }
    
    // Write data rows
    for i, srv := range servers {
        row := i + 2 // data starts at row 2
        f.SetCellValue(sheet, cellName(1, row), srv.ServerID)
        f.SetCellValue(sheet, cellName(2, row), srv.ServerName)
        f.SetCellValue(sheet, cellName(3, row), srv.Status)
        f.SetCellValue(sheet, cellName(4, row), srv.IPv4)
        f.SetCellValue(sheet, cellName(5, row), srv.OS)
        if srv.CPUCores != nil { f.SetCellValue(sheet, cellName(6, row), *srv.CPUCores) }
        if srv.RAMGB != nil { f.SetCellValue(sheet, cellName(7, row), *srv.RAMGB) }
        if srv.DiskGB != nil { f.SetCellValue(sheet, cellName(8, row), *srv.DiskGB) }
        f.SetCellValue(sheet, cellName(9, row), srv.Location)
        f.SetCellValue(sheet, cellName(10, row), srv.Description)
        f.SetCellValue(sheet, cellName(11, row), srv.CreatedAt)
        f.SetCellValue(sheet, cellName(12, row), srv.UpdatedAt)
    }
    
    // Auto-fit columns
    for col := 1; col <= len(headers); col++ {
        colName, _ := excelize.ColumnNumberToName(col)
        f.SetColWidth(sheet, colName, colName, 18)
    }
    
    // Write to buffer
    var buf bytes.Buffer
    if err := f.Write(&buf); err != nil {
        return nil, fmt.Errorf("failed to write excel: %w", err)
    }
    
    return &buf, nil
}
```

**Test cases (`generator_test.go`):**
```
✅ TestGenerate_ValidData → buffer not empty, valid xlsx
✅ TestGenerate_EmptyList → file with only headers
✅ TestGenerate_NilOptionalFields → no panic, cells empty
✅ TestGenerate_LargeDataset → 10000 rows, no error
✅ TestGenerate_VerifyHeaders → correct header names
```

---

## 4.3. Import Service — Async Flow

**File:** `fileio-service/internal/service/import_service.go`

### 4.3.1. Interface

```go
type ImportService interface {
    // InitiateImport validate file + tạo job + publish Kafka event
    InitiateImport(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error)
    
    // ProcessImportJob consumer handler — xử lý import bất đồng bộ
    ProcessImportJob(ctx context.Context, jobID string) error
    
    // GetImportJobStatus lấy trạng thái import job
    GetImportJobStatus(ctx context.Context, jobID string) (*dto.ImportJobDetailResponse, error)
}
```

### 4.3.2. InitiateImport Logic

```
1. Validate file extension (.xlsx)
2. Validate file size (max 10MB)
3. Generate job_id (UUID)
4. Save file to /uploads/{job_id}.xlsx
5. Create import_job record in DB (status='pending')
6. Publish "import.job.created" to Kafka {job_id, file_path, user_id}
7. Return 202 Accepted {job_id, status: "pending"}
```

### 4.3.3. ProcessImportJob Logic (Kafka consumer)

```
1. Update job status → 'processing', started_at = now
2. Parse Excel file (ExcelParser)
3. If parse error → update job status='failed', return

4. For each ParsedRow:
   a. If row invalid (validation) → save to import_job_details (status='failed', reason)
   b. If row valid:
      i.   Query server_schema.servers: CHECK server_id exists
      ii.  Query server_schema.servers: CHECK server_name exists
      iii. If duplicate → save to details (status='failed', reason='duplicate server_id/name')
      iv.  If unique → INSERT into server_schema.servers
                     → save to details (status='success')
                     → Publish "server.created" to Kafka

5. Update import_job:
   - status = 'completed'
   - total_rows = count(all rows)
   - success_count = count(success)
   - failed_count = count(failed)
   - completed_at = now

6. Invalidate server list cache (Redis)
```

### 4.3.4. GetImportJobStatus Logic

```
1. Query import_job by job_id
2. If not found → 404
3. Query import_job_details by job_id (optional: paginated)
4. Build response:
   - Job status, counts
   - success_list: [{server_id, server_name}]
   - failed_list: [{server_id, server_name, row_number, reason}]
5. Return
```

---

## 4.4. Export Service — Sync Stream

**File:** `fileio-service/internal/service/export_service.go`

### 4.4.1. Interface

```go
type ExportService interface {
    // ExportServers query servers từ DB theo filter, generate Excel, trả về buffer
    ExportServers(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error)
    // Returns: (excel_buffer, filename, error)
}
```

### 4.4.2. Logic

```
1. Validate filter params
2. Query server_schema.servers (cross-schema read) với filter, sort
   - Không phân trang — export tất cả matching results (max 50.000)
3. Map results → []ServerExportRow
4. Generate Excel (ExcelGenerator)
5. Generate filename: "servers_export_20260608_103045.xlsx"
6. Return buffer + filename
```

---

## 4.5. Import/Export Handlers

**File:** `fileio-service/internal/handler/import_handler.go`

```go
// POST /api/v1/servers/import
func (h *ImportHandler) ImportServers(c *gin.Context) {
    // 1. Get uploaded file
    file, header, err := c.Request.FormFile("file")
    if err != nil {
        response.Error(c, 400, "File is required")
        return
    }
    defer file.Close()
    
    // 2. Get user_id from header (injected by Gateway)
    userID := c.GetHeader("X-User-ID")
    
    // 3. Call service
    result, err := h.service.InitiateImport(c.Request.Context(), file, header, userID)
    if err != nil {
        handleError(c, err)
        return
    }
    
    // 4. Return 202 Accepted
    response.Success(c, 202, "Import job created", result)
}

// GET /api/v1/servers/import/:job_id
func (h *ImportHandler) GetImportStatus(c *gin.Context) {
    jobID := c.Param("job_id")
    
    result, err := h.service.GetImportJobStatus(c.Request.Context(), jobID)
    if err != nil {
        handleError(c, err)
        return
    }
    
    response.Success(c, 200, "Import job status", result)
}
```

**File:** `fileio-service/internal/handler/export_handler.go`

```go
// POST /api/v1/servers/export
func (h *ExportHandler) ExportServers(c *gin.Context) {
    var filter dto.ExportFilter
    if err := c.ShouldBindJSON(&filter); err != nil {
        response.Error(c, 400, "Invalid filter parameters")
        return
    }
    
    buf, filename, err := h.service.ExportServers(c.Request.Context(), &filter)
    if err != nil {
        handleError(c, err)
        return
    }
    
    // Stream Excel file as download
    c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
    c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
    c.Header("Content-Length", strconv.Itoa(buf.Len()))
    c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}
```

### DTOs

```go
// dto/request.go
type ExportFilter struct {
    Status     string `json:"status,omitempty"`
    ServerName string `json:"server_name,omitempty"`
    IPv4       string `json:"ipv4,omitempty"`
    Location   string `json:"location,omitempty"`
    SortBy     string `json:"sort_by,omitempty"`
    SortOrder  string `json:"sort_order,omitempty"`
}

// dto/response.go
type ImportJobResponse struct {
    JobID  string `json:"job_id"`
    Status string `json:"status"`  // "pending"
}

type ImportJobDetailResponse struct {
    JobID        string              `json:"job_id"`
    Status       string              `json:"status"`
    TotalRows    int                 `json:"total_rows"`
    SuccessCount int                 `json:"success_count"`
    FailedCount  int                 `json:"failed_count"`
    SuccessList  []ImportRowResult   `json:"success_list"`
    FailedList   []ImportRowResult   `json:"failed_list"`
    CreatedAt    string              `json:"created_at"`
    CompletedAt  *string             `json:"completed_at,omitempty"`
}

type ImportRowResult struct {
    RowNumber  int    `json:"row_number"`
    ServerID   string `json:"server_id"`
    ServerName string `json:"server_name"`
    Status     string `json:"status"`       // "success" or "failed"
    Reason     string `json:"reason,omitempty"`
}
```

---

## 4.6. — 4.8. Repository, Kafka, main.go

### Import Job Repository

```go
type ImportJobRepo interface {
    Create(ctx context.Context, job *model.ImportJob) error
    FindByID(ctx context.Context, jobID string) (*model.ImportJob, error)
    UpdateStatus(ctx context.Context, jobID, status string, counts ...int) error
    SaveDetail(ctx context.Context, detail *model.ImportJobDetail) error
    SaveDetailsBatch(ctx context.Context, details []model.ImportJobDetail) error
    GetDetailsByJobID(ctx context.Context, jobID string) ([]model.ImportJobDetail, error)
}
```

### Kafka — Import Job Consumer

```go
// Trong main.go:
consumer.Subscribe("import.job.created", "fileio-group", func(ctx context.Context, event *kafka.Event) error {
    data := event.Data.(map[string]interface{})
    jobID := data["job_id"].(string)
    return importService.ProcessImportJob(ctx, jobID)
})
```

### main.go setup

```go
func main() {
    cfg := config.LoadConfig()
    log := logger.NewLogger("fileio-service", &cfg.Log)
    
    // DB connections
    fileioDb := database.Connect(cfg.FileIODB)   // fileio_schema
    serverDb := database.Connect(cfg.ServerDB)    // cross-schema access
    
    // Kafka
    producer := kafka.NewProducer(cfg.Kafka.Brokers)
    consumer := kafka.NewConsumer(cfg.Kafka.Brokers)
    
    // Redis (for cache invalidation)
    rdb := redis.NewClient(cfg.Redis)
    
    // Init services
    parser := excel.NewExcelParser()
    generator := excel.NewExcelGenerator()
    importJobRepo := repository.NewImportJobRepo(fileioDb)
    serverWriter := repository.NewServerWriter(serverDb)
    
    importSvc := service.NewImportService(importJobRepo, serverWriter, parser, producer, rdb, cfg, log)
    exportSvc := service.NewExportService(serverWriter, generator, log)
    
    // Handlers
    importHandler := handler.NewImportHandler(importSvc)
    exportHandler := handler.NewExportHandler(exportSvc)
    
    // Kafka consumer
    consumer.Subscribe("import.job.created", "fileio-group", importSvc.ProcessImportJobHandler)
    go consumer.Start(ctx)
    
    // Gin router
    r := gin.Default()
    r.POST("/api/v1/servers/import", importHandler.ImportServers)
    r.GET("/api/v1/servers/import/:job_id", importHandler.GetImportStatus)
    r.POST("/api/v1/servers/export", exportHandler.ExportServers)
    
    r.Run(":8085")
}
```

---

## 4.9. Unit Tests

### Test cases:

**`excel/parser_test.go`** — 8 test cases (đã liệt kê ở 4.1)

**`excel/generator_test.go`** — 5 test cases (đã liệt kê ở 4.2)

**`service/import_service_test.go`:**
```
✅ TestInitiateImport_ValidFile → job created, kafka published
✅ TestInitiateImport_InvalidExtension → 400 error
✅ TestInitiateImport_FileTooLarge → 400 error
✅ TestProcessImportJob_AllSuccess → success_count = total_rows
✅ TestProcessImportJob_SomeDuplicates → mixed success/failed
✅ TestProcessImportJob_AllDuplicates → failed_count = total_rows
✅ TestProcessImportJob_InvalidRows → rows with bad data marked failed
✅ TestProcessImportJob_ParseError → job status='failed'
✅ TestGetImportJobStatus_Found → return full detail
✅ TestGetImportJobStatus_NotFound → 404
✅ TestGetImportJobStatus_Pending → status still pending
```

**`service/export_service_test.go`:**
```
✅ TestExportServers_NoFilter → all servers exported
✅ TestExportServers_WithFilter → filtered servers
✅ TestExportServers_EmptyResult → file with headers only
✅ TestExportServers_LargeDataset → 10000 servers
```

**`handler/import_handler_test.go`:**
```
✅ TestImportHandler_ValidFile → 202 Accepted
✅ TestImportHandler_NoFile → 400
✅ TestGetImportStatusHandler_Found → 200
✅ TestGetImportStatusHandler_NotFound → 404
```

**`handler/export_handler_test.go`:**
```
✅ TestExportHandler_ValidFilter → 200 + Content-Disposition header
✅ TestExportHandler_InvalidFilter → 400
```

---

## 4.10. End-to-End Verification

### Tạo test Excel file:

Tạo file `test_import.xlsx` với nội dung:

| server_id | server_name | ipv4 | os | cpu_cores | ram_gb | disk_gb | location | description |
|-----------|-------------|------|----|-----------|--------|---------|----------|-------------|
| SRV-IMP-001 | import-web-01 | 10.0.1.1 | Ubuntu 22.04 | 4 | 8 | 200 | DC-HN | Web server |
| SRV-IMP-002 | import-db-01 | 10.0.1.2 | CentOS 9 | 16 | 64 | 2000 | DC-HCM | Database |
| SRV-001 | existing-name | 10.0.1.3 | | | | | | Duplicate test |

### Test commands:

```bash
# 1. Import
curl -X POST http://localhost:8080/api/v1/servers/import \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@test_import.xlsx"
# Expected: 202 {job_id: "uuid", status: "pending"}

# 2. Wait 5-10 seconds, then check status
curl http://localhost:8080/api/v1/servers/import/{job_id} \
  -H "Authorization: Bearer $TOKEN"
# Expected: status=completed, success_count=2, failed_count=1

# 3. Export (all servers)
curl -X POST http://localhost:8080/api/v1/servers/export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}' \
  --output exported_servers.xlsx
# Expected: File downloaded, open in Excel to verify

# 4. Export (filtered)
curl -X POST http://localhost:8080/api/v1/servers/export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"on","sort_by":"server_name","sort_order":"asc"}' \
  --output exported_on_servers.xlsx
```

---

## Deliverables Phase 4

| # | Deliverable | Verify |
|---|------------|--------|
| 1 | Excel Parser | Reads .xlsx correctly |
| 2 | Excel Generator | Creates valid .xlsx with styling |
| 3 | Import (async) | Upload → Kafka → process → track status |
| 4 | Import duplicate handling | Existing server_id/name skipped |
| 5 | Import status API | Returns success/failed lists |
| 6 | Export (sync) | Downloads .xlsx with filter/sort |
| 7 | Unit tests | Coverage ≥ 90% |

---

> **Tiếp theo:** [Phase 5: Polish & Documentation →](./phase-5-polish.md)
