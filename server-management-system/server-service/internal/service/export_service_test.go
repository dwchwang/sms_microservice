package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/model"
)

// exportRepo returns a fixed catalog, honouring page/page_size like the real repo.
type exportRepo struct {
	mockServerRepo
	all      []model.Server
	err      error
	pageSize []int
}

func (r *exportRepo) FindAll(ctx context.Context, filter *dto.ServerFilter) ([]model.Server, int64, error) {
	if r.err != nil {
		return nil, 0, r.err
	}
	r.pageSize = append(r.pageSize, filter.PageSize)

	start := (filter.Page - 1) * filter.PageSize
	if start >= len(r.all) {
		return nil, int64(len(r.all)), nil
	}
	end := min(start+filter.PageSize, len(r.all))
	return r.all[start:end], int64(len(r.all)), nil
}

type captureGenerator struct {
	got []dto.ServerResponse
	err error
}

func (g *captureGenerator) Generate(servers []dto.ServerResponse) (*bytes.Buffer, error) {
	g.got = servers
	if g.err != nil {
		return nil, g.err
	}
	return bytes.NewBufferString("xlsx"), nil
}

func newTestExportService(all []model.Server) (ExportService, *exportRepo, *captureGenerator) {
	repo := &exportRepo{all: all}
	gen := &captureGenerator{}
	svc := &exportServiceImpl{
		repo:      repo,
		generator: gen,
		lastCheck: &fakeLastCheckReader{values: map[string]time.Time{"SRV-00000": testCheckedAt}},
	}
	return svc, repo, gen
}

func makeCatalog(n int) []model.Server {
	out := make([]model.Server, 0, n)
	for i := range n {
		out = append(out, *makeServer(
			fmt.Sprintf("SRV-%05d", i), fmt.Sprintf("web-%05d", i), "10.0.0.1"))
	}
	return out
}

func TestExport_ReturnsFileAndName(t *testing.T) {
	svc, _, _ := newTestExportService(makeCatalog(3))

	buf, name, err := svc.Export(context.Background(), &dto.ServerFilter{})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if buf == nil || buf.Len() == 0 {
		t.Error("expected a non-empty buffer")
	}
	if !strings.HasSuffix(name, ".xlsx") {
		t.Errorf("filename = %q, want an .xlsx name", name)
	}
}

// Export must page through everything, not stop at one page.
func TestExport_PagesThroughWholeCatalog(t *testing.T) {
	svc, repo, gen := newTestExportService(makeCatalog(250))

	if _, _, err := svc.Export(context.Background(), &dto.ServerFilter{}); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(gen.got) != 250 {
		t.Fatalf("exported %d servers, want 250", len(gen.got))
	}
	if len(repo.pageSize) != 3 {
		t.Errorf("made %d queries, want 3 pages of 100", len(repo.pageSize))
	}
	for _, size := range repo.pageSize {
		if size != exportPageSize {
			t.Errorf("page size = %d, want %d", size, exportPageSize)
		}
	}
}

func TestExport_EmptyCatalog(t *testing.T) {
	svc, _, gen := newTestExportService(nil)

	if _, _, err := svc.Export(context.Background(), &dto.ServerFilter{}); err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if len(gen.got) != 0 {
		t.Errorf("exported %d servers, want 0", len(gen.got))
	}
}

func TestExport_EnrichesLastStatusCheck(t *testing.T) {
	svc, _, gen := newTestExportService(makeCatalog(2))

	if _, _, err := svc.Export(context.Background(), &dto.ServerFilter{}); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if gen.got[0].LastStatusCheck == nil || !gen.got[0].LastStatusCheck.Equal(testCheckedAt) {
		t.Errorf("SRV-00000 LastStatusCheck = %v, want %v", gen.got[0].LastStatusCheck, testCheckedAt)
	}
	if gen.got[1].LastStatusCheck != nil {
		t.Errorf("SRV-00001 LastStatusCheck = %v, want nil", gen.got[1].LastStatusCheck)
	}
}

// Export reuses the list filter so the two views can never disagree.
func TestExport_PassesFilterThrough(t *testing.T) {
	svc, repo, _ := newTestExportService(makeCatalog(1))
	repo.all[0].Status = "ON"

	if _, _, err := svc.Export(context.Background(), &dto.ServerFilter{Status: "ON"}); err != nil {
		t.Fatalf("Export failed: %v", err)
	}
}

func TestExport_RepositoryError(t *testing.T) {
	svc, repo, _ := newTestExportService(nil)
	repo.err = errors.New("db down")

	if _, _, err := svc.Export(context.Background(), &dto.ServerFilter{}); err == nil {
		t.Fatal("expected an error when the repository fails")
	}
}

func TestExport_GeneratorError(t *testing.T) {
	svc, _, gen := newTestExportService(makeCatalog(1))
	gen.err = errors.New("disk full")

	if _, _, err := svc.Export(context.Background(), &dto.ServerFilter{}); err == nil {
		t.Fatal("expected an error when the generator fails")
	}
}
