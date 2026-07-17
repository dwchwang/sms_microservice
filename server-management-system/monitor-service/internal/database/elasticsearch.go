package database

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// indexTemplate maps every daily health-fact index. A template is used rather
// than creating each index by hand: indices are created daily by the first bulk
// write, and without a template they would be mapped dynamically, turning the
// keyword fields into text and breaking the uptime aggregation.
const indexTemplate = `{
  "index_patterns": ["%s-*"],
  "template": {
    "settings": {
      "number_of_shards": 1,
      "number_of_replicas": 0,
      "refresh_interval": "5s"
    },
    "mappings": {
      "properties": {
        "server_id":   { "type": "keyword" },
        "server_name": { "type": "keyword" },
        "status":      { "type": "keyword" },
        "checked_at":  { "type": "date", "format": "strict_date_optional_time||epoch_millis" },
        "round_id":    { "type": "long" },
        "latency_ms":  { "type": "integer" },
        "error_code":  { "type": "keyword" }
      }
    }
  }
}`

// ESConfig is the subset of config needed to reach Elasticsearch.
type ESConfig struct {
	Addresses   string
	Username    string
	Password    string
	IndexPrefix string
}

// ConnectES creates an Elasticsearch client and verifies it answers.
func ConnectES(cfg ESConfig) (*elasticsearch.Client, error) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: strings.Split(cfg.Addresses, ","),
		Username:  cfg.Username,
		Password:  cfg.Password,
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   32,
			ResponseHeaderTimeout: 10 * time.Second,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ES client: %w", err)
	}

	res, err := client.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to reach ES: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("ES info returned %s", res.Status())
	}
	return client, nil
}

// EnsureIndexTemplate installs the mapping template for the daily indices.
func EnsureIndexTemplate(ctx context.Context, client *elasticsearch.Client, prefix string) error {
	body := fmt.Sprintf(indexTemplate, prefix)

	res, err := client.Indices.PutIndexTemplate(
		prefix,
		strings.NewReader(body),
		client.Indices.PutIndexTemplate.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to put index template: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		msg, _ := io.ReadAll(res.Body)
		return fmt.Errorf("index template rejected: [%s] %s", res.Status(), msg)
	}
	return nil
}
