package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/vcs-sms/monitor-service/internal/model"
)

// Doc is one health fact as stored in Elasticsearch.
type Doc struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Status     string `json:"status"`
	CheckedAt  string `json:"checked_at"`
	RoundID    int64  `json:"round_id"`
	LatencyMs  int    `json:"latency_ms"`
	ErrorCode  string `json:"error_code,omitempty"`
}

// FactWriter writes a batch of health facts.
type FactWriter interface {
	Write(ctx context.Context, facts []model.Fact) error
}

type esFactWriter struct {
	client      *elasticsearch.Client
	indexPrefix string
}

// NewFactWriter creates an Elasticsearch-backed FactWriter.
func NewFactWriter(client *elasticsearch.Client, indexPrefix string) FactWriter {
	return &esFactWriter{client: client, indexPrefix: indexPrefix}
}

// IndexName returns the daily index a fact belongs to.
func IndexName(prefix string, checkedAt time.Time) string {
	return fmt.Sprintf("%s-%s", prefix, checkedAt.UTC().Format("2006.01.02"))
}

// DocID is deterministic so a retried bulk cannot double-count a check.
func DocID(serverID string, roundID int64) string {
	return fmt.Sprintf("%s:%d", serverID, roundID)
}

// Write bulk-indexes the facts. Documents carry a deterministic _id, so the
// whole call is safe to retry.
func (w *esFactWriter) Write(ctx context.Context, facts []model.Fact) error {
	if len(facts) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, f := range facts {
		meta := map[string]any{
			"index": map[string]any{
				"_index": IndexName(w.indexPrefix, f.CheckedAt),
				"_id":    DocID(f.ServerID, f.RoundID),
			},
		}
		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("failed to encode bulk metadata: %w", err)
		}
		buf.Write(metaJSON)
		buf.WriteByte('\n')

		docJSON, err := json.Marshal(Doc{
			ServerID:   f.ServerID,
			ServerName: f.ServerName,
			Status:     f.Status,
			CheckedAt:  f.CheckedAt.UTC().Format(time.RFC3339),
			RoundID:    f.RoundID,
			LatencyMs:  f.LatencyMs,
			ErrorCode:  f.ErrorCode,
		})
		if err != nil {
			return fmt.Errorf("failed to encode health fact: %w", err)
		}
		buf.Write(docJSON)
		buf.WriteByte('\n')
	}

	res, err := w.client.Bulk(bytes.NewReader(buf.Bytes()), w.client.Bulk.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("bulk request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("bulk returned %s", res.Status())
	}
	return bulkItemError(res.Body)
}

type bulkResponse struct {
	Errors bool `json:"errors"`
	Items  []map[string]struct {
		Status int `json:"status"`
		Error  *struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	} `json:"items"`
}

// A _bulk answers 200 even when every item was rejected; the outcome is in the body.
func bulkItemError(body io.Reader) error {
	var parsed bulkResponse
	if err := json.NewDecoder(body).Decode(&parsed); err != nil {
		return fmt.Errorf("failed to decode bulk response: %w", err)
	}
	if !parsed.Errors {
		return nil
	}

	failed := 0
	var sample string
	for _, item := range parsed.Items {
		for _, outcome := range item {
			if outcome.Error == nil {
				continue
			}
			failed++
			if sample == "" {
				sample = fmt.Sprintf("%s: %s", outcome.Error.Type, outcome.Error.Reason)
			}
		}
	}
	if failed == 0 {
		return nil
	}
	return fmt.Errorf("bulk rejected %d of %d items (%s)", failed, len(parsed.Items), sample)
}
