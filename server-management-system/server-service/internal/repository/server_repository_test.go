package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupServerTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	dialector := postgres.New(postgres.Config{Conn: mockDB, DriverName: "postgres"})
	db, err := gorm.Open(dialector, &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("failed to open gorm: %v", err)
	}
	return db, mock
}

func TestServerRepository_Create(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "servers"`)).
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	s := &model.Server{
		ServerID: "SRV-001", ServerName: "test", Status: "off",
		IPv4: "10.0.0.1",
	}
	err := repo.Create(context.Background(), s)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
}

func TestServerRepository_FindByServerID(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	rows := sqlmock.NewRows([]string{"id", "server_id", "server_name", "status", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description", "created_at", "updated_at", "deleted_at"}).
		AddRow(uuid.New(), "SRV-001", "test", "off", "10.0.0.1", "", nil, nil, nil, "", "", nil, nil, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "servers" WHERE server_id = $1 AND "servers"."deleted_at" IS NULL ORDER BY "servers"."id" LIMIT $2`)).
		WithArgs("SRV-001", 1).
		WillReturnRows(rows)

	s, err := repo.FindByServerID(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("FindByServerID failed: %v", err)
	}
	if s.ServerID != "SRV-001" {
		t.Errorf("expected 'SRV-001', got '%s'", s.ServerID)
	}
}

func TestServerRepository_FindByServerID_NotFound(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "servers"`)).
		WithArgs("NONEXIST", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindByServerID(context.Background(), "NONEXIST")
	if err != gorm.ErrRecordNotFound {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestServerRepository_FindAll(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	rows := sqlmock.NewRows([]string{"id", "server_id", "server_name", "status", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description", "created_at", "updated_at", "deleted_at"}).
		AddRow(uuid.New(), "SRV-001", "web", "off", "10.0.0.1", "", nil, nil, nil, "", "", nil, nil, nil).
		AddRow(uuid.New(), "SRV-002", "db", "off", "10.0.0.2", "", nil, nil, nil, "", "", nil, nil, nil)

	// Count query
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "servers" WHERE "servers"."deleted_at" IS NULL`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	// Data query (with order + limit/offset)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "servers" WHERE "servers"."deleted_at" IS NULL ORDER BY created_at DESC LIMIT $1`)).
		WithArgs(20).
		WillReturnRows(rows)

	servers, total, err := repo.FindAll(context.Background(), &dto.ServerFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2, got %d", total)
	}
	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(servers))
	}
}

func TestServerRepository_FindAll_FilterByIDNameAndIPv4(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	rows := sqlmock.NewRows([]string{"id", "server_id", "server_name", "status", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description", "created_at", "updated_at", "deleted_at"}).
		AddRow(uuid.New(), "SRV-WEB-001", "web-01", "on", "10.0.0.1", "", nil, nil, nil, "", "", nil, nil, nil)

	countQuery := `SELECT count(*) FROM "servers" WHERE server_id ILIKE $1 AND server_name ILIKE $2 AND ipv4 LIKE $3 AND "servers"."deleted_at" IS NULL`
	mock.ExpectQuery(regexp.QuoteMeta(countQuery)).
		WithArgs("%SRV-WEB%", "%web%", "10.0.%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	dataQuery := `SELECT * FROM "servers" WHERE server_id ILIKE $1 AND server_name ILIKE $2 AND ipv4 LIKE $3 AND "servers"."deleted_at" IS NULL ORDER BY server_id ASC LIMIT $4`
	mock.ExpectQuery(regexp.QuoteMeta(dataQuery)).
		WithArgs("%SRV-WEB%", "%web%", "10.0.%", 20).
		WillReturnRows(rows)

	servers, total, err := repo.FindAll(context.Background(), &dto.ServerFilter{
		ServerID:   "SRV-WEB",
		ServerName: "web",
		IPv4:       "10.0.",
		SortBy:     "server_id",
		SortOrder:  "asc",
		Page:       1,
		PageSize:   20,
	})
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1, got %d", total)
	}
	if len(servers) != 1 || servers[0].ServerID != "SRV-WEB-001" {
		t.Fatalf("expected SRV-WEB-001, got %#v", servers)
	}
}

func TestServerRepository_FindAll_AllFiltersAndPaginationBounds(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	rows := sqlmock.NewRows([]string{"id", "server_id", "server_name", "status", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description", "created_at", "updated_at", "deleted_at"}).
		AddRow(uuid.New(), "SRV-001", "web-01", "on", "10.0.0.1", "Ubuntu", nil, nil, nil, "HN", "", nil, nil, nil)

	countQuery := `SELECT count(*) FROM "servers" WHERE status = $1 AND server_id ILIKE $2 AND server_name ILIKE $3 AND ipv4 LIKE $4 AND os ILIKE $5 AND location ILIKE $6 AND "servers"."deleted_at" IS NULL`
	mock.ExpectQuery(regexp.QuoteMeta(countQuery)).
		WithArgs("on", "%SRV%", "%web%", "10.0.%", "%Ubuntu%", "%HN%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	dataQuery := `SELECT * FROM "servers" WHERE status = $1 AND server_id ILIKE $2 AND server_name ILIKE $3 AND ipv4 LIKE $4 AND os ILIKE $5 AND location ILIKE $6 AND "servers"."deleted_at" IS NULL ORDER BY updated_at DESC LIMIT $7`
	mock.ExpectQuery(regexp.QuoteMeta(dataQuery)).
		WithArgs("on", "%SRV%", "%web%", "10.0.%", "%Ubuntu%", "%HN%", 100).
		WillReturnRows(rows)

	servers, total, err := repo.FindAll(context.Background(), &dto.ServerFilter{
		Status:     "on",
		ServerID:   "SRV",
		ServerName: "web",
		IPv4:       "10.0.",
		OS:         "Ubuntu",
		Location:   "HN",
		SortBy:     "updated_at",
		SortOrder:  "desc",
		Page:       0,
		PageSize:   200,
	})
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if total != 1 || len(servers) != 1 {
		t.Fatalf("expected one server, got total=%d servers=%#v", total, servers)
	}
}

func TestServerRepository_FindAll_CountError(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "servers" WHERE "servers"."deleted_at" IS NULL`)).
		WillReturnError(gorm.ErrInvalidDB)

	_, _, err := repo.FindAll(context.Background(), &dto.ServerFilter{})
	if err == nil {
		t.Fatal("expected count error")
	}
}

func TestServerRepository_ExistsByServerID(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "servers" WHERE server_id = $1`)).
		WithArgs("SRV-001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	exists, err := repo.ExistsByServerID(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("ExistsByServerID failed: %v", err)
	}
	if !exists {
		t.Error("expected server to exist")
	}
}

func TestServerRepository_ExistsByServerName(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "servers" WHERE server_name = $1 AND "servers"."deleted_at" IS NULL`)).
		WithArgs("my-server").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	exists, err := repo.ExistsByServerName(context.Background(), "my-server")
	if err != nil {
		t.Fatalf("ExistsByServerName failed: %v", err)
	}
	if exists {
		t.Error("expected server to not exist")
	}
}

func TestServerRepository_ExistsByServerNameExclude(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "servers" WHERE (server_name = $1 AND server_id != $2) AND "servers"."deleted_at" IS NULL`)).
		WithArgs("my-server", "SRV-001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	exists, err := repo.ExistsByServerNameExclude(context.Background(), "my-server", "SRV-001")
	if err != nil {
		t.Fatalf("ExistsByServerNameExclude failed: %v", err)
	}
	if !exists {
		t.Error("expected another server with name to exist")
	}
}

func TestServerRepository_Delete(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "servers" SET "deleted_at"=$1 WHERE server_id = $2 AND "servers"."deleted_at" IS NULL`)).
		WithArgs(sqlmock.AnyArg(), "SRV-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Delete(context.Background(), "SRV-001")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestServerRepository_Update(t *testing.T) {
	db, mock := setupServerTestDB(t)
	repo := NewServerRepository(db)

	mock.ExpectExec(`UPDATE "servers" SET .* WHERE "servers"."deleted_at" IS NULL AND "id" = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	s := &model.Server{ID: uuid.New(), ServerID: "SRV-001", ServerName: "updated", IPv4: "10.0.0.1"}
	err := repo.Update(context.Background(), s)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
}
