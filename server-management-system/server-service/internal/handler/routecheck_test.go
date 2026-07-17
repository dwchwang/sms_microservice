package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// /stats and /import must not be swallowed by the /:server_id wildcard.
func TestRouteOrderingResolvesLiteralsBeforeWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	hit := ""
	servers := r.Group("/api/v1/servers")
	{
		servers.POST("", func(c *gin.Context) { hit = "create" })
		servers.POST("/import", func(c *gin.Context) { hit = "import" })
		servers.GET("", func(c *gin.Context) { hit = "list" })
		servers.GET("/stats", func(c *gin.Context) { hit = "stats" })
		servers.POST("/export", func(c *gin.Context) { hit = "export" })
		servers.GET("/:server_id", func(c *gin.Context) { hit = "get" })
		servers.PUT("/:server_id", func(c *gin.Context) { hit = "update" })
		servers.DELETE("/:server_id", func(c *gin.Context) { hit = "delete" })
	}
	r.GET("/internal/servers", func(c *gin.Context) { hit = "population" })

	for _, tc := range []struct{ method, path, want string }{
		{"GET", "/api/v1/servers/stats", "stats"},
		{"POST", "/api/v1/servers/import", "import"},
		{"POST", "/api/v1/servers/export", "export"},
		{"GET", "/api/v1/servers/SRV-001", "get"},
		{"DELETE", "/api/v1/servers/SRV-001", "delete"},
		{"GET", "/internal/servers", "population"},
	} {
		hit = ""
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if hit != tc.want {
			t.Errorf("%s %s hit %q, want %q (code %d)", tc.method, tc.path, hit, tc.want, w.Code)
		}
	}
}
