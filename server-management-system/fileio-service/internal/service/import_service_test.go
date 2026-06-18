package service

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/fileio-service/config"
	"github.com/vcs-sms/fileio-service/internal/excel"
	"github.com/vcs-sms/fileio-service/internal/model"
	"github.com/vcs-sms/fileio-service/internal/repository/mocks"
	"github.com/vcs-sms/shared/kafka"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

type publishedEvent struct {
	topic string
	key   string
	event kafka.Event
}

// fakeProducer is a mock Kafka producer for testing.
type fakeProducer struct {
	publishedEvents []publishedEvent
	failTopic       map[string]error
}

func (p *fakeProducer) Publish(ctx context.Context, topic, key string, value interface{}) error {
	if err, ok := p.failTopic[topic]; ok {
		return err
	}
	event, ok := value.(*kafka.Event)
	if ok {
		p.publishedEvents = append(p.publishedEvents, publishedEvent{topic: topic, key: key, event: *event})
	}
	return nil
}

func (p *fakeProducer) Close() error {
	return nil
}

type fakeCache struct {
	patterns []string
}

func (c *fakeCache) DeleteByPattern(ctx context.Context, pattern string) {
	c.patterns = append(c.patterns, pattern)
}

type fakeExcelParser struct {
	parseFunc           func(filePath string) ([]excel.ParsedRow, error)
	validateHeadersFunc func(filePath string) error
}

func (p *fakeExcelParser) Parse(filePath string) ([]excel.ParsedRow, error) {
	if p.parseFunc != nil {
		return p.parseFunc(filePath)
	}
	return nil, nil
}

func (p *fakeExcelParser) ValidateHeaders(filePath string) error {
	if p.validateHeadersFunc != nil {
		return p.validateHeadersFunc(filePath)
	}
	return nil
}

// createTestMultipartFile creates a multipart form file from bytes.
func createTestMultipartFile(t *testing.T, filename string, content []byte) (multipart.File, *multipart.FileHeader) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")

	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatalf("failed to create form part: %v", err)
	}
	part.Write(content)
	w.Close()

	req, err := http.NewRequest("POST", "/", &buf)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	file, header, err := req.FormFile("file")
	if err != nil {
		t.Fatalf("failed to get form file: %v", err)
	}

	return file, header
}

func setupTestConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{
			UploadDir:     os.TempDir(),
			MaxFileSizeMB: 10,
		},
	}
}

func TestNewImportService_WithRedisClient(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	defer rdb.Close()

	svc := NewImportService(&mocks.ImportJobRepoMock{}, &mocks.ServerWriterMock{}, excel.NewExcelParser(), &fakeProducer{}, rdb, setupTestConfig(), zerolog.New(nil))
	if svc.(*importServiceImpl).cache == nil {
		t.Fatal("expected cache invalidator")
	}
}

func TestRedisCacheInvalidator_DeleteByPattern_NoServer(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	invalidator := &redisCacheInvalidator{client: rdb}
	invalidator.DeleteByPattern(ctx, "servers:list:*")
}

func createTestXLSX(t *testing.T, headers []string, rows [][]string) string {
	t.Helper()

	f := excelize.NewFile()
	sheet := "Sheet1"
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}
	for rowIdx, row := range rows {
		for colIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			f.SetCellValue(sheet, cell, val)
		}
	}

	tmpFile, err := os.CreateTemp("", "test-import-*.xlsx")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := f.Write(tmpFile); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to write xlsx: %v", err)
	}
	tmpFile.Close()
	return tmpFile.Name()
}

func TestInitiateImport_ValidFile(t *testing.T) {
	// Create a minimal valid xlsx
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	xlsxData, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read test xlsx: %v", err)
	}

	file, header := createTestMultipartFile(t, "servers.xlsx", xlsxData)
	defer file.Close()

	jobRepo := &mocks.ImportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ImportJob) error {
			return nil
		},
	}
	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{}

	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	result, err := svc.InitiateImport(context.Background(), file, header, "user-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", result.Status)
	}
	if result.FileName != "servers.xlsx" {
		t.Errorf("expected filename 'servers.xlsx', got %q", result.FileName)
	}
	if _, err := uuid.Parse(result.JobID); err != nil {
		t.Errorf("invalid job_id: %v", err)
	}
	if len(producer.publishedEvents) != 1 || producer.publishedEvents[0].topic != "import.job.created" {
		t.Fatalf("expected import.job.created event, got %#v", producer.publishedEvents)
	}
}

func TestInitiateImport_OnlyValidatesHeadersBeforeQueue(t *testing.T) {
	file, header := createTestMultipartFile(t, "servers.xlsx", []byte("xlsx bytes"))
	defer file.Close()

	parser := &fakeExcelParser{
		parseFunc: func(filePath string) ([]excel.ParsedRow, error) {
			t.Fatal("InitiateImport should not parse all rows before queuing the job")
			return nil, nil
		},
		validateHeadersFunc: func(filePath string) error {
			return nil
		},
	}
	producer := &fakeProducer{}
	jobRepo := &mocks.ImportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ImportJob) error {
			return nil
		},
	}

	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, parser, producer, nil, setupTestConfig(), zerolog.New(nil))

	result, err := svc.InitiateImport(context.Background(), file, header, "user-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "pending" || len(producer.publishedEvents) != 1 {
		t.Fatalf("expected queued pending job, got result=%#v events=%#v", result, producer.publishedEvents)
	}
}

func TestInitiateImport_CreateJobError(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	xlsxData, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read test xlsx: %v", err)
	}

	file, header := createTestMultipartFile(t, "servers.xlsx", xlsxData)
	defer file.Close()

	jobRepo := &mocks.ImportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ImportJob) error {
			return fmt.Errorf("create job failed")
		},
	}
	producer := &fakeProducer{}
	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, excel.NewExcelParser(), producer, nil, setupTestConfig(), zerolog.New(nil))

	_, err = svc.InitiateImport(context.Background(), file, header, uuid.New().String())
	if err == nil {
		t.Fatal("expected create job error")
	}
	if len(producer.publishedEvents) != 0 {
		t.Fatalf("expected no Kafka events after create failure, got %d", len(producer.publishedEvents))
	}
}

func TestInitiateImport_PublishFailureMarksJobFailed(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	xlsxData, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read test xlsx: %v", err)
	}

	file, header := createTestMultipartFile(t, "servers.xlsx", xlsxData)
	defer file.Close()

	failedUpdated := false
	jobRepo := &mocks.ImportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ImportJob) error {
			return nil
		},
		UpdateFailedFunc: func(ctx context.Context, id string, errMsg string) error {
			failedUpdated = true
			return nil
		},
	}
	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{failTopic: map[string]error{"import.job.created": fmt.Errorf("kafka unavailable")}}

	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	_, err = svc.InitiateImport(context.Background(), file, header, "user-123")
	if err == nil {
		t.Fatal("expected queue error")
	}
	if !failedUpdated {
		t.Fatal("expected import job to be marked failed")
	}
}

func TestInitiateImport_InvalidExtension(t *testing.T) {
	file, header := createTestMultipartFile(t, "servers.txt", []byte("not xlsx"))
	defer file.Close()

	jobRepo := &mocks.ImportJobRepoMock{}
	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{}

	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	_, err := svc.InitiateImport(context.Background(), file, header, "user-123")
	if err == nil {
		t.Fatal("expected error for invalid extension")
	}
}

func TestInitiateImport_RejectsLegacyXLS(t *testing.T) {
	file, header := createTestMultipartFile(t, "servers.xls", []byte("not xlsx"))
	defer file.Close()

	jobRepo := &mocks.ImportJobRepoMock{}
	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{}

	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	_, err := svc.InitiateImport(context.Background(), file, header, "user-123")
	if err == nil {
		t.Fatal("expected error for legacy .xls extension")
	}
}

func TestInitiateImport_FileTooLarge(t *testing.T) {
	// Create a file larger than 1MB (but our max is small for test)
	largeData := make([]byte, 2*1024*1024) // 2MB
	file, header := createTestMultipartFile(t, "servers.xlsx", largeData)
	defer file.Close()

	jobRepo := &mocks.ImportJobRepoMock{}
	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{}

	cfg := &config.Config{
		App: config.AppConfig{
			UploadDir:     os.TempDir(),
			MaxFileSizeMB: 1, // 1MB max
		},
	}
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	_, err := svc.InitiateImport(context.Background(), file, header, "user-123")
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}

func TestInitiateImport_HeaderValidationError(t *testing.T) {
	file, header := createTestMultipartFile(t, "servers.xlsx", []byte("xlsx bytes"))
	defer file.Close()

	createCalled := false
	jobRepo := &mocks.ImportJobRepoMock{
		CreateFunc: func(ctx context.Context, job *model.ImportJob) error {
			createCalled = true
			return nil
		},
	}
	parser := &fakeExcelParser{
		validateHeadersFunc: func(filePath string) error {
			return fmt.Errorf("bad headers")
		},
	}
	producer := &fakeProducer{}

	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, parser, producer, nil, setupTestConfig(), zerolog.New(nil))

	_, err := svc.InitiateImport(context.Background(), file, header, "user-123")
	if err == nil {
		t.Fatal("expected header validation error")
	}
	if createCalled {
		t.Fatal("job should not be created after header validation fails")
	}
	if len(producer.publishedEvents) != 0 {
		t.Fatalf("expected no Kafka events, got %#v", producer.publishedEvents)
	}
}

func TestProcessImportJob_AllSuccess(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
		{"SRV-002", "db-01", "10.0.1.2", "CentOS", "16", "64", "2000", "DC-HCM", "DB"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()

	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error {
			return nil
		},
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{
				ID:       jobID,
				FilePath: tmpFile,
			}, nil
		},
		UpdateCompletedFunc: func(ctx context.Context, id string, total, success, failed int) error {
			if success != 2 || failed != 0 {
				t.Errorf("expected 2 success, 0 failed; got %d, %d", success, failed)
			}
			return nil
		},
		CreateServerWithDetailFunc: func(ctx context.Context, server *model.Server, detail *model.ImportJobDetail) error {
			if detail.Status != "success" {
				t.Errorf("expected success detail, got %q", detail.Status)
			}
			return nil
		},
	}

	serverWriter := &mocks.ServerWriterMock{
		FindByServerIDOrNameFunc: func(ctx context.Context, serverID, serverName string) (*model.Server, error) {
			return nil, gorm.ErrRecordNotFound // simulate no duplicates
		},
	}

	parser := excel.NewExcelParser()
	producer := &fakeProducer{}
	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)
	cache := &fakeCache{}
	svc.(*importServiceImpl).cache = cache

	err := svc.ProcessImportJob(context.Background(), jobID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(producer.publishedEvents) != 2 {
		t.Fatalf("expected 2 server.created events, got %d", len(producer.publishedEvents))
	}
	for _, evt := range producer.publishedEvents {
		if evt.topic != "server.created" || evt.event.EventType != "server.created" {
			t.Fatalf("unexpected event: %#v", evt)
		}
	}
	if len(cache.patterns) != 2 || cache.patterns[0] != "server:detail:*" || cache.patterns[1] != "servers:list:*" {
		t.Fatalf("unexpected cache invalidation patterns: %#v", cache.patterns)
	}
}

func TestProcessImportJob_UpdateStatusError(t *testing.T) {
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error {
			return fmt.Errorf("status update failed")
		},
	}
	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), uuid.New().String()); err == nil {
		t.Fatal("expected update status error")
	}
}

func TestProcessImportJob_FindByIDErrorMarksFailed(t *testing.T) {
	failedUpdated := false
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error { return nil },
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return nil, fmt.Errorf("not found")
		},
		UpdateFailedFunc: func(ctx context.Context, id string, errMsg string) error {
			failedUpdated = true
			return nil
		},
	}
	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), uuid.New().String()); err == nil {
		t.Fatal("expected find job error")
	}
	if !failedUpdated {
		t.Fatal("expected job to be marked failed")
	}
}

func TestProcessImportJob_Duplicates(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
		{"SRV-001", "web-02", "10.0.1.2", "CentOS", "16", "64", "2000", "DC-HCM", "DB"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()
	now := time.Now().UTC()

	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error {
			return nil
		},
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{
				ID:       jobID,
				FilePath: tmpFile,
			}, nil
		},
		UpdateCompletedFunc: func(ctx context.Context, id string, total, success, failed int) error {
			if success != 1 || failed != 1 {
				t.Errorf("expected 1 success, 1 failed; got %d, %d", success, failed)
			}
			return nil
		},
		SaveDetailFunc: func(ctx context.Context, detail *model.ImportJobDetail) error {
			// The first row succeeds, second row is duplicate and should be "failed"
			return nil
		},
	}

	callCount := 0
	serverWriter := &mocks.ServerWriterMock{
		FindByServerIDOrNameFunc: func(ctx context.Context, serverID, serverName string) (*model.Server, error) {
			callCount++
			if callCount == 1 {
				return nil, gorm.ErrRecordNotFound // first call: no duplicate
			}
			// second call: duplicate server_id
			return &model.Server{
				ServerID:   "SRV-001",
				ServerName: "web-01",
				CreatedAt:  now,
				UpdatedAt:  now,
			}, nil
		},
		CreateFunc: func(ctx context.Context, server *model.Server) error {
			// First row: server created successfully
			return nil
		},
	}

	parser := excel.NewExcelParser()
	producer := &fakeProducer{}
	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	err := svc.ProcessImportJob(context.Background(), jobID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessImportJob_InvalidRowsSavedAsFailed(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()
	saveDetailCalled := false
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error { return nil },
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{ID: jobID, FilePath: tmpFile}, nil
		},
		SaveDetailFunc: func(ctx context.Context, detail *model.ImportJobDetail) error {
			saveDetailCalled = true
			if detail.Status != "failed" || detail.ErrorReason == "" {
				t.Fatalf("expected failed detail with reason, got %#v", detail)
			}
			return nil
		},
		UpdateCompletedFunc: func(ctx context.Context, id string, total, success, failed int) error {
			if total != 1 || success != 0 || failed != 1 {
				t.Fatalf("expected 1 total, 0 success, 1 failed; got %d/%d/%d", total, success, failed)
			}
			return nil
		},
	}

	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), jobID.String()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !saveDetailCalled {
		t.Fatal("expected failed row detail to be saved")
	}
}

func TestProcessImportJob_DuplicateCheckErrorMarksRowFailed(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error { return nil },
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{ID: jobID, FilePath: tmpFile}, nil
		},
		SaveDetailFunc: func(ctx context.Context, detail *model.ImportJobDetail) error {
			if detail.Status != "failed" || detail.ErrorReason == "" {
				t.Fatalf("expected failed detail with duplicate check error, got %#v", detail)
			}
			return nil
		},
		UpdateCompletedFunc: func(ctx context.Context, id string, total, success, failed int) error {
			if success != 0 || failed != 1 {
				t.Fatalf("expected 0 success, 1 failed; got %d/%d", success, failed)
			}
			return nil
		},
	}
	serverWriter := &mocks.ServerWriterMock{
		FindByServerIDOrNameFunc: func(ctx context.Context, serverID, serverName string) (*model.Server, error) {
			return nil, fmt.Errorf("db down")
		},
	}

	svc := NewImportService(jobRepo, serverWriter, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), jobID.String()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessImportJob_CreateServerWithDetailErrorMarksRowFailed(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error { return nil },
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{ID: jobID, FilePath: tmpFile}, nil
		},
		SaveDetailFunc: func(ctx context.Context, detail *model.ImportJobDetail) error {
			if detail.Status != "failed" || detail.ErrorReason == "" {
				t.Fatalf("expected failed detail with db error, got %#v", detail)
			}
			return nil
		},
		UpdateCompletedFunc: func(ctx context.Context, id string, total, success, failed int) error {
			if success != 0 || failed != 1 {
				t.Fatalf("expected 0 success, 1 failed; got %d/%d", success, failed)
			}
			return nil
		},
	}
	serverWriter := &mocks.ServerWriterMock{
		FindByServerIDOrNameFunc: func(ctx context.Context, serverID, serverName string) (*model.Server, error) {
			return nil, gorm.ErrRecordNotFound
		},
		CreateFunc: func(ctx context.Context, server *model.Server) error {
			return fmt.Errorf("insert failed")
		},
	}

	svc := NewImportService(jobRepo, serverWriter, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), jobID.String()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessImportJob_SaveDetailErrorFailsJob(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()
	failedUpdated := false
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error { return nil },
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{ID: jobID, FilePath: tmpFile}, nil
		},
		SaveDetailFunc: func(ctx context.Context, detail *model.ImportJobDetail) error {
			return fmt.Errorf("detail insert failed")
		},
		UpdateFailedFunc: func(ctx context.Context, id string, errMsg string) error {
			failedUpdated = true
			return nil
		},
	}

	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), jobID.String()); err == nil {
		t.Fatal("expected error")
	}
	if !failedUpdated {
		t.Fatal("expected job to be marked failed")
	}
}

func TestProcessImportJob_UpdateCompletedError(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error { return nil },
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{ID: jobID, FilePath: tmpFile}, nil
		},
		CreateServerWithDetailFunc: func(ctx context.Context, server *model.Server, detail *model.ImportJobDetail) error {
			return nil
		},
		UpdateCompletedFunc: func(ctx context.Context, id string, total, success, failed int) error {
			return fmt.Errorf("update failed")
		},
	}
	serverWriter := &mocks.ServerWriterMock{
		FindByServerIDOrNameFunc: func(ctx context.Context, serverID, serverName string) (*model.Server, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}

	svc := NewImportService(jobRepo, serverWriter, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), jobID.String()); err == nil {
		t.Fatal("expected update completed error")
	}
}

func TestProcessImportJob_ServerCreatedPublishFailureDoesNotFailImport(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	jobID := uuid.New()
	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error { return nil },
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{ID: jobID, FilePath: tmpFile}, nil
		},
		CreateServerWithDetailFunc: func(ctx context.Context, server *model.Server, detail *model.ImportJobDetail) error {
			return nil
		},
		UpdateCompletedFunc: func(ctx context.Context, id string, total, success, failed int) error {
			return nil
		},
	}
	serverWriter := &mocks.ServerWriterMock{
		FindByServerIDOrNameFunc: func(ctx context.Context, serverID, serverName string) (*model.Server, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	producer := &fakeProducer{failTopic: map[string]error{"server.created": fmt.Errorf("kafka unavailable")}}

	svc := NewImportService(jobRepo, serverWriter, excel.NewExcelParser(), producer, nil, setupTestConfig(), zerolog.New(nil))
	if err := svc.ProcessImportJob(context.Background(), jobID.String()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessImportJob_ParseError(t *testing.T) {
	jobID := uuid.New()

	// Create a file with invalid headers
	headers := []string{"wrong", "headers"}
	tmpFile := createTestXLSX(t, headers, nil)
	defer os.Remove(tmpFile)

	jobRepo := &mocks.ImportJobRepoMock{
		UpdateStatusFunc: func(ctx context.Context, id string, status string) error {
			return nil
		},
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{
				ID:       jobID,
				FilePath: tmpFile,
			}, nil
		},
		UpdateFailedFunc: func(ctx context.Context, id string, errMsg string) error {
			return nil
		},
	}

	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{}
	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	err := svc.ProcessImportJob(context.Background(), jobID.String())
	if err == nil {
		t.Fatal("expected error for invalid file")
	}
}

func TestGetImportJobStatus_Found(t *testing.T) {
	jobID := uuid.New()
	now := time.Now().UTC()

	jobRepo := &mocks.ImportJobRepoMock{
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{
				ID:           jobID,
				Status:       "completed",
				FileName:     "servers.xlsx",
				TotalRows:    2,
				SuccessCount: 2,
				FailedCount:  0,
				CreatedAt:    now,
			}, nil
		},
		GetDetailsByJobIDFunc: func(ctx context.Context, id string) ([]model.ImportJobDetail, error) {
			return []model.ImportJobDetail{
				{RowNumber: 2, ServerID: "SRV-001", ServerName: "web-01", Status: "success"},
				{RowNumber: 3, ServerID: "SRV-002", ServerName: "db-01", Status: "success"},
			}, nil
		},
	}

	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{}
	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	result, err := svc.GetImportJobStatus(context.Background(), jobID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", result.Status)
	}
	if result.TotalRows != 2 {
		t.Errorf("expected 2 total rows, got %d", result.TotalRows)
	}
	if len(result.SuccessList) != 2 {
		t.Errorf("expected 2 success items, got %d", len(result.SuccessList))
	}
	if len(result.FailedList) != 0 {
		t.Errorf("expected 0 failed items, got %d", len(result.FailedList))
	}
}

func TestGetImportJobStatus_WithTimesAndFailedRows(t *testing.T) {
	jobID := uuid.New()
	now := time.Now().UTC()
	errMsg := "partial failure"

	jobRepo := &mocks.ImportJobRepoMock{
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{
				ID:           jobID,
				Status:       "completed",
				FileName:     "servers.xlsx",
				TotalRows:    2,
				SuccessCount: 1,
				FailedCount:  1,
				ErrorMessage: &errMsg,
				StartedAt:    &now,
				CompletedAt:  &now,
				CreatedAt:    now,
			}, nil
		},
		GetDetailsByJobIDFunc: func(ctx context.Context, id string) ([]model.ImportJobDetail, error) {
			return []model.ImportJobDetail{
				{RowNumber: 2, ServerID: "SRV-001", ServerName: "web-01", Status: "success"},
				{RowNumber: 3, ServerID: "SRV-002", ServerName: "db-01", Status: "failed", ErrorReason: "duplicate"},
			}, nil
		},
	}

	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	result, err := svc.GetImportJobStatus(context.Background(), jobID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StartedAt == nil || result.CompletedAt == nil || result.ErrorMessage == nil {
		t.Fatalf("expected timestamps and error message in response: %#v", result)
	}
	if len(result.SuccessList) != 1 || len(result.FailedList) != 1 {
		t.Fatalf("expected one success and one failed row, got %d/%d", len(result.SuccessList), len(result.FailedList))
	}
}

func TestGetImportJobStatus_DetailsError(t *testing.T) {
	jobID := uuid.New()
	now := time.Now().UTC()
	jobRepo := &mocks.ImportJobRepoMock{
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return &model.ImportJob{ID: jobID, Status: "processing", FileName: "servers.xlsx", CreatedAt: now}, nil
		},
		GetDetailsByJobIDFunc: func(ctx context.Context, id string) ([]model.ImportJobDetail, error) {
			return nil, fmt.Errorf("details unavailable")
		},
	}

	svc := NewImportService(jobRepo, &mocks.ServerWriterMock{}, excel.NewExcelParser(), &fakeProducer{}, nil, setupTestConfig(), zerolog.New(nil))
	if _, err := svc.GetImportJobStatus(context.Background(), jobID.String()); err == nil {
		t.Fatal("expected details error")
	}
}

func TestGetImportJobStatus_NotFound(t *testing.T) {
	jobRepo := &mocks.ImportJobRepoMock{
		FindByIDFunc: func(ctx context.Context, id string) (*model.ImportJob, error) {
			return nil, fmt.Errorf("not found")
		},
	}

	serverWriter := &mocks.ServerWriterMock{}
	parser := excel.NewExcelParser()
	producer := &fakeProducer{}
	cfg := setupTestConfig()
	log := zerolog.New(nil)

	svc := NewImportService(jobRepo, serverWriter, parser, producer, nil, cfg, log)

	_, err := svc.GetImportJobStatus(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}
