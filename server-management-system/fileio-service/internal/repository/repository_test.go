package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	dialector := postgres.New(postgres.Config{
		Conn: mockDB,
	})

	db, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("failed to open gorm DB: %v", err)
	}

	return db, mock
}

func TestImportJobRepo_Create(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()
	now := time.Now().UTC()
	job := &model.ImportJob{
		ID:        jobID,
		Status:    "pending",
		FileName:  "test.xlsx",
		FilePath:  "/uploads/test.xlsx",
		CreatedAt: now,
		UpdatedAt: now,
	}

	mock.ExpectQuery(`INSERT INTO "fileio_schema"."import_jobs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(jobID))

	err := repo.Create(context.Background(), job)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestImportJobRepo_FindByID(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()
	now := time.Now().UTC()

	rows := sqlmock.NewRows([]string{
		"id", "status", "file_name", "file_path", "total_rows",
		"success_count", "failed_count", "created_at", "updated_at",
	}).
		AddRow(jobID, "completed", "test.xlsx", "/uploads/test.xlsx",
			10, 8, 2, now, now)

	mock.ExpectQuery(`SELECT \* FROM "fileio_schema"."import_jobs"`).
		WithArgs(jobID, 1).
		WillReturnRows(rows)

	job, err := repo.FindByID(context.Background(), jobID.String())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if job.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", job.Status)
	}
	if job.TotalRows != 10 {
		t.Errorf("expected 10 total rows, got %d", job.TotalRows)
	}
}

func TestImportJobRepo_FindByID_NotFound(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()

	mock.ExpectQuery(`SELECT \* FROM "fileio_schema"."import_jobs"`).
		WithArgs(jobID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindByID(context.Background(), jobID.String())
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestImportJobRepo_FindByID_InvalidUUID(t *testing.T) {
	db, _ := setupMockDB(t)
	repo := NewImportJobRepo(db)

	_, err := repo.FindByID(context.Background(), "not-a-uuid")
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
}

func TestImportJobRepo_UpdateStatus(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()

	mock.ExpectExec(`UPDATE "fileio_schema"."import_jobs" SET`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.UpdateStatus(context.Background(), jobID.String(), "processing")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestImportJobRepo_UpdateStatus_InvalidUUID(t *testing.T) {
	db, _ := setupMockDB(t)
	repo := NewImportJobRepo(db)

	if err := repo.UpdateStatus(context.Background(), "bad-id", "processing"); err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

func TestImportJobRepo_UpdateCompleted(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()

	mock.ExpectExec(`UPDATE "fileio_schema"."import_jobs" SET`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.UpdateCompleted(context.Background(), jobID.String(), 10, 8, 2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImportJobRepo_UpdateCompleted_InvalidUUID(t *testing.T) {
	db, _ := setupMockDB(t)
	repo := NewImportJobRepo(db)

	if err := repo.UpdateCompleted(context.Background(), "bad-id", 1, 1, 0); err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

func TestImportJobRepo_UpdateFailed(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()

	mock.ExpectExec(`UPDATE "fileio_schema"."import_jobs" SET`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.UpdateFailed(context.Background(), jobID.String(), "parse error")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImportJobRepo_UpdateFailed_InvalidUUID(t *testing.T) {
	db, _ := setupMockDB(t)
	repo := NewImportJobRepo(db)

	if err := repo.UpdateFailed(context.Background(), "bad-id", "failed"); err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

func TestImportJobRepo_SaveDetail(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()
	detail := &model.ImportJobDetail{
		ImportJobID: jobID,
		RowNumber:   2,
		ServerID:    "SRV-001",
		ServerName:  "web-01",
		Status:      "success",
	}

	mock.ExpectQuery(`INSERT INTO "fileio_schema"."import_job_details"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	err := repo.SaveDetail(context.Background(), detail)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImportJobRepo_CreateServerWithDetail(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()
	now := time.Now().UTC()
	server := &model.Server{
		ServerID:   "SRV-NEW",
		ServerName: "new-server",
		Status:     "off",
		IPv4:       "10.0.1.100",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	detail := &model.ImportJobDetail{
		ImportJobID: jobID,
		RowNumber:   2,
		ServerID:    "SRV-NEW",
		ServerName:  "new-server",
		Status:      "success",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "server_schema"."servers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("new-uuid"))
	mock.ExpectQuery(`INSERT INTO "fileio_schema"."import_job_details"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err := repo.CreateServerWithDetail(context.Background(), server, detail)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestImportJobRepo_CreateServerWithDetail_RollsBackOnDetailError(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()
	now := time.Now().UTC()
	server := &model.Server{
		ServerID:   "SRV-NEW",
		ServerName: "new-server",
		Status:     "off",
		IPv4:       "10.0.1.100",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	detail := &model.ImportJobDetail{
		ImportJobID: jobID,
		RowNumber:   2,
		ServerID:    "SRV-NEW",
		ServerName:  "new-server",
		Status:      "success",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "server_schema"."servers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("new-uuid"))
	mock.ExpectQuery(`INSERT INTO "fileio_schema"."import_job_details"`).
		WillReturnError(gorm.ErrInvalidData)
	mock.ExpectRollback()

	if err := repo.CreateServerWithDetail(context.Background(), server, detail); err == nil {
		t.Fatal("expected detail insert error")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestImportJobRepo_SaveDetailsBatch_Empty(t *testing.T) {
	db, _ := setupMockDB(t)
	repo := NewImportJobRepo(db)

	err := repo.SaveDetailsBatch(context.Background(), nil)
	if err != nil {
		t.Errorf("expected no error for empty batch, got %v", err)
	}

	err = repo.SaveDetailsBatch(context.Background(), []model.ImportJobDetail{})
	if err != nil {
		t.Errorf("expected no error for empty batch, got %v", err)
	}
}

func TestImportJobRepo_SaveDetailsBatch(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()
	details := []model.ImportJobDetail{
		{ImportJobID: jobID, RowNumber: 2, ServerID: "SRV-001", ServerName: "web-01", Status: "success"},
		{ImportJobID: jobID, RowNumber: 3, ServerID: "SRV-002", ServerName: "db-01", Status: "success"},
	}

	mock.ExpectQuery(`INSERT INTO "fileio_schema"."import_job_details"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()).AddRow(uuid.New()))

	err := repo.SaveDetailsBatch(context.Background(), details)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImportJobRepo_GetDetailsByJobID(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewImportJobRepo(db)

	jobID := uuid.New()
	now := time.Now().UTC()

	rows := sqlmock.NewRows([]string{
		"id", "import_job_id", "row_number", "server_id",
		"server_name", "status", "error_reason", "created_at",
	}).
		AddRow(uuid.New(), jobID, 2, "SRV-001", "web-01", "success", "", now).
		AddRow(uuid.New(), jobID, 3, "SRV-002", "db-01", "failed", "duplicate server_id", now)

	mock.ExpectQuery(`SELECT \* FROM "fileio_schema"."import_job_details"`).
		WithArgs(jobID).
		WillReturnRows(rows)

	details, err := repo.GetDetailsByJobID(context.Background(), jobID.String())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(details) != 2 {
		t.Errorf("expected 2 details, got %d", len(details))
	}
	if details[0].Status != "success" {
		t.Errorf("expected first detail success, got %q", details[0].Status)
	}
	if details[1].Status != "failed" {
		t.Errorf("expected second detail failed, got %q", details[1].Status)
	}
}

func TestImportJobRepo_GetDetailsByJobID_InvalidUUID(t *testing.T) {
	db, _ := setupMockDB(t)
	repo := NewImportJobRepo(db)

	if _, err := repo.GetDetailsByJobID(context.Background(), "bad-id"); err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

// ===================== ServerWriter Tests =====================

func TestServerWriter_FindByServerIDOrName_Found(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewServerWriter(db)

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"id", "server_id", "server_name", "status", "ipv4", "os",
		"created_at", "updated_at",
	}).AddRow("uuid-1", "SRV-001", "web-01", "on", "10.0.1.1", "Ubuntu", now, now)

	mock.ExpectQuery(`SELECT \* FROM "server_schema"."servers"`).
		WithArgs("SRV-001", "web-01", 1).
		WillReturnRows(rows)

	server, err := repo.FindByServerIDOrName(context.Background(), "SRV-001", "web-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if server.ServerID != "SRV-001" {
		t.Errorf("expected SRV-001, got %s", server.ServerID)
	}
}

func TestServerWriter_FindByServerIDOrName_NotFound(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewServerWriter(db)

	mock.ExpectQuery(`SELECT \* FROM "server_schema"."servers"`).
		WithArgs("SRV-999", "nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindByServerIDOrName(context.Background(), "SRV-999", "nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestServerWriter_Create(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewServerWriter(db)

	server := &model.Server{
		ServerID:   "SRV-NEW",
		ServerName: "new-server",
		Status:     "off",
		IPv4:       "10.0.1.100",
		OS:         "Ubuntu 22.04",
		CPUCores:   intPtr(8),
		RAMGB:      floatPtr(16),
		DiskGB:     floatPtr(500),
		Location:   "DC-HN",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	mock.ExpectQuery(`INSERT INTO "server_schema"."servers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("new-uuid"))

	err := repo.Create(context.Background(), server)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestServerWriter_FindAllWithFilter_NoFilter(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewServerWriter(db)

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"id", "server_id", "server_name", "status", "ipv4", "os",
		"created_at", "updated_at",
	}).AddRow("uuid-1", "SRV-001", "web-01", "on", "10.0.1.1", "Ubuntu", now, now)

	mock.ExpectQuery(`SELECT \* FROM "server_schema"."servers"`).
		WillReturnRows(rows)

	servers, err := repo.FindAllWithFilter(context.Background(), &dto.ExportFilter{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(servers))
	}
}

func TestServerWriter_FindAllWithFilter_WithFilters(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewServerWriter(db)

	mock.ExpectQuery(`SELECT \* FROM "server_schema"."servers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "server_id", "server_name", "status", "ipv4", "os", "created_at", "updated_at"}))

	filter := &dto.ExportFilter{
		Status:    "on",
		SortBy:    "server_name",
		SortOrder: "asc",
	}
	_, err := repo.FindAllWithFilter(context.Background(), filter)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestServerWriter_FindAllWithFilter_AllFiltersAndDefaultSort(t *testing.T) {
	db, mock := setupMockDB(t)
	repo := NewServerWriter(db)

	mock.ExpectQuery(`SELECT \* FROM "server_schema"."servers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "server_id", "server_name", "status", "ipv4", "os", "created_at", "updated_at"}))

	filter := &dto.ExportFilter{
		Status:     "off",
		ServerName: "web",
		IPv4:       "10.0.",
		Location:   "HN",
		OS:         "Ubuntu",
		SortBy:     "not_allowed",
		SortOrder:  "desc",
	}
	_, err := repo.FindAllWithFilter(context.Background(), filter)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---- helpers ----

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
