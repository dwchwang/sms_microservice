package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// newGuardedRouter mirrors how the services wire the two middlewares.
func newGuardedRouter(scope string) *gin.Engine {
	r := gin.New()
	r.GET("/guarded", AuthFromForwardAuth(), RequireScope(scope), func(c *gin.Context) {
		c.String(http.StatusOK, "reached")
	})
	return r
}

func do(r *gin.Engine, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequireScope_AllowsMatchingScope(t *testing.T) {
	w := do(newGuardedRouter("server:delete"), map[string]string{
		"X-User-Id":     "u1",
		"X-User-Scopes": "server:list,server:delete",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body = %s", w.Code, w.Body)
	}
}

// A viewer token carries no server:delete, and must not reach the handler.
func TestRequireScope_RejectsMissingScope(t *testing.T) {
	w := do(newGuardedRouter("server:delete"), map[string]string{
		"X-User-Id":     "viewer",
		"X-User-Scopes": "server:list,server:view,server:stats,report:view",
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403; body = %s", w.Code, w.Body)
	}
	if w.Body.String() == "reached" {
		t.Fatal("handler ran without the required scope")
	}
}

func TestAuthFromForwardAuth_RejectsMissingIdentity(t *testing.T) {
	w := do(newGuardedRouter("server:delete"), nil)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401; body = %s", w.Code, w.Body)
	}
}

func TestRequireScope_RejectsEmptyScopes(t *testing.T) {
	w := do(newGuardedRouter("server:delete"), map[string]string{"X-User-Id": "u1"})

	if w.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403; body = %s", w.Code, w.Body)
	}
}

// server:list must not satisfy server:list_all.
func TestRequireScope_DoesNotPrefixMatch(t *testing.T) {
	w := do(newGuardedRouter("server:list_all"), map[string]string{
		"X-User-Id":     "u1",
		"X-User-Scopes": "server:list",
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403; body = %s", w.Code, w.Body)
	}
}

func TestRequireScope_TrimsSpaceAroundScopes(t *testing.T) {
	w := do(newGuardedRouter("server:delete"), map[string]string{
		"X-User-Id":     "u1",
		"X-User-Scopes": "server:list, server:delete",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body = %s", w.Code, w.Body)
	}
}
