package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/service"
	apperrors "github.com/vcs-sms/shared/errors"
)

type mockImportService struct {
	result *dto.ImportResponse
	err    error
}

func (m *mockImportService) Import(ctx context.Context, r io.Reader) (*dto.ImportResponse, error) {
	return m.result, m.err
}

type mockExportService struct {
	buf      *bytes.Buffer
	filename string
	err      error
	gotFiler dto.ServerFilter
}

func (m *mockExportService) Export(ctx context.Context, f *dto.ServerFilter) (*bytes.Buffer, string, error) {
	m.gotFiler = *f
	return m.buf, m.filename, m.err
}

func uploadRequest(t *testing.T, fieldName, filename, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	w.Close()

	req, _ := http.NewRequest("POST", "/api/v1/servers/import", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func importRouter(svc service.ImportService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/servers/import", NewImportHandler(svc).ImportServers)
	return r
}

func TestImportHandler_Success(t *testing.T) {
	mock := &mockImportService{result: &dto.ImportResponse{
		TotalRows: 2,
		Succeeded: dto.ImportSucceeded{Count: 2, Items: []string{"SRV-001", "SRV-002"}},
		Failed:    dto.ImportFailed{Count: 0, Items: []dto.ImportFailedItem{}},
	}}
	w := httptest.NewRecorder()

	importRouter(mock).ServeHTTP(w, uploadRequest(t, "file", "servers.xlsx", "fake"))

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}

	var got importEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad response json: %v", err)
	}
	if got.Data.Succeeded.Count != 2 {
		t.Errorf("succeeded = %d, want 2", got.Data.Succeeded.Count)
	}
	// meta.duration_ms is how import cost gets measured in production.
	if got.Meta.DurationMs == nil {
		t.Error("expected meta.duration_ms to be present")
	}
}

type importEnvelope struct {
	Status string             `json:"status"`
	Data   dto.ImportResponse `json:"data"`
	Meta   struct {
		DurationMs *int64 `json:"duration_ms"`
	} `json:"meta"`
}

func TestImportHandler_MissingFile(t *testing.T) {
	req, _ := http.NewRequest("POST", "/api/v1/servers/import", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()

	importRouter(&mockImportService{}).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
}

func TestImportHandler_FileRejected(t *testing.T) {
	mock := &mockImportService{err: fmt.Errorf("%w: not xlsx", service.ErrImportFileRejected)}
	w := httptest.NewRecorder()

	importRouter(mock).ServeHTTP(w, uploadRequest(t, "file", "bad.txt", "nope"))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", w.Code)
	}
	if !strings.Contains(w.Body.String(), apperrors.CodeServerImportRejected) {
		t.Errorf("expected %s, got %s", apperrors.CodeServerImportRejected, w.Body.String())
	}
}

func TestImportHandler_UnexpectedErrorIs500(t *testing.T) {
	mock := &mockImportService{err: errors.New("db exploded")}
	w := httptest.NewRecorder()

	importRouter(mock).ServeHTTP(w, uploadRequest(t, "file", "servers.xlsx", "fake"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.Code)
	}
}

func exportRouter(svc service.ExportService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/servers/export", NewExportHandler(svc).ExportServers)
	return r
}

func TestExportHandler_ReturnsXLSXAttachment(t *testing.T) {
	mock := &mockExportService{buf: bytes.NewBufferString("xlsx-bytes"), filename: "servers_export_20260716.xlsx"}
	req, _ := http.NewRequest("POST", "/api/v1/servers/export", strings.NewReader(`{"status":"ON"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	exportRouter(mock).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != xlsxContentType {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "servers_export_20260716.xlsx") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if w.Body.String() != "xlsx-bytes" {
		t.Errorf("body = %q", w.Body.String())
	}
	if mock.gotFiler.Status != "ON" {
		t.Errorf("filter.Status = %q, want ON", mock.gotFiler.Status)
	}
}

// An empty body means export everything, not a validation error.
func TestExportHandler_EmptyBodyExportsAll(t *testing.T) {
	mock := &mockExportService{buf: bytes.NewBufferString("xlsx"), filename: "e.xlsx"}
	req, _ := http.NewRequest("POST", "/api/v1/servers/export", nil)
	w := httptest.NewRecorder()

	exportRouter(mock).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}
	if mock.gotFiler.Status != "" {
		t.Errorf("expected an empty filter, got %#v", mock.gotFiler)
	}
}

func TestExportHandler_ServiceError(t *testing.T) {
	mock := &mockExportService{err: errors.New("db down")}
	req, _ := http.NewRequest("POST", "/api/v1/servers/export", nil)
	w := httptest.NewRecorder()

	exportRouter(mock).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.Code)
	}
}
