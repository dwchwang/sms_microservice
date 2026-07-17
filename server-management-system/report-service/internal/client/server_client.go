package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const pageLimit = 1000

// PopulationServer is one server that existed during a report window.
type PopulationServer struct {
	ServerID   string     `json:"server_id"`
	ServerName string     `json:"server_name"`
	CreatedAt  time.Time  `json:"created_at"`
	DeletedAt  *time.Time `json:"deleted_at"`
}

// ServerClient reads the reporting population from Server Service.
type ServerClient interface {
	// Population lists every server alive during [startAt, endAt), including
	// ones deleted inside the window.
	Population(ctx context.Context, startAt, endAt time.Time) ([]PopulationServer, error)
}

type httpServerClient struct {
	baseURL string
	http    *http.Client
}

// NewServerClient creates a ServerClient against the internal API.
func NewServerClient(baseURL string, timeout time.Duration) ServerClient {
	return &httpServerClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: timeout},
	}
}

type populationResponse struct {
	Data struct {
		Servers    []PopulationServer `json:"servers"`
		NextCursor string             `json:"next_cursor"`
	} `json:"data"`
}

// Population walks the cursor until the pages run out, so 10.000 servers cost
// 10 requests instead of one unbounded URL.
func (c *httpServerClient) Population(ctx context.Context, startAt, endAt time.Time) ([]PopulationServer, error) {
	var all []PopulationServer
	cursor := ""

	for {
		page, next, err := c.fetchPage(ctx, startAt, endAt, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next == "" {
			return all, nil
		}
		cursor = next
	}
}

func (c *httpServerClient) fetchPage(ctx context.Context, startAt, endAt time.Time, cursor string) ([]PopulationServer, string, error) {
	q := url.Values{}
	// A server belongs to the window if it existed before the window ended and
	// was not already deleted when it began.
	q.Set("created_before", endAt.UTC().Format(time.RFC3339))
	q.Set("deleted_after", startAt.UTC().Format(time.RFC3339))
	q.Set("limit", strconv.Itoa(pageLimit))
	if cursor != "" {
		q.Set("cursor", cursor)
	}

	endpoint := fmt.Sprintf("%s/internal/servers?%s", c.baseURL, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build population request: %w", err)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("population request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("population request returned %s", res.Status)
	}

	var body populationResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, "", fmt.Errorf("failed to decode population response: %w", err)
	}
	return body.Data.Servers, body.Data.NextCursor, nil
}
