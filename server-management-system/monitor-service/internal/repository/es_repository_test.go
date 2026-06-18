package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/vcs-sms/monitor-service/internal/checker"
)

// mockESRepo is a simple test double implementing ESStatusLogRepo
type mockESRepo struct {
	bulkErr   error
	callCount int
}

func (m *mockESRepo) BulkIndex(ctx context.Context, results []*checker.HealthResult) error {
	m.callCount++
	return m.bulkErr
}

// Verify mockESRepo satisfies ESStatusLogRepo
var _ ESStatusLogRepo = (*mockESRepo)(nil)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestESRepo_BulkIndex_Empty(t *testing.T) {
	mock := &mockESRepo{}
	err := mock.BulkIndex(context.Background(), nil)
	if err != nil {
		t.Errorf("Expected no error for empty results, got %v", err)
	}
	if mock.callCount != 1 {
		t.Errorf("Expected 1 call, got %d", mock.callCount)
	}
}

func TestESRepo_BulkIndex_Success(t *testing.T) {
	mock := &mockESRepo{}
	results := []*checker.HealthResult{
		{
			ServerID:       "SRV-001",
			ServerName:     "Test",
			Status:         "on",
			ResponseTimeMs: 5,
			CheckMethod:    "tcp",
			CheckedAt:      time.Now().UTC(),
		},
	}
	err := mock.BulkIndex(context.Background(), results)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if mock.callCount != 1 {
		t.Errorf("Expected 1 call, got %d", mock.callCount)
	}
}

func TestESRepo_BulkIndex_Error(t *testing.T) {
	mock := &mockESRepo{bulkErr: fmt.Errorf("ES unavailable")}
	results := []*checker.HealthResult{
		{ServerID: "SRV-001", Status: "on"},
	}
	err := mock.BulkIndex(context.Background(), results)
	if err == nil {
		t.Error("Expected error from mock ES")
	}
}

func TestESStatusLogRepo_BulkIndex_Empty(t *testing.T) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("empty bulk index should not call Elasticsearch")
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("new ES client: %v", err)
	}

	repo := NewESStatusLogRepo(client, "health")
	if err := repo.BulkIndex(context.Background(), nil); err != nil {
		t.Fatalf("BulkIndex empty failed: %v", err)
	}
}

func TestESStatusLogRepo_BulkIndex_Success(t *testing.T) {
	var body string
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			data, _ := io.ReadAll(req.Body)
			body = string(data)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"errors":false}`)),
				Header:     http.Header{"X-Elastic-Product": []string{"Elasticsearch"}},
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("new ES client: %v", err)
	}

	repo := NewESStatusLogRepo(client, "health")
	err = repo.BulkIndex(context.Background(), []*checker.HealthResult{{
		ServerID:       "SRV-001",
		ServerName:     "Server 1",
		Status:         "off",
		ResponseTimeMs: 15,
		CheckMethod:    "tcp",
		CheckedAt:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Error:          "timeout",
	}})
	if err != nil {
		t.Fatalf("BulkIndex failed: %v", err)
	}
	if !strings.Contains(body, `"server_id":"SRV-001"`) || !strings.Contains(body, `"error":"timeout"`) {
		t.Fatalf("bulk body missing document fields: %s", body)
	}
}

func TestESStatusLogRepo_BulkIndex_RequestError(t *testing.T) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network down")
		}),
	})
	if err != nil {
		t.Fatalf("new ES client: %v", err)
	}

	repo := NewESStatusLogRepo(client, "health")
	err = repo.BulkIndex(context.Background(), []*checker.HealthResult{{ServerID: "SRV-001", CheckedAt: time.Now().UTC()}})
	if err == nil {
		t.Fatal("expected request error")
	}
}

func TestESStatusLogRepo_BulkIndex_ResponseError(t *testing.T) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				Body:       io.NopCloser(strings.NewReader(`{"error":"boom"}`)),
				Header:     http.Header{"X-Elastic-Product": []string{"Elasticsearch"}},
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("new ES client: %v", err)
	}

	repo := NewESStatusLogRepo(client, "health")
	err = repo.BulkIndex(context.Background(), []*checker.HealthResult{{ServerID: "SRV-001", CheckedAt: time.Now().UTC()}})
	if err == nil {
		t.Fatal("expected response error")
	}
}
