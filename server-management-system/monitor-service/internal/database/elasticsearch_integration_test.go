package database

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// ES rejects a bad ILM policy at runtime, not at compile time, so this needs a
// real cluster. Run: docker run -d --rm -e discovery.type=single-node \
//   -e xpack.security.enabled=false -p 59200:9200 \
//   docker.elastic.co/elasticsearch/elasticsearch:8.12.0
// then set MONITOR_TEST_ES_URL=http://localhost:59200
const testESEnv = "MONITOR_TEST_ES_URL"

func setupES(t *testing.T) (*elasticsearch.Client, string) {
	t.Helper()
	addr := os.Getenv(testESEnv)
	if addr == "" {
		t.Skipf("%s is not set", testESEnv)
	}

	client, err := elasticsearch.NewClient(elasticsearch.Config{Addresses: []string{addr}})
	if err != nil {
		t.Fatalf("failed to build client: %v", err)
	}

	prefix := "test-health-facts"
	t.Cleanup(func() {
		client.Indices.DeleteIndexTemplate(prefix)
		client.ILM.DeleteLifecycle(PolicyName(prefix))
	})
	return client, prefix
}

func TestIntegration_EnsureIndexTemplateInstallsILM(t *testing.T) {
	client, prefix := setupES(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := EnsureIndexTemplate(ctx, client, prefix); err != nil {
		t.Fatalf("EnsureIndexTemplate failed: %v", err)
	}

	res, err := client.ILM.GetLifecycle(client.ILM.GetLifecycle.WithPolicy(PolicyName(prefix)))
	if err != nil {
		t.Fatalf("failed to read policy: %v", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		t.Fatalf("policy was not installed: %s", res.Status())
	}

	var body map[string]struct {
		Policy struct {
			Phases struct {
				Delete struct {
					MinAge string `json:"min_age"`
				} `json:"delete"`
			} `json:"phases"`
		} `json:"policy"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode policy: %v", err)
	}
	if got := body[PolicyName(prefix)].Policy.Phases.Delete.MinAge; got != "7d" {
		t.Errorf("delete min_age = %q, want 7d (design §12.4)", got)
	}
}

// A new daily index must inherit the policy from the template, not need binding by hand.
func TestIntegration_DailyIndexInheritsPolicyAndMapping(t *testing.T) {
	client, prefix := setupES(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := EnsureIndexTemplate(ctx, client, prefix); err != nil {
		t.Fatalf("EnsureIndexTemplate failed: %v", err)
	}

	index := prefix + "-2026.07.17"
	t.Cleanup(func() { client.Indices.Delete([]string{index}) })

	create, err := client.Indices.Create(index)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer create.Body.Close()
	if create.IsError() {
		t.Fatalf("index creation failed: %s", create.Status())
	}

	res, err := client.Indices.GetSettings(client.Indices.GetSettings.WithIndex(index))
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	defer res.Body.Close()

	var settings map[string]struct {
		Settings struct {
			Index struct {
				Lifecycle struct {
					Name string `json:"name"`
				} `json:"lifecycle"`
			} `json:"index"`
		} `json:"settings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&settings); err != nil {
		t.Fatalf("failed to decode settings: %v", err)
	}
	if got := settings[index].Settings.Index.Lifecycle.Name; got != PolicyName(prefix) {
		t.Errorf("index.lifecycle.name = %q, want %q", got, PolicyName(prefix))
	}

	// status must stay keyword or the uptime aggregation silently returns 0.
	mres, err := client.Indices.GetMapping(client.Indices.GetMapping.WithIndex(index))
	if err != nil {
		t.Fatalf("failed to read mapping: %v", err)
	}
	defer mres.Body.Close()

	var mapping map[string]struct {
		Mappings struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"mappings"`
	}
	if err := json.NewDecoder(mres.Body).Decode(&mapping); err != nil {
		t.Fatalf("failed to decode mapping: %v", err)
	}
	for _, field := range []string{"server_id", "server_name", "status"} {
		if got := mapping[index].Mappings.Properties[field].Type; got != "keyword" {
			t.Errorf("%s mapped as %q, want keyword", field, got)
		}
	}
}
