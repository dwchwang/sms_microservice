package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/server-service/internal/repository"
	"gorm.io/gorm"
)

type populationRepo struct {
	repository.ServerRepository
	servers []model.Server
	gotQ    repository.PopulationQuery
	err     error
}

func (r *populationRepo) FindPopulation(ctx context.Context, q repository.PopulationQuery) ([]model.Server, error) {
	r.gotQ = q
	if r.err != nil {
		return nil, r.err
	}
	return r.servers, nil
}

func populationRouter(repo repository.ServerRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/internal/servers", NewInternalHandler(repo).ListPopulation)
	return r
}

type populationEnvelope struct {
	Data dto.PopulationResponse `json:"data"`
}

func getPopulation(t *testing.T, repo repository.ServerRepository, query string) (*httptest.ResponseRecorder, populationEnvelope) {
	t.Helper()
	req, _ := http.NewRequest("GET", "/internal/servers"+query, nil)
	w := httptest.NewRecorder()
	populationRouter(repo).ServeHTTP(w, req)

	var env populationEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	return w, env
}

const validPopulationQuery = "?created_before=2026-07-16T00:00:00Z&deleted_after=2026-07-15T00:00:00Z"

func TestListPopulation_ReturnsServers(t *testing.T) {
	deleted := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	repo := &populationRepo{servers: []model.Server{
		{ServerID: "SRV-001", ServerName: "web-01", CreatedAt: time.Now().UTC()},
		{ServerID: "SRV-002", ServerName: "web-02", CreatedAt: time.Now().UTC(),
			DeletedAt: gorm.DeletedAt{Time: deleted, Valid: true}},
	}}

	w, env := getPopulation(t, repo, validPopulationQuery)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}
	if len(env.Data.Servers) != 2 {
		t.Fatalf("got %d servers, want 2", len(env.Data.Servers))
	}
	// A server deleted during the window still belongs to the population.
	if env.Data.Servers[1].DeletedAt == nil || !env.Data.Servers[1].DeletedAt.Equal(deleted) {
		t.Errorf("DeletedAt = %v, want %v", env.Data.Servers[1].DeletedAt, deleted)
	}
	if env.Data.Servers[0].DeletedAt != nil {
		t.Errorf("expected a live server to have a null deleted_at")
	}
}

func TestListPopulation_PassesQueryThrough(t *testing.T) {
	repo := &populationRepo{}

	getPopulation(t, repo, validPopulationQuery+"&cursor=SRV-005&limit=250")

	if repo.gotQ.Cursor != "SRV-005" {
		t.Errorf("Cursor = %q, want SRV-005", repo.gotQ.Cursor)
	}
	if repo.gotQ.Limit != 250 {
		t.Errorf("Limit = %d, want 250", repo.gotQ.Limit)
	}
	if !repo.gotQ.CreatedBefore.Equal(time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("CreatedBefore = %v", repo.gotQ.CreatedBefore)
	}
}

// A full page hands back a cursor; a short page ends the walk.
func TestListPopulation_NextCursorOnlyWhenPageIsFull(t *testing.T) {
	full := make([]model.Server, 0, maxPopulationLimit)
	for i := range maxPopulationLimit {
		full = append(full, model.Server{ServerID: fmt.Sprintf("SRV-%05d", i)})
	}
	repo := &populationRepo{servers: full}

	_, env := getPopulation(t, repo, validPopulationQuery)
	if env.Data.NextCursor != fmt.Sprintf("SRV-%05d", maxPopulationLimit-1) {
		t.Errorf("NextCursor = %q, want the last server_id", env.Data.NextCursor)
	}

	repo.servers = full[:3]
	_, env = getPopulation(t, repo, validPopulationQuery)
	if env.Data.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty on a short page", env.Data.NextCursor)
	}
}

func TestListPopulation_ClampsLimit(t *testing.T) {
	repo := &populationRepo{}

	getPopulation(t, repo, validPopulationQuery+"&limit=99999")

	if repo.gotQ.Limit != maxPopulationLimit {
		t.Errorf("Limit = %d, want clamped to %d", repo.gotQ.Limit, maxPopulationLimit)
	}
}

func TestListPopulation_DefaultsLimit(t *testing.T) {
	repo := &populationRepo{}

	getPopulation(t, repo, validPopulationQuery)

	if repo.gotQ.Limit != defaultPopulationLimit {
		t.Errorf("Limit = %d, want %d", repo.gotQ.Limit, defaultPopulationLimit)
	}
}

// Missing dates must fail loudly: defaulting them would return an empty
// population and silently drop every server from the report.
func TestListPopulation_RejectsMissingOrBadDates(t *testing.T) {
	cases := map[string]string{
		"both missing":        "",
		"created_before only": "?created_before=2026-07-16T00:00:00Z",
		"deleted_after only":  "?deleted_after=2026-07-15T00:00:00Z",
		"created_before junk": "?created_before=yesterday&deleted_after=2026-07-15T00:00:00Z",
		"deleted_after junk":  "?created_before=2026-07-16T00:00:00Z&deleted_after=soon",
		"not RFC3339":         "?created_before=2026-07-16&deleted_after=2026-07-15",
	}

	for name, query := range cases {
		w, _ := getPopulation(t, &populationRepo{}, query)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: code = %d, want 422", name, w.Code)
		}
	}
}

func TestListPopulation_RejectsBadLimit(t *testing.T) {
	for _, limit := range []string{"0", "-5", "abc"} {
		w, _ := getPopulation(t, &populationRepo{}, validPopulationQuery+"&limit="+limit)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("limit=%s: code = %d, want 422", limit, w.Code)
		}
	}
}

func TestListPopulation_RepositoryError(t *testing.T) {
	repo := &populationRepo{err: errors.New("db down")}

	w, _ := getPopulation(t, repo, validPopulationQuery)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", w.Code)
	}
}

func TestListPopulation_EmptyResultIsEmptyArray(t *testing.T) {
	_, env := getPopulation(t, &populationRepo{}, validPopulationQuery)

	if env.Data.Servers == nil {
		t.Error("expected an empty array rather than null")
	}
}
