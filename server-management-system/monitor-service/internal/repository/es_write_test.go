package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/vcs-sms/monitor-service/internal/monitor"
)

func newTestWriter(t *testing.T, handler http.HandlerFunc) FactWriter {
	t.Helper()
	// The v8 client requires the product header.
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("X-Elastic-Product", "Elasticsearch")
		handler(rw, r)
	}))
	t.Cleanup(srv.Close)

	client, err := elasticsearch.NewClient(elasticsearch.Config{Addresses: []string{srv.URL}})
	if err != nil {
		t.Fatalf("failed to build client: %v", err)
	}
	return NewFactWriter(client, "server-status-logs")
}

func sampleFacts() []monitor.Fact {
	return []monitor.Fact{{
		ServerID:  "SRV-001",
		Status:    "ON",
		CheckedAt: time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
		RoundID:   29737001,
	}}
}

// A bulk that rejects items still answers 200 with errors:true.
func TestWrite_ReportsPerItemFailure(t *testing.T) {
	const body = `{"took":1,"errors":true,"items":[{"index":{"_index":"server-status-logs-2026.07.17","_id":"SRV-001:29737001","status":429,"error":{"type":"es_rejected_execution_exception","reason":"bulk queue full"}}}]}`

	w := newTestWriter(t, func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(body))
	})

	err := w.Write(context.Background(), sampleFacts())
	if err == nil {
		t.Fatal("Write returned nil for a bulk whose item was rejected; facts are lost silently")
	}
	if !strings.Contains(err.Error(), "es_rejected_execution_exception") {
		t.Errorf("error does not name the rejection cause: %v", err)
	}
}

func TestWrite_SucceedsWhenNoItemFails(t *testing.T) {
	const body = `{"took":1,"errors":false,"items":[{"index":{"_index":"server-status-logs-2026.07.17","_id":"SRV-001:29737001","status":201}}]}`

	w := newTestWriter(t, func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(body))
	})

	if err := w.Write(context.Background(), sampleFacts()); err != nil {
		t.Fatalf("Write failed on a clean bulk: %v", err)
	}
}

func TestWrite_ReportsTransportError(t *testing.T) {
	w := newTestWriter(t, func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	})

	if err := w.Write(context.Background(), sampleFacts()); err == nil {
		t.Fatal("Write returned nil for a 503 bulk")
	}
}
