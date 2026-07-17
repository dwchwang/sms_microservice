package consumer

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/repository"
)

type fakeApplier struct {
	applied []repository.StatusUpdate
	rows    int64
	err     error
}

func (f *fakeApplier) ApplyStatusEvent(ctx context.Context, u repository.StatusUpdate) (int64, error) {
	f.applied = append(f.applied, u)
	if f.err != nil {
		return 0, f.err
	}
	return f.rows, nil
}

func newTestConsumer(rows int64, err error) (*StatusConsumer, *fakeApplier) {
	repo := &fakeApplier{rows: rows, err: err}
	c := &StatusConsumer{repo: repo, name: "test-host", log: zerolog.New(io.Discard)}
	return c, repo
}

func validMessage() redis.XMessage {
	return redis.XMessage{
		ID: "1700000000-0",
		Values: map[string]any{
			"event_type":     "status.changed",
			"server_id":      "SRV-001",
			"status":         "ON",
			"changed_at":     "2026-07-16T10:00:00Z",
			"checked_at":     "2026-07-16T10:00:00Z",
			"status_version": "29166666",
		},
	}
}

func TestApply_AppliesAndAcksValidEvent(t *testing.T) {
	c, repo := newTestConsumer(1, nil)

	ack, changed := c.apply(context.Background(), validMessage())

	if !ack || !changed {
		t.Fatalf("ack=%v changed=%v, want true/true", ack, changed)
	}
	if len(repo.applied) != 1 {
		t.Fatalf("expected 1 apply, got %d", len(repo.applied))
	}
	got := repo.applied[0]
	if got.ServerID != "SRV-001" || got.Status != "ON" || got.StatusVersion != 29166666 {
		t.Errorf("unexpected update %#v", got)
	}
	if !got.ChangedAt.Equal(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("ChangedAt = %v", got.ChangedAt)
	}
	if got.StreamID != "1700000000-0" {
		t.Errorf("StreamID = %q, want the message ID", got.StreamID)
	}
}

// A stale or duplicate event is rejected by the version guard: still acked,
// but it must not bump the cache version.
func TestApply_AcksStaleEventWithoutBumping(t *testing.T) {
	c, _ := newTestConsumer(0, nil)

	ack, changed := c.apply(context.Background(), validMessage())

	if !ack {
		t.Error("expected a stale event to be acked")
	}
	if changed {
		t.Error("expected no cache bump when no row changed")
	}
}

// A database failure must leave the message pending so it gets reclaimed.
func TestApply_DoesNotAckWhenApplyFails(t *testing.T) {
	c, _ := newTestConsumer(0, errors.New("db down"))

	ack, changed := c.apply(context.Background(), validMessage())

	if ack {
		t.Error("expected no ack when the database fails")
	}
	if changed {
		t.Error("expected no cache bump when the database fails")
	}
}

// An unparseable message is acked, or it would be redelivered forever.
func TestApply_AcksMalformedEvent(t *testing.T) {
	c, repo := newTestConsumer(1, nil)
	msg := validMessage()
	msg.Values["status"] = "BOGUS"

	ack, changed := c.apply(context.Background(), msg)

	if !ack {
		t.Error("expected a malformed event to be acked")
	}
	if changed {
		t.Error("expected no cache bump for a malformed event")
	}
	if len(repo.applied) != 0 {
		t.Error("expected a malformed event not to reach the database")
	}
}

func TestParseStatusEvent_Rejects(t *testing.T) {
	cases := map[string]func(m *redis.XMessage){
		"wrong event type":    func(m *redis.XMessage) { m.Values["event_type"] = "server.created" },
		"missing event type":  func(m *redis.XMessage) { delete(m.Values, "event_type") },
		"missing server_id":   func(m *redis.XMessage) { delete(m.Values, "server_id") },
		"empty server_id":     func(m *redis.XMessage) { m.Values["server_id"] = "" },
		"unknown status":      func(m *redis.XMessage) { m.Values["status"] = "UNKNOWN" },
		"missing status":      func(m *redis.XMessage) { delete(m.Values, "status") },
		"version not numeric": func(m *redis.XMessage) { m.Values["status_version"] = "abc" },
		"missing version":     func(m *redis.XMessage) { delete(m.Values, "status_version") },
		"changed_at garbage":  func(m *redis.XMessage) { m.Values["changed_at"] = "yesterday" },
		"missing changed_at":  func(m *redis.XMessage) { delete(m.Values, "changed_at") },
	}

	for name, mutate := range cases {
		msg := validMessage()
		mutate(&msg)
		if _, err := parseStatusEvent(msg); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

// Monitoring only publishes real transitions, so UNKNOWN is never a valid
// incoming status even though it is a valid column value.
func TestParseStatusEvent_AcceptsOnAndOff(t *testing.T) {
	for _, status := range []string{"ON", "OFF"} {
		msg := validMessage()
		msg.Values["status"] = status
		u, err := parseStatusEvent(msg)
		if err != nil {
			t.Fatalf("status %s: unexpected error %v", status, err)
		}
		if u.Status != status {
			t.Errorf("Status = %q, want %q", u.Status, status)
		}
	}
}

func TestParseStatusEvent_NormalisesChangedAtToUTC(t *testing.T) {
	msg := validMessage()
	msg.Values["changed_at"] = "2026-07-16T17:00:00+07:00"

	u, err := parseStatusEvent(msg)
	if err != nil {
		t.Fatalf("parseStatusEvent failed: %v", err)
	}

	want := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	if !u.ChangedAt.Equal(want) {
		t.Errorf("ChangedAt = %v, want %v", u.ChangedAt, want)
	}
}

// Losing the consumer group is not hypothetical: a Redis restart without
// persistence, a FLUSHDB, or maxmemory eviction all destroy it. Without
// detection the consumer loops on NOGROUP forever while Monitoring keeps
// publishing, and PostgreSQL silently stops tracking status.
func TestIsMissingGroup(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"group gone": {
			errors.New("NOGROUP No such key 'stream:monitor.status' or consumer group 'server-svc' in XREADGROUP with GROUP option"),
			true,
		},
		"stream gone": {errors.New("NOGROUP No such key 'stream:monitor.status'"), true},
		"other redis": {errors.New("READONLY You can't write against a read only replica"), false},
		"connection":  {errors.New("dial tcp: connect: connection refused"), false},
		"nil":         {nil, false},
	}

	for name, tc := range cases {
		if got := isMissingGroup(tc.err); got != tc.want {
			t.Errorf("%s: isMissingGroup = %v, want %v", name, got, tc.want)
		}
	}
}
