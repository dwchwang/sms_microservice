package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/service"
	"github.com/vcs-sms/shared/response"
)

// fakeExportService is a mock for testing export handlers.
type fakeExportService struct {
	exportFunc func(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error)
}

func (s *fakeExportService) ExportServers(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error) {
	if s.exportFunc != nil {
		return s.exportFunc(ctx, filter)
	}
	return bytes.NewBuffer([]byte("test-xlsx-content")), "servers_export_20260611_000000.xlsx", nil
}

func TestExportHandler_ValidRequest(t *testing.T) {
	body := `{"status":"on","sort_by":"server_name","sort_order":"asc","page":2,"page_size":50}`
	req := httptest.NewRequest("POST", "/api/v1/servers/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	router := setupTestRouter()
	svc := &fakeExportService{
		exportFunc: func(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error) {
			if filter.Status != "on" || filter.SortBy != "server_name" || filter.SortOrder != "asc" {
				t.Fatalf("unexpected filter fields: %#v", filter)
			}
			if filter.Page != 2 || filter.PageSize != 50 {
				t.Fatalf("expected pagination 2/50, got %d/%d", filter.Page, filter.PageSize)
			}
			return bytes.NewBuffer([]byte("test-xlsx-content")), "servers_export_20260611_000000.xlsx", nil
		},
	}
	h := NewExportHandler(svc)
	router.POST("/api/v1/servers/export", h.ExportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("expected xlsx content type, got %q", contentType)
	}

	contentDisposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, "attachment") {
		t.Errorf("expected attachment in Content-Disposition, got %q", contentDisposition)
	}
}

func TestExportHandler_LegacyNestedFilterRequest(t *testing.T) {
	body := `{"filter":{"status":"off","location":"DC-HN"},"sort_by":"server_name","page":1,"page_size":25}`
	req := httptest.NewRequest("POST", "/api/v1/servers/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	router := setupTestRouter()
	svc := &fakeExportService{
		exportFunc: func(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error) {
			if filter.Status != "off" || filter.Location != "DC-HN" {
				t.Fatalf("unexpected nested filter fields: %#v", filter)
			}
			if filter.SortBy != "server_name" || filter.Page != 1 || filter.PageSize != 25 {
				t.Fatalf("unexpected sort/page fields: %#v", filter)
			}
			return bytes.NewBuffer([]byte("test-xlsx-content")), "servers_export_20260611_000000.xlsx", nil
		},
	}
	h := NewExportHandler(svc)
	router.POST("/api/v1/servers/export", h.ExportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestExportHandler_EmptyRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/servers/export", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	router := setupTestRouter()
	svc := &fakeExportService{}
	h := NewExportHandler(svc)
	router.POST("/api/v1/servers/export", h.ExportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestExportHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/servers/export", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")

	router := setupTestRouter()
	svc := &fakeExportService{}
	h := NewExportHandler(svc)
	router.POST("/api/v1/servers/export", h.ExportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestExportHandler_ServiceError(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/servers/export", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	router := setupTestRouter()
	svc := &fakeExportService{
		exportFunc: func(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error) {
			return nil, "", service.ErrExportFailed
		},
	}
	h := NewExportHandler(svc)
	router.POST("/api/v1/servers/export", h.ExportServers)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// Ensure interface satisfaction
var _ service.ExportService = (*fakeExportService)(nil)
var _ = json.Marshal
var _ = response.ApiResponse{}
