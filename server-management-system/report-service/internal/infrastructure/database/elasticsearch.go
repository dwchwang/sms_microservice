package database

import (
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/vcs-sms/report-service/config"
)

// ConnectES creates an Elasticsearch client and verifies it answers.
func ConnectES(cfg config.ESConfig) (*elasticsearch.Client, error) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: strings.Split(cfg.Addresses, ","),
		Username:  cfg.Username,
		Password:  cfg.Password,
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
