package database

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// indexMapping defines the Elasticsearch mapping for server-status-logs index.
// Critical: server_id, server_name, status, check_method must be "keyword" type
// for aggregations (terms, bucket_script) to work correctly.
const indexMapping = `{
  "settings": {
    "number_of_shards": 1,
    "number_of_replicas": 0,
    "refresh_interval": "5s"
  },
  "mappings": {
    "properties": {
      "server_id": {
        "type": "keyword"
      },
      "server_name": {
        "type": "keyword"
      },
      "status": {
        "type": "keyword"
      },
      "checked_at": {
        "type": "date",
        "format": "strict_date_optional_time||epoch_millis"
      },
      "response_time_ms": {
        "type": "integer"
      },
      "check_method": {
        "type": "keyword"
      },
      "error": {
        "type": "text"
      }
    }
  }
}`

// EnsureIndexMapping ensures the Elasticsearch index exists with the correct mapping.
// If the index already exists, it checks if the mapping is correct and updates it if needed.
// This MUST be called before any documents are bulk-indexed, otherwise dynamic mapping
// will map string fields as "text" which breaks aggregation queries.
func EnsureIndexMapping(ctx context.Context, client *elasticsearch.Client, indexName string) error {
	// 1. Check if index exists
	res, err := client.Indices.Exists([]string{indexName}, client.Indices.Exists.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to check index existence: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == 200 {
		// Index exists — check if mapping needs update
		return ensureMappingCorrect(ctx, client, indexName)
	}

	// 2. Index does not exist — create it with proper mapping
	return createIndexWithMapping(ctx, client, indexName)
}

// createIndexWithMapping creates the index with the predefined mapping.
func createIndexWithMapping(ctx context.Context, client *elasticsearch.Client, indexName string) error {
	res, err := client.Indices.Create(
		indexName,
		client.Indices.Create.WithContext(ctx),
		client.Indices.Create.WithBody(strings.NewReader(indexMapping)),
	)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("index creation failed: [%s] %s", res.Status(), string(body))
	}

	fmt.Printf("[ES] Created index %q with proper mapping (keyword fields)\n", indexName)
	return nil
}

// ensureMappingCorrect checks if the existing index has the correct mapping and fixes it if not.
func ensureMappingCorrect(ctx context.Context, client *elasticsearch.Client, indexName string) error {
	// Get current mapping
	res, err := client.Indices.GetMapping(
		client.Indices.GetMapping.WithContext(ctx),
		client.Indices.GetMapping.WithIndex(indexName),
	)
	if err != nil {
		return fmt.Errorf("failed to get index mapping: %w", err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)

	// Check if server_id is mapped as "keyword" — if it's "text", we need to fix it
	// We can detect this by checking if the mapping contains "type":"text" for server_id
	if strings.Contains(string(body), `"server_id":{"type":"text"`) ||
		!strings.Contains(string(body), `"server_id":{"type":"keyword"`) {

		fmt.Printf("[ES] Index %q has incorrect mapping (server_id is text, not keyword). Updating...\n", indexName)

		// Unfortunately, you cannot change the type of an existing field in ES.
		// The safest approach is to re-create the index with proper mapping.
		// First, delete the existing index.
		delRes, err := client.Indices.Delete([]string{indexName},
			client.Indices.Delete.WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf("failed to delete old index: %w", err)
		}
		delRes.Body.Close()

		// Re-create with proper mapping
		time.Sleep(1 * time.Second) // Wait for ES to process deletion
		if err := createIndexWithMapping(ctx, client, indexName); err != nil {
			return err
		}
		fmt.Printf("[ES] Index %q re-created with keyword mapping\n", indexName)
		return nil
	}

	// Check if server_id is in the mapping at all (empty mapping = auto-created)
	var mappingResponse map[string]interface{}
	if err := json.Unmarshal(body, &mappingResponse); err == nil {
		indexData, ok := mappingResponse[indexName]
		if ok {
			indexMap, ok := indexData.(map[string]interface{})
			if ok {
				mappings, ok := indexMap["mappings"]
				if !ok || mappings == nil || mappings == "" {
					// Empty mapping — need to update
					fmt.Printf("[ES] Index %q has empty mapping. Updating...\n", indexName)
					return updateIndexMapping(ctx, client, indexName)
				}
			}
		}
	}

	fmt.Printf("[ES] Index %q mapping is correct\n", indexName)
	return nil
}

// updateIndexMapping updates the mapping for an existing index.
// Note: only new fields can be added; existing field types cannot be changed.
// If server_id is already text, the index must be deleted and re-created.
func updateIndexMapping(ctx context.Context, client *elasticsearch.Client, indexName string) error {
	mappingUpdate := `{
  "properties": {
    "server_id": {
      "type": "keyword"
    },
    "server_name": {
      "type": "keyword"
    },
    "status": {
      "type": "keyword"
    },
    "checked_at": {
      "type": "date",
      "format": "strict_date_optional_time||epoch_millis"
    },
    "response_time_ms": {
      "type": "integer"
    },
    "check_method": {
      "type": "keyword"
    },
    "error": {
      "type": "text"
    }
  }
}`

	res, err := client.Indices.PutMapping(
		[]string{indexName},
		strings.NewReader(mappingUpdate),
		client.Indices.PutMapping.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to update mapping: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		// If the error is about changing existing field type, we need to re-create
		if strings.Contains(string(body), "can't change type") ||
			strings.Contains(string(body), "mapper_parsing_exception") {
			fmt.Printf("[ES] Cannot update existing field types. Re-creating index %q...\n", indexName)

			// Delete and re-create
			delRes, _ := client.Indices.Delete([]string{indexName}, client.Indices.Delete.WithContext(ctx))
			if delRes != nil {
				delRes.Body.Close()
			}
			time.Sleep(1 * time.Second)

			return createIndexWithMapping(ctx, client, indexName)
		}
		return fmt.Errorf("mapping update failed: [%s] %s", res.Status(), string(body))
	}

	fmt.Printf("[ES] Updated mapping for index %q\n", indexName)
	return nil
}
