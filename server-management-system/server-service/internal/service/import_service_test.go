package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/infrastructure/excel"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/server-service/internal/repository"
	"gorm.io/gorm"
)

// fakeParser returns canned rows instead of reading a file.
type fakeParser struct {
	rows []excel.ParsedRow
	err  error
}

func (p *fakeParser) Parse(r io.Reader) ([]excel.ParsedRow, error) {
	return p.rows, p.err
}

// importRepo models the insert behaviour the import service depends on.
type importRepo struct {
	mockServerRepo
	existingNames  []string
	existingNamErr error
	// takenIDs are rejected by ON CONFLICT (server_id).
	takenIDs map[string]bool
	// nameClash makes InsertBatch abort the way a real 23505 would.
	nameClash map[string]bool
	batchErr  error
	batches   [][]string
	perRow    []string
}

func newImportRepo() *importRepo {
	return &importRepo{
		takenIDs:  make(map[string]bool),
		nameClash: make(map[string]bool),
	}
}

func nameViolation() error {
	return &pgconn.PgError{Code: "23505", ConstraintName: repository.ActiveNameConstraint}
}

func (r *importRepo) FindExistingNames(ctx context.Context, names []string) ([]string, error) {
	if r.existingNamErr != nil {
		return nil, r.existingNamErr
	}
	var out []string
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	for _, n := range r.existingNames {
		if wanted[n] {
			out = append(out, n)
		}
	}
	return out, nil
}

func (r *importRepo) InsertBatch(ctx context.Context, servers []model.Server) ([]string, error) {
	ids := make([]string, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, s.ServerID)
	}
	r.batches = append(r.batches, ids)

	if r.batchErr != nil {
		return nil, r.batchErr
	}
	// A single clashing row aborts the whole statement, as PostgreSQL would.
	for _, s := range servers {
		if r.nameClash[s.ServerName] {
			return nil, nameViolation()
		}
	}

	var inserted []string
	for _, s := range servers {
		if !r.takenIDs[s.ServerID] {
			inserted = append(inserted, s.ServerID)
		}
	}
	return inserted, nil
}

func (r *importRepo) InsertOne(ctx context.Context, s *model.Server) error {
	r.perRow = append(r.perRow, s.ServerID)
	if r.nameClash[s.ServerName] {
		return nameViolation()
	}
	if r.takenIDs[s.ServerID] {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func newTestImportService(rows []excel.ParsedRow) (ImportService, *importRepo) {
	repo := newImportRepo()
	svc := &importServiceImpl{
		repo:    repo,
		parser:  &fakeParser{rows: rows},
		cidr:    testCIDRValidator(),
		targets: newFakeTargetProjection(),
		log:     zerolog.New(io.Discard),
	}
	return svc, repo
}

func goodRow(n int, id, name, ip string) excel.ParsedRow {
	return excel.ParsedRow{
		RowNumber: n, ServerID: id, ServerName: name,
		IPv4: ip, TCPPort: 8080, IsValid: true,
	}
}

func TestImport_InsertsValidRows(t *testing.T) {
	svc, _ := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "10.0.0.1"),
		goodRow(3, "SRV-002", "web-02", "10.0.0.2"),
	})

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.TotalRows != 2 {
		t.Errorf("TotalRows = %d, want 2", resp.TotalRows)
	}
	if resp.Succeeded.Count != 2 {
		t.Fatalf("Succeeded = %d, want 2", resp.Succeeded.Count)
	}
	if resp.Failed.Count != 0 || resp.SkippedDuplicate.Count != 0 {
		t.Errorf("unexpected failed=%d skipped=%d", resp.Failed.Count, resp.SkippedDuplicate.Count)
	}
}

func TestImport_RejectsBadFile(t *testing.T) {
	svc, _ := newTestImportService(nil)
	svc.(*importServiceImpl).parser = &fakeParser{err: errors.New("not xlsx")}

	_, err := svc.Import(context.Background(), strings.NewReader(""))

	if !errors.Is(err, ErrImportFileRejected) {
		t.Fatalf("err = %v, want ErrImportFileRejected", err)
	}
}

func TestImport_ReportsInvalidRowsAsFailed(t *testing.T) {
	svc, _ := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "10.0.0.1"),
		{RowNumber: 3, ServerID: "SRV-002", IsValid: false, ErrorCode: "INVALID_IPV4"},
	})

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Succeeded.Count != 1 {
		t.Errorf("Succeeded = %d, want 1", resp.Succeeded.Count)
	}
	if resp.Failed.Count != 1 {
		t.Fatalf("Failed = %d, want 1", resp.Failed.Count)
	}
	got := resp.Failed.Items[0]
	if got.Row != 3 || got.Reason != "INVALID_IPV4" || got.ServerID != "SRV-002" {
		t.Errorf("unexpected failed item %#v", got)
	}
}

func TestImport_RejectsRowOutsideCIDRAllowlist(t *testing.T) {
	svc, _ := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "127.0.0.1"),
	})

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Failed.Count != 1 || resp.Failed.Items[0].Reason != "SERVER_IP_NOT_ALLOWED" {
		t.Fatalf("expected SERVER_IP_NOT_ALLOWED, got %#v", resp.Failed)
	}
}

// Duplicates inside the file itself keep the first occurrence.
func TestImport_DedupesWithinFile(t *testing.T) {
	svc, _ := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "10.0.0.1"),
		goodRow(3, "SRV-001", "web-dup-id", "10.0.0.2"),
		goodRow(4, "SRV-002", "web-01", "10.0.0.3"),
	})

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Succeeded.Count != 1 || resp.Succeeded.Items[0] != "SRV-001" {
		t.Errorf("Succeeded = %#v, want just SRV-001", resp.Succeeded)
	}
	if resp.SkippedDuplicate.Count != 2 {
		t.Errorf("SkippedDuplicate = %d, want 2", resp.SkippedDuplicate.Count)
	}
}

func TestImport_SkipsNameAlreadyInDatabase(t *testing.T) {
	svc, repo := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "taken-name", "10.0.0.1"),
		goodRow(3, "SRV-002", "free-name", "10.0.0.2"),
	})
	repo.existingNames = []string{"taken-name"}

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Succeeded.Count != 1 || resp.Succeeded.Items[0] != "SRV-002" {
		t.Errorf("Succeeded = %#v, want just SRV-002", resp.Succeeded)
	}
	if resp.SkippedDuplicate.Count != 1 || resp.SkippedDuplicate.Items[0] != "SRV-001" {
		t.Errorf("SkippedDuplicate = %#v, want SRV-001", resp.SkippedDuplicate)
	}
}

// An id absent from RETURNING was rejected by ON CONFLICT (server_id).
func TestImport_SkipsIDAlreadyInDatabase(t *testing.T) {
	svc, repo := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "10.0.0.1"),
		goodRow(3, "SRV-002", "web-02", "10.0.0.2"),
	})
	repo.takenIDs["SRV-001"] = true

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Succeeded.Count != 1 || resp.Succeeded.Items[0] != "SRV-002" {
		t.Errorf("Succeeded = %#v, want just SRV-002", resp.Succeeded)
	}
	if resp.SkippedDuplicate.Count != 1 || resp.SkippedDuplicate.Items[0] != "SRV-001" {
		t.Errorf("SkippedDuplicate = %#v, want SRV-001", resp.SkippedDuplicate)
	}
}

// The race the whole fallback exists for: a name is taken between the
// pre-check and the insert, aborting the batch. Every other row must survive.
func TestImport_NameRaceFallsBackPerRowAndKeepsGoodRows(t *testing.T) {
	rows := []excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "10.0.0.1"),
		goodRow(3, "SRV-002", "raced-name", "10.0.0.2"),
		goodRow(4, "SRV-003", "web-03", "10.0.0.3"),
	}
	svc, repo := newTestImportService(rows)
	repo.nameClash["raced-name"] = true // clashes only at insert time

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Succeeded.Count != 2 {
		t.Fatalf("Succeeded = %#v, want SRV-001 and SRV-003", resp.Succeeded)
	}
	if resp.SkippedDuplicate.Count != 1 || resp.SkippedDuplicate.Items[0] != "SRV-002" {
		t.Errorf("SkippedDuplicate = %#v, want SRV-002", resp.SkippedDuplicate)
	}
	if resp.Failed.Count != 0 {
		t.Errorf("Failed = %#v, want none", resp.Failed)
	}
	if len(repo.perRow) != 3 {
		t.Errorf("per-row retried %d rows, want all 3", len(repo.perRow))
	}
}

// A non-clash database error must not trigger the per-row retry.
func TestImport_BatchErrorReportsRowsFailed(t *testing.T) {
	svc, repo := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "10.0.0.1"),
	})
	repo.batchErr = errors.New("connection reset")

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Failed.Count != 1 || resp.Failed.Items[0].Reason != "DATABASE_ERROR" {
		t.Fatalf("Failed = %#v, want one DATABASE_ERROR", resp.Failed)
	}
	if len(repo.perRow) != 0 {
		t.Error("expected no per-row retry for a non-clash error")
	}
}

func TestImport_BatchesAt500(t *testing.T) {
	var rows []excel.ParsedRow
	for i := range 1001 {
		rows = append(rows, goodRow(i+2, fmt.Sprintf("SRV-%05d", i), fmt.Sprintf("web-%05d", i), "10.0.0.1"))
	}
	svc, repo := newTestImportService(rows)

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.Succeeded.Count != 1001 {
		t.Fatalf("Succeeded = %d, want 1001", resp.Succeeded.Count)
	}
	if len(repo.batches) != 3 {
		t.Fatalf("got %d batches, want 3", len(repo.batches))
	}
	if len(repo.batches[0]) != 500 || len(repo.batches[1]) != 500 || len(repo.batches[2]) != 1 {
		t.Errorf("batch sizes = %d/%d/%d, want 500/500/1",
			len(repo.batches[0]), len(repo.batches[1]), len(repo.batches[2]))
	}
}

func TestImport_SyncsTargetProjectionForInsertedRows(t *testing.T) {
	svc, _ := newTestImportService([]excel.ParsedRow{
		goodRow(2, "SRV-001", "web-01", "10.0.0.1"),
	})
	targets := newFakeTargetProjection()
	svc.(*importServiceImpl).targets = targets

	if _, err := svc.Import(context.Background(), strings.NewReader("")); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if got := targets.synced["SRV-001"]; got != "10.0.0.1:8080" {
		t.Errorf("target = %q, want 10.0.0.1:8080", got)
	}
	if got := targets.names["SRV-001"]; got != "web-01" {
		t.Errorf("projected server_name = %q, want web-01", got)
	}
}

func TestImport_EmptyFileReturnsEmptyGroups(t *testing.T) {
	svc, _ := newTestImportService(nil)

	resp, err := svc.Import(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if resp.TotalRows != 0 || resp.Succeeded.Count != 0 {
		t.Errorf("unexpected response %#v", resp)
	}
	// Serialising nil would emit null, not [].
	if resp.Succeeded.Items == nil || resp.Failed.Items == nil || resp.SkippedDuplicate.Items == nil {
		t.Error("expected empty slices rather than nil")
	}
}
