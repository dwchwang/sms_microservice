package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/vcs-sms/auth-service/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupAuthTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func TestUserRepository_Create(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	userID := uuid.New()
	roleID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "users"`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))

	user := &model.User{
		ID: userID, Email: "test@test.com",
		PasswordHash: "hash", FullName: "Test", RoleID: roleID, IsActive: true,
	}
	err := repo.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestUserRepository_FindByEmail(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "full_name", "role_id", "is_active", "created_at", "updated_at"}).
		AddRow(uuid.New(), "test@test.com", "hash", "Test", uuid.New(), true, nil, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE email = $1 AND "users"."deleted_at" IS NULL ORDER BY "users"."id" LIMIT $2`)).
		WithArgs("testuser", 1).
		WillReturnRows(rows)

	user, err := repo.FindByEmail(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("FindByEmail failed: %v", err)
	}
	if user.Email != "test@test.com" {
		t.Errorf("expected 'test@test.com', got '%s'", user.Email)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestUserRepository_FindByEmail_NotFound(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users"`)).
		WithArgs("nonexistent", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindByEmail(context.Background(), "nonexistent")
	if err != gorm.ErrRecordNotFound {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}



func TestUserRepository_FindByID(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	id := uuid.New()
	rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "full_name", "role_id", "is_active", "created_at", "updated_at"}).
		AddRow(id, "test@test.com", "hash", "Test", uuid.New(), true, nil, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE id = $1 AND "users"."deleted_at" IS NULL ORDER BY "users"."id" LIMIT $2`)).
		WithArgs(id, 1).
		WillReturnRows(rows)

	user, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if user.ID != id {
		t.Errorf("expected ID match")
	}
}

func TestUserRepository_FindByID_NotFound(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	id := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users"`)).
		WithArgs(id, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindByID(context.Background(), id)
	if err != gorm.ErrRecordNotFound {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestUserRepository_FindByIDWithRole(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	userID := uuid.New()
	roleID := uuid.New()
	permID := uuid.New()

	userRows := sqlmock.NewRows([]string{"id", "email", "password_hash", "full_name", "role_id", "is_active", "created_at", "updated_at"}).
		AddRow(userID, "test@test.com", "hash", "Test", roleID, true, nil, nil)
	roleRows := sqlmock.NewRows([]string{"id", "name", "description", "created_at", "updated_at"}).
		AddRow(roleID, "admin", "Admin role", nil, nil)
	permRows := sqlmock.NewRows([]string{"id", "role_id", "scope", "created_at"}).
		AddRow(permID, roleID, "server:read", nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE id = $1 AND "users"."deleted_at" IS NULL ORDER BY "users"."id" LIMIT $2`)).
		WithArgs(userID, 1).
		WillReturnRows(userRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "roles" WHERE "roles"."id" = $1`)).
		WithArgs(roleID).
		WillReturnRows(roleRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "role_permissions" WHERE "role_permissions"."role_id" = $1`)).
		WithArgs(roleID).
		WillReturnRows(permRows)

	user, err := repo.FindByIDWithRole(context.Background(), userID)
	if err != nil {
		t.Fatalf("FindByIDWithRole failed: %v", err)
	}
	if user.Role == nil || user.Role.Name != "admin" {
		t.Fatalf("expected admin role, got %#v", user.Role)
	}
	if len(user.Role.Permissions) != 1 || user.Role.Permissions[0].Scope != "server:read" {
		t.Fatalf("expected server:read permission, got %#v", user.Role.Permissions)
	}
}

func TestUserRepository_FindByIDWithRole_NotFound(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	id := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users"`)).
		WithArgs(id, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindByIDWithRole(context.Background(), id)
	if err != gorm.ErrRecordNotFound {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestUserRepository_FindByIDFull(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	userID := uuid.New()
	roleID := uuid.New()
	permID := uuid.New()

	userRows := sqlmock.NewRows([]string{"id", "email", "password_hash", "full_name", "role_id", "is_active", "created_at", "updated_at"}).
		AddRow(userID, "full@test.com", "hash", "Full User", roleID, true, nil, nil)
	roleRows := sqlmock.NewRows([]string{"id", "name", "description", "created_at", "updated_at"}).
		AddRow(roleID, "admin", "Admin role", nil, nil)
	permRows := sqlmock.NewRows([]string{"id", "role_id", "scope", "created_at"}).
		AddRow(permID, roleID, "user:manage", nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE id = $1 AND "users"."deleted_at" IS NULL ORDER BY "users"."id" LIMIT $2`)).
		WithArgs(userID, 1).
		WillReturnRows(userRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "roles" WHERE "roles"."id" = $1`)).
		WithArgs(roleID).
		WillReturnRows(roleRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "role_permissions" WHERE "role_permissions"."role_id" = $1`)).
		WithArgs(roleID).
		WillReturnRows(permRows)

	user, err := repo.FindByIDFull(context.Background(), userID)
	if err != nil {
		t.Fatalf("FindByIDFull failed: %v", err)
	}
	if user.Role == nil || user.Role.Name != "admin" {
		t.Fatalf("expected admin role, got %#v", user.Role)
	}
	if len(user.Role.Permissions) != 1 || user.Role.Permissions[0].Scope != "user:manage" {
		t.Fatalf("expected user:manage permission, got %#v", user.Role.Permissions)
	}
}

func TestUserRepository_FindAllUsers(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	userID := uuid.New()
	roleID := uuid.New()
	permID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "users" WHERE "users"."deleted_at" IS NULL`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	userRows := sqlmock.NewRows([]string{"id", "email", "password_hash", "full_name", "role_id", "is_active", "created_at", "updated_at"}).
		AddRow(userID, "list@test.com", "hash", "List User", roleID, true, nil, nil)
	roleRows := sqlmock.NewRows([]string{"id", "name", "description", "created_at", "updated_at"}).
		AddRow(roleID, "viewer", "Viewer role", nil, nil)
	permRows := sqlmock.NewRows([]string{"id", "role_id", "scope", "created_at"}).
		AddRow(permID, roleID, "server:read", nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE "users"."deleted_at" IS NULL ORDER BY created_at DESC LIMIT $1`)).
		WithArgs(20).
		WillReturnRows(userRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "roles" WHERE "roles"."id" = $1`)).
		WithArgs(roleID).
		WillReturnRows(roleRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "role_permissions" WHERE "role_permissions"."role_id" = $1`)).
		WithArgs(roleID).
		WillReturnRows(permRows)

	users, total, err := repo.FindAllUsers(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("FindAllUsers failed: %v", err)
	}
	if total != 1 || len(users) != 1 {
		t.Fatalf("expected one user and total 1, got total=%d len=%d", total, len(users))
	}
	if users[0].Role == nil || users[0].Role.Name != "viewer" {
		t.Fatalf("expected viewer role, got %#v", users[0].Role)
	}
	if len(users[0].Role.Permissions) != 1 || users[0].Role.Permissions[0].Scope != "server:read" {
		t.Fatalf("expected server:read permission, got %#v", users[0].Role.Permissions)
	}
}

func TestUserRepository_UpdateLastLogin(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	id := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "users" SET "last_login_at"=$1,"updated_at"=$2 WHERE id = $3 AND "users"."deleted_at" IS NULL`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateLastLogin(context.Background(), id)
	if err != nil {
		t.Fatalf("UpdateLastLogin failed: %v", err)
	}
}

func TestUserRepository_UpdateLastLogin_Error(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	id := uuid.New()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "users" SET "last_login_at"=$1,"updated_at"=$2 WHERE id = $3 AND "users"."deleted_at" IS NULL`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), id).
		WillReturnError(gorm.ErrInvalidDB)

	err := repo.UpdateLastLogin(context.Background(), id)
	if err == nil {
		t.Fatal("expected update error")
	}
}

func TestUserRepository_UpdateRole(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	userID := uuid.New()
	roleID := uuid.New()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "users" SET "role_id"=$1,"updated_at"=$2 WHERE id = $3 AND "users"."deleted_at" IS NULL`)).
		WithArgs(roleID, sqlmock.AnyArg(), userID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateRole(context.Background(), userID, roleID); err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}
}

func TestUserRepository_FindRoleByName(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	roleID := uuid.New()
	rows := sqlmock.NewRows([]string{"id", "name", "description", "created_at", "updated_at"}).
		AddRow(roleID, "admin", "Admin role", nil, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "roles" WHERE name = $1 ORDER BY "roles"."id" LIMIT $2`)).
		WithArgs("admin", 1).
		WillReturnRows(rows)

	// Permissions preload
	permRows := sqlmock.NewRows([]string{"id", "role_id", "scope", "created_at"}).
		AddRow(uuid.New(), roleID, "server:create", nil)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "role_permissions" WHERE "role_permissions"."role_id" = $1`)).
		WithArgs(roleID).
		WillReturnRows(permRows)

	role, err := repo.FindRoleByName(context.Background(), "admin")
	if err != nil {
		t.Fatalf("FindRoleByName failed: %v", err)
	}
	if role.Name != "admin" {
		t.Errorf("expected 'admin', got '%s'", role.Name)
	}
}

func TestUserRepository_FindRoleByName_NotFound(t *testing.T) {
	db, mock := setupAuthTestDB(t)
	repo := NewUserRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "roles"`)).
		WithArgs("missing", 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := repo.FindRoleByName(context.Background(), "missing")
	if err != gorm.ErrRecordNotFound {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}
