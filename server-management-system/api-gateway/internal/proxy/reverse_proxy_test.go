package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errReadCloser struct{}

func (errReadCloser) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read failed")
}

func (errReadCloser) Close() error {
	return nil
}

func TestNewReverseProxy_ForwardsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("X-Upstream", "auth")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	r := gin.New()
	r.Any("/api/v1/auth/*path", NewReverseProxy(upstream.URL))
	gateway := httptest.NewServer(r)
	defer gateway.Close()

	resp, err := http.Post(gateway.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("gateway request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, string(body))
	}
	if resp.Header.Get("X-Upstream") != "auth" {
		t.Fatalf("missing upstream header")
	}
}

func TestNewReverseProxy_InvalidTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/test", NewReverseProxy("://bad-url"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestReverseProxy_BackendConnectionError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	original := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("backend unavailable")
	})}
	t.Cleanup(func() { httpClient = original })

	r := gin.New()
	r.POST("/api/v1/servers", NewReverseProxy("http://server-service"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers", strings.NewReader(`{"ok":true}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestReverseProxy_ReadBodyError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/api/v1/servers", NewReverseProxy("http://server-service"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers", nil)
	req.Body = errReadCloser{}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestReverseProxy_ForwardsHeadersAndQueryWithBasePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	original := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://server-service/internal/api/v1/servers?status=on" {
			t.Fatalf("unexpected upstream URL: %s", req.URL.String())
		}
		if req.Header.Get("X-Custom") != "value" {
			t.Fatalf("missing copied header")
		}
		if req.Header.Get("Connection") != "" {
			t.Fatalf("hop-by-hop header should be removed")
		}
		if req.Header.Get("X-Forwarded-Proto") != "http" || req.Host != "server-service" {
			t.Fatalf("unexpected forwarded headers host=%s proto=%s", req.Host, req.Header.Get("X-Forwarded-Proto"))
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"X-Upstream": []string{"server"}},
			Body:       io.NopCloser(strings.NewReader(`{"created":true}`)),
		}, nil
	})}
	t.Cleanup(func() { httpClient = original })

	r := gin.New()
	r.GET("/api/v1/servers", NewReverseProxy("http://server-service/internal"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers?status=on", nil)
	req.Header.Set("X-Custom", "value")
	req.Header.Set("Connection", "keep-alive")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if w.Header().Get("X-Upstream") != "server" {
		t.Fatalf("missing upstream response header")
	}
}
