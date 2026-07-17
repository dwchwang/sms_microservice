package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/model"
)

// fakeIdemRepo models the claim/complete/release cycle in memory.
type fakeIdemRepo struct {
	records   map[string]*model.Idempotency
	claimErr  error
	completed int
	released  int
}

func newFakeIdemRepo() *fakeIdemRepo {
	return &fakeIdemRepo{records: make(map[string]*model.Idempotency)}
}

func idemID(actor, endpoint, key string) string { return actor + "|" + endpoint + "|" + key }

func (r *fakeIdemRepo) Claim(ctx context.Context, rec *model.Idempotency) (bool, *model.Idempotency, error) {
	if r.claimErr != nil {
		return false, nil, r.claimErr
	}
	id := idemID(rec.ActorID, rec.Endpoint, rec.IdempotencyKey)
	if existing, ok := r.records[id]; ok {
		if time.Now().UTC().After(existing.ExpiresAt) {
			copied := *rec
			r.records[id] = &copied
			return true, nil, nil
		}
		return false, existing, nil
	}
	copied := *rec
	r.records[id] = &copied
	return true, nil, nil
}

func (r *fakeIdemRepo) Complete(ctx context.Context, rec *model.Idempotency) error {
	r.completed++
	id := idemID(rec.ActorID, rec.Endpoint, rec.IdempotencyKey)
	if existing, ok := r.records[id]; ok {
		existing.State = model.IdempotencyCompleted
		existing.StatusCode = rec.StatusCode
		existing.ResponseBody = rec.ResponseBody
	}
	return nil
}

func (r *fakeIdemRepo) Release(ctx context.Context, actor, endpoint, key string) error {
	r.released++
	delete(r.records, idemID(actor, endpoint, key))
	return nil
}

// idemRouter counts handler invocations so replays are detectable.
func idemRouter(repo *fakeIdemRepo, calls *int, status int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/servers", Idempotency(repo, zerolog.New(io.Discard)), func(c *gin.Context) {
		*calls++
		c.JSON(status, gin.H{"server_id": "SRV-001"})
	})
	return r
}

func postWithKey(t *testing.T, r *gin.Engine, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest("POST", "/api/v1/servers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(idempotencyHeader, key)
	}
	req.Header.Set(actorHeader, "user-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIdempotency_RequiresKey(t *testing.T) {
	calls := 0
	r := idemRouter(newFakeIdemRepo(), &calls, http.StatusCreated)

	w := postWithKey(t, r, "", `{"server_id":"SRV-001"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
	if calls != 0 {
		t.Error("handler ran despite a missing key")
	}
}

func TestIdempotency_FirstRequestRunsHandler(t *testing.T) {
	calls := 0
	repo := newFakeIdemRepo()
	r := idemRouter(repo, &calls, http.StatusCreated)

	w := postWithKey(t, r, "key-1", `{"server_id":"SRV-001"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201", w.Code)
	}
	if calls != 1 {
		t.Errorf("handler ran %d times, want 1", calls)
	}
	if repo.completed != 1 {
		t.Errorf("completed %d times, want 1", repo.completed)
	}
}

// The point of the whole mechanism: a retried submit must replay, not re-create.
func TestIdempotency_SameKeySameBodyReplaysWithoutRerunning(t *testing.T) {
	calls := 0
	r := idemRouter(newFakeIdemRepo(), &calls, http.StatusCreated)
	body := `{"server_id":"SRV-001"}`

	first := postWithKey(t, r, "key-1", body)
	second := postWithKey(t, r, "key-1", body)

	if calls != 1 {
		t.Fatalf("handler ran %d times, want 1", calls)
	}
	if second.Code != first.Code {
		t.Errorf("replay code = %d, want %d", second.Code, first.Code)
	}
	if second.Body.String() != first.Body.String() {
		t.Errorf("replay body = %q, want %q", second.Body.String(), first.Body.String())
	}
}

func TestIdempotency_SameKeyDifferentBodyIsConflict(t *testing.T) {
	calls := 0
	r := idemRouter(newFakeIdemRepo(), &calls, http.StatusCreated)

	postWithKey(t, r, "key-1", `{"server_id":"SRV-001"}`)
	w := postWithKey(t, r, "key-1", `{"server_id":"SRV-999"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", w.Code)
	}
	if calls != 1 {
		t.Errorf("handler ran %d times, want 1", calls)
	}
}

func TestIdempotency_DifferentKeysBothRun(t *testing.T) {
	calls := 0
	r := idemRouter(newFakeIdemRepo(), &calls, http.StatusCreated)

	postWithKey(t, r, "key-1", `{"server_id":"SRV-001"}`)
	postWithKey(t, r, "key-2", `{"server_id":"SRV-002"}`)

	if calls != 2 {
		t.Errorf("handler ran %d times, want 2", calls)
	}
}

// A failed attempt must release the key so the caller can fix the input and
// retry with the same key rather than being locked out.
func TestIdempotency_FailedResponseReleasesKey(t *testing.T) {
	calls := 0
	repo := newFakeIdemRepo()
	r := idemRouter(repo, &calls, http.StatusUnprocessableEntity)

	postWithKey(t, r, "key-1", `{"bad":"input"}`)

	if repo.released != 1 {
		t.Fatalf("released %d times, want 1", repo.released)
	}
	if repo.completed != 0 {
		t.Error("a failed response must not be stored for replay")
	}
	if len(repo.records) != 0 {
		t.Error("expected the key to be free again")
	}
}

func TestIdempotency_RetryAfterFailureRunsHandlerAgain(t *testing.T) {
	calls := 0
	repo := newFakeIdemRepo()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	status := http.StatusUnprocessableEntity
	r.POST("/api/v1/servers", Idempotency(repo, zerolog.New(io.Discard)), func(c *gin.Context) {
		calls++
		c.JSON(status, gin.H{"ok": calls})
	})

	postWithKey(t, r, "key-1", `{"a":1}`)
	status = http.StatusCreated
	w := postWithKey(t, r, "key-1", `{"a":1}`)

	if calls != 2 {
		t.Fatalf("handler ran %d times, want 2", calls)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d, want 201 on the successful retry", w.Code)
	}
}

// An in-flight claim has no stored response yet, so a concurrent retry is told
// to back off rather than served an empty body.
func TestIdempotency_InFlightKeyIsConflict(t *testing.T) {
	calls := 0
	repo := newFakeIdemRepo()
	repo.records[idemID("user-1", "/api/v1/servers", "key-1")] = &model.Idempotency{
		ActorID: "user-1", Endpoint: "/api/v1/servers", IdempotencyKey: "key-1",
		RequestHash: hashOf([]byte(`{"a":1}`)),
		State:       model.IdempotencyProcessing,
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	}
	r := idemRouter(repo, &calls, http.StatusCreated)

	w := postWithKey(t, r, "key-1", `{"a":1}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", w.Code)
	}
	if calls != 0 {
		t.Error("handler ran while another request held the key")
	}
}

func TestIdempotency_ExpiredKeyIsReusable(t *testing.T) {
	calls := 0
	repo := newFakeIdemRepo()
	repo.records[idemID("user-1", "/api/v1/servers", "key-1")] = &model.Idempotency{
		ActorID: "user-1", Endpoint: "/api/v1/servers", IdempotencyKey: "key-1",
		State:     model.IdempotencyCompleted,
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}
	r := idemRouter(repo, &calls, http.StatusCreated)

	w := postWithKey(t, r, "key-1", `{"a":1}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201", w.Code)
	}
	if calls != 1 {
		t.Errorf("handler ran %d times, want 1", calls)
	}
}

// The middleware reads the body to hash it and must put it back.
func TestIdempotency_HandlerStillSeesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	got := ""
	r.POST("/api/v1/servers", Idempotency(newFakeIdemRepo(), zerolog.New(io.Discard)), func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		got = string(b)
		c.JSON(http.StatusCreated, gin.H{})
	})

	postWithKey(t, r, "key-1", `{"server_id":"SRV-001"}`)

	if got != `{"server_id":"SRV-001"}` {
		t.Errorf("handler saw body %q, want the original", got)
	}
}

func TestIdempotency_ClaimErrorIs500(t *testing.T) {
	calls := 0
	repo := newFakeIdemRepo()
	repo.claimErr = errors.New("db down")
	r := idemRouter(repo, &calls, http.StatusCreated)

	w := postWithKey(t, r, "key-1", `{"a":1}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.Code)
	}
	if calls != 0 {
		t.Error("handler ran despite the claim failing")
	}
}

// Keys are scoped per actor, so two users may pick the same key.
func TestIdempotency_KeyIsScopedPerActor(t *testing.T) {
	calls := 0
	repo := newFakeIdemRepo()
	r := idemRouter(repo, &calls, http.StatusCreated)
	body := `{"server_id":"SRV-001"}`

	postWithKey(t, r, "key-1", body)

	req, _ := http.NewRequest("POST", "/api/v1/servers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(idempotencyHeader, "key-1")
	req.Header.Set(actorHeader, "user-2")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201 for a second actor", w.Code)
	}
	if calls != 2 {
		t.Errorf("handler ran %d times, want 2", calls)
	}
}
