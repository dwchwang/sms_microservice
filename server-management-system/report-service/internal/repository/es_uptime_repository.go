package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

const aggPageSize = 1000

// ServerFacts is one server's measured health for a day.
type ServerFacts struct {
	ServerID     string
	ServerName   string
	OnChecks     int
	ActualChecks int
	LastStatus   string
}

// UptimeAggregator reads measured facts out of Elasticsearch. Only the
// snapshot job calls it, once a day.
type UptimeAggregator interface {
	// AggregateDay returns facts keyed by server_id for [start, end).
	AggregateDay(ctx context.Context, start, end time.Time) (map[string]ServerFacts, error)
}

type esUptimeAggregator struct {
	client      *elasticsearch.Client
	indexPrefix string
}

// NewUptimeAggregator creates an Elasticsearch-backed UptimeAggregator.
func NewUptimeAggregator(client *elasticsearch.Client, indexPrefix string) UptimeAggregator {
	return &esUptimeAggregator{client: client, indexPrefix: indexPrefix}
}

// aggResponse is the slice of the ES reply the job needs.
type aggResponse struct {
	Aggregations struct {
		ByServer struct {
			AfterKey map[string]any `json:"after_key"`
			Buckets  []struct {
				Key      map[string]string `json:"key"`
				DocCount int               `json:"doc_count"`
				OnChecks struct {
					DocCount int `json:"doc_count"`
				} `json:"on_checks"`
				LastFact struct {
					Hits struct {
						Hits []struct {
							Source struct {
								ServerName string `json:"server_name"`
								Status     string `json:"status"`
							} `json:"_source"`
						} `json:"hits"`
					} `json:"hits"`
				} `json:"last_fact"`
			} `json:"buckets"`
		} `json:"by_server"`
	} `json:"aggregations"`
}

// AggregateDay pages a composite aggregation. A terms agg cannot paginate over
// 10.000 buckets; composite can, via after_key.
func (a *esUptimeAggregator) AggregateDay(ctx context.Context, start, end time.Time) (map[string]ServerFacts, error) {
	out := make(map[string]ServerFacts)
	var after map[string]any

	for {
		body, err := a.search(ctx, start, end, after)
		if err != nil {
			return nil, err
		}

		for _, b := range body.Aggregations.ByServer.Buckets {
			serverID := b.Key["server_id"]
			facts := ServerFacts{
				ServerID:     serverID,
				OnChecks:     b.OnChecks.DocCount,
				ActualChecks: b.DocCount,
			}
			// top_hits sorted checked_at desc gives the day's last fact, which
			// carries both the name at that moment and the closing status.
			if hits := b.LastFact.Hits.Hits; len(hits) > 0 {
				facts.ServerName = hits[0].Source.ServerName
				facts.LastStatus = hits[0].Source.Status
			}
			out[serverID] = facts
		}

		after = body.Aggregations.ByServer.AfterKey
		if len(after) == 0 {
			return out, nil
		}
	}
}

func (a *esUptimeAggregator) search(ctx context.Context, start, end time.Time, after map[string]any) (*aggResponse, error) {
	composite := map[string]any{
		"size": aggPageSize,
		"sources": []any{
			map[string]any{"server_id": map[string]any{"terms": map[string]any{"field": "server_id"}}},
		},
	}
	if len(after) > 0 {
		composite["after"] = after
	}

	query := map[string]any{
		"size": 0,
		"query": map[string]any{
			"range": map[string]any{
				"checked_at": map[string]any{
					"gte": start.UTC().Format(time.RFC3339),
					"lt":  end.UTC().Format(time.RFC3339),
				},
			},
		},
		"aggs": map[string]any{
			"by_server": map[string]any{
				"composite": composite,
				"aggs": map[string]any{
					"last_fact": map[string]any{
						"top_hits": map[string]any{
							"size":    1,
							"_source": []string{"server_name", "status"},
							"sort":    []any{map[string]any{"checked_at": "desc"}},
						},
					},
					"on_checks": map[string]any{
						"filter": map[string]any{"term": map[string]any{"status": "ON"}},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		return nil, fmt.Errorf("failed to encode aggregation: %w", err)
	}

	res, err := a.client.Search(
		a.client.Search.WithContext(ctx),
		a.client.Search.WithIndex(IndexPattern(a.indexPrefix, start, end)),
		a.client.Search.WithBody(&buf),
		// A window may touch a day whose index ILM already deleted.
		a.client.Search.WithIgnoreUnavailable(true),
		a.client.Search.WithAllowNoIndices(true),
	)
	if err != nil {
		return nil, fmt.Errorf("aggregation request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("aggregation returned %s", res.Status())
	}

	var body aggResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode aggregation: %w", err)
	}
	return &body, nil
}

// IndexPattern names the daily indices a window touches, rather than fanning
// the search out over every index ever written.
func IndexPattern(prefix string, start, end time.Time) string {
	var names []string
	day := time.Date(start.UTC().Year(), start.UTC().Month(), start.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for ; day.Before(end.UTC()); day = day.AddDate(0, 0, 1) {
		names = append(names, fmt.Sprintf("%s-%s", prefix, day.Format("2006.01.02")))
	}
	if len(names) == 0 {
		names = append(names, fmt.Sprintf("%s-%s", prefix, start.UTC().Format("2006.01.02")))
	}
	return strings.Join(names, ",")
}
