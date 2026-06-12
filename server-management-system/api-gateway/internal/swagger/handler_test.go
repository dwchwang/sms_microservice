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
