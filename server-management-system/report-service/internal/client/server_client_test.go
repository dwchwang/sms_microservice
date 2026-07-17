package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (ServerClient, *[]url.Values) {
	t.Helper()
	var seen []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewServerClient(srv.URL, 5*time.Second), &seen
}

const day = 24 * time.Hour

func window() (time.Time, time.Time) {
	start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	return start, start.Add(day)
}

func TestPopulation_ReturnsServers(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"servers":[
			{"server_id":"SRV-001","server_name":"web-01","created_at":"2026-07-01T00:00:00Z","deleted_at":null}
		],"next_cursor":""}}`)
	})
	start, end := window()

	got, err := c.Population(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Population failed: %v", err)
	}

	if len(got) != 1 || got[0].ServerID != "SRV-001" || got[0].ServerName != "web-01" {
		t.Fatalf("got %#v", got)
	}
	if got[0].DeletedAt != nil {
		t.Error("expected a live server to have a nil deleted_at")
	}
}

// The window bounds have to reach the API, or the population is wrong.
func TestPopulation_SendsWindowBounds(t *testing.T) {
	c, seen := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"servers":[],"next_cursor":""}}`)
	})
	start, end := window()

	if _, err := c.Population(context.Background(), start, end); err != nil {
		t.Fatalf("Population failed: %v", err)
	}

	q := (*seen)[0]
	// A server counts if it existed before the window ended and was not
	// already deleted when it began.
	if q.Get("created_before") != end.Format(time.RFC3339) {
		t.Errorf("created_before = %q, want the window end", q.Get("created_before"))
	}
	if q.Get("deleted_after") != start.Format(time.RFC3339) {
		t.Errorf("deleted_after = %q, want the window start", q.Get("deleted_after"))
	}
}

// 10.000 servers must come back as pages, not one giant URL.
func TestPopulation_FollowsTheCursor(t *testing.T) {
	page := 0
	c, seen := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		page++
		if page < 3 {
			fmt.Fprintf(w, `{"data":{"servers":[{"server_id":"SRV-%d"}],"next_cursor":"SRV-%d"}}`, page, page)
			return
		}
		fmt.Fprintf(w, `{"data":{"servers":[{"server_id":"SRV-%d"}],"next_cursor":""}}`, page)
	})
	start, end := window()

	got, err := c.Population(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Population failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d servers, want 3 across pages", len(got))
	}
	if len(*seen) != 3 {
		t.Fatalf("made %d requests, want 3", len(*seen))
	}
	if cursor := (*seen)[1].Get("cursor"); cursor != "SRV-1" {
		t.Errorf("second request cursor = %q, want SRV-1", cursor)
	}
	// The first request must not send a cursor.
	if (*seen)[0].Has("cursor") {
		t.Error("the first request should not carry a cursor")
	}
}

func TestPopulation_ErrorStatus(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	start, end := window()

	if _, err := c.Population(context.Background(), start, end); err == nil {
		t.Fatal("expected an error on a 500")
	}
}

func TestPopulation_MalformedBody(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	start, end := window()

	if _, err := c.Population(context.Background(), start, end); err == nil {
		t.Fatal("expected an error on a malformed body")
	}
}

func TestPopulation_RespectsContext(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"servers":[],"next_cursor":""}}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start, end := window()

	if _, err := c.Population(ctx, start, end); err == nil {
		t.Fatal("expected an error on a cancelled context")
	}
}
