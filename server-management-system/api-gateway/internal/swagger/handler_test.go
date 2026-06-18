package swagger

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterRoutes_ServesSwaggerIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	RegisterRoutes(r, "")

	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "SwaggerUIBundle") {
		t.Fatalf("expected swagger UI html, got %q", w.Body.String())
	}
}

func TestRegisterRoutes_ServesOpenAPISpec(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	specPath := filepath.Join(dir, "api-spec.yaml")
	if err := os.WriteFile(specPath, []byte("openapi: 3.0.3\ninfo:\n  title: test\n"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	r := gin.New()
	RegisterRoutes(r, specPath)

	req := httptest.NewRequest(http.MethodGet, "/swagger/doc.yaml", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "openapi: 3.0.3") {
		t.Fatalf("expected openapi spec, got %q", w.Body.String())
	}
}

func TestRegisterRoutes_MissingOpenAPISpec(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	RegisterRoutes(r, filepath.Join(t.TempDir(), "missing.yaml"))

	req := httptest.NewRequest(http.MethodGet, "/swagger/doc.yaml", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestResolveSpecPath_UsesEnvironmentPath(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "api-spec.yaml")
	if err := os.WriteFile(specPath, []byte("openapi: 3.0.3\n"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	t.Setenv("API_SPEC_PATH", specPath)

	if got := resolveSpecPath(); got != specPath {
		t.Fatalf("expected env spec path %q, got %q", specPath, got)
	}
}

func TestResolveSpecPath_ReturnsEmptyWhenNoSpecExists(t *testing.T) {
	t.Setenv("API_SPEC_PATH", filepath.Join(t.TempDir(), "missing.yaml"))

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})

	if got := resolveSpecPath(); got != "" {
		t.Fatalf("expected empty path, got %q", got)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "api-spec.yaml")
	if err := os.WriteFile(filePath, []byte("openapi: 3.0.3\n"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	if !fileExists(filePath) {
		t.Fatal("expected file to exist")
	}
	if fileExists(dir) {
		t.Fatal("directory should not count as file")
	}
	if fileExists("") {
		t.Fatal("empty path should not exist")
	}
}
