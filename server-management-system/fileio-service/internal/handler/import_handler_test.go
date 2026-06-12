package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/model"
	"github.com/vcs-sms/fileio-service/internal/repository/mocks"
	"github.com/vcs-sms/fileio-service/internal/service"
	"github.com/vcs-sms/shared/response"
	"github.com/xuri/excelize/v2"

	"github.com/rs/zerolog"
)

// fakeImportService is a mock for testing handlers
type fakeImportService struct {
	initiateFunc   func(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error)
	getStatusFunc  func(ctx context.Context, jobID string) (*dto.ImportJobStatusResponse, error)
	processJobFunc func(ctx context.Context, jobID string) error
}

func (s *fakeImportService) InitiateImport(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error) {
	if s.initiateFunc != nil {
		return s.initiateFunc(ctx, file, header, userID)
	}
	return &dto.ImportJobResponse{JobID: "test-job", Status: "pending"}, nil
}

func (s *fakeImportService) ProcessImportJob(ctx context.Context, jobID string) error {
	if s.processJobFunc != nil {
		return s.processJobFunc(ctx, jobID)
	}
	return nil
}

func (s *fakeImportService) GetImportJobStatus(ctx context.Context, jobID string) (*dto.ImportJobStatusResponse, error) {
	if s.getStatusFunc != nil {
		return s.getStatusFunc(ctx, jobID)
	}
	return &dto.ImportJobStatusResponse{JobID: jobID, Status: "completed"}, nil
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
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

func TestImportHandler_ValidFile(t *testing.T) {
	// Create a valid xlsx file
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	tmpFile := createTestXLSX(t, headers, [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	})
	defer os.Remove(tmpFile)

	xlsxData, _ := os.ReadFile(tmpFile)

	// Create multipart request
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "servers.xlsx")
	part.Write(xlsxData)
	w.Close()

	req := httptest.NewRequest("POST", "/api/v1/servers/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-User-ID", "user-123")

	router := setupTestRouter()
	svc := &fakeImportService{
		initiateFunc: func(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error) {
			return &dto.ImportJobResponse{
				JobID:    "test-uuid",
				Status:   "pending",
				FileName: "servers.xlsx",
				Message:  "File received",
			}, nil
		},
	}

	h := NewImportHandler(svc)
	router.POST("/api/v1/servers/import", h.ImportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var apiResp response.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &apiResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if apiResp.Status != "success" {
		t.Errorf("expected success, got %s", apiResp.Status)
	}
}

func TestImportHandler_FallbackUserHeader(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "servers.xlsx")
	part.Write([]byte("xlsx"))
	w.Close()

	req := httptest.NewRequest("POST", "/api/v1/servers/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Auth-User-ID", "fallback-user")

	router := setupTestRouter()
	svc := &fakeImportService{
		initiateFunc: func(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error) {
			if userID != "fallback-user" {
				t.Fatalf("expected fallback user header, got %q", userID)
			}
			return &dto.ImportJobResponse{JobID: "test-uuid", Status: "pending"}, nil
		},
	}
	h := NewImportHandler(svc)
	router.POST("/api/v1/servers/import", h.ImportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestImportHandler_ServiceError(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "servers.xlsx")
	part.Write([]byte("xlsx"))
	w.Close()

	req := httptest.NewRequest("POST", "/api/v1/servers/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	router := setupTestRouter()
	svc := &fakeImportService{
		initiateFunc: func(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error) {
			return nil, service.ErrJobNotFound
		},
	}
	h := NewImportHandler(svc)
	router.POST("/api/v1/servers/import", h.ImportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestImportHandler_NoFile(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/servers/import", nil)
	req.Header.Set("Content-Type", "multipart/form-data")

	router := setupTestRouter()
	svc := &fakeImportService{}
	h := NewImportHandler(svc)
	router.POST("/api/v1/servers/import", h.ImportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestGetImportStatusHandler_MissingJobID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/servers/import/", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	h := NewImportHandler(&fakeImportService{})
	h.GetImportStatus(c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestGetImportStatusHandler_Found(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/servers/import/test-job-id", nil)

	router := setupTestRouter()
	svc := &fakeImportService{
		getStatusFunc: func(ctx context.Context, jobID string) (*dto.ImportJobStatusResponse, error) {
			return &dto.ImportJobStatusResponse{
				JobID:        jobID,
				Status:       "completed",
				TotalRows:    10,
				SuccessCount: 8,
				FailedCount:  2,
			}, nil
		},
	}
	h := NewImportHandler(svc)
	router.GET("/api/v1/servers/import/:job_id", h.GetImportStatus)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestGetImportStatusHandler_NotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/servers/import/nonexistent", nil)

	router := setupTestRouter()
	svc := &fakeImportService{
		getStatusFunc: func(ctx context.Context, jobID string) (*dto.ImportJobStatusResponse, error) {
			return nil, service.ErrJobNotFound
		},
	}
	h := NewImportHandler(svc)
	router.GET("/api/v1/servers/import/:job_id", h.GetImportStatus)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// Ensure interface satisfaction
var _ service.ImportService = (*fakeImportService)(nil)
var _ = zerolog.Logger{}
var _ = model.ImportJob{}
var _ = mocks.ImportJobRepoMock{}
