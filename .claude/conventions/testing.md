# 🧪 VCS-SMS Testing Conventions

> **Phiên bản:** 1.0 (Phase 1)
> **Coverage target:** ≥ 90% per package

---

## Công cụ

| Công cụ | Mục đích | Lệnh cài |
|---------|----------|----------|
| `go test -cover` | Built-in coverage | Có sẵn |
| `mockery` | Generate mock từ interface | `go install github.com/vektra/mockery/v2@latest` |
| `sqlmock` | Mock PostgreSQL cho repository test | `go get github.com/DATA-DOG/go-sqlmock` |
| `httptest` | Mock HTTP request/response | Có sẵn (stdlib) |

---

## Cấu trúc mock

```
<service>/internal/repository/
├── mocks/                          ← mockery auto-generate
│   ├── user_repository_mock.go
│   └── server_repository_mock.go
├── user_repository.go              ← interface + implementation
└── user_repository_test.go         ← sqlmock test
```

Config: `.mockery.yaml` ở thư mục gốc.

---

## Quy trình viết test

### 1. Repository — sqlmock

```go
func TestXxx(t *testing.T) {
    db, mock := setupTestDB(t)       // sqlmock DB
    repo := NewXxxRepository(db)

    mock.ExpectQuery("SELECT ...").
        WillReturnRows(sqlmock.NewRows([]string{"col"}).AddRow(val))

    result, err := repo.FindXxx(ctx, id)
    assert.NoError(t, err)
    assert.Equal(t, expected, result)
}
```

### 2. Service — mockery

```go
func TestXxx(t *testing.T) {
    mock := &mocks.UserRepositoryMock{
        FindByUsernameFunc: func(ctx context.Context, u string) (*model.User, error) {
            return &model.User{Username: "test"}, nil
        },
    }
    svc := NewAuthService(mock, nil, jwtCfg)
    resp, err := svc.Register(ctx, req)
}
```

### 3. Handler — httptest + mock service

```go
func TestXxxHandler(t *testing.T) {
    mock := &mockAuthService{registerResult: &dto.UserResponse{...}}
    handler := NewAuthHandler(mock, "secret")
    router := setupTestRouter(handler)

    req := httptest.NewRequest("POST", "/path", body)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    assert.Equal(t, 201, w.Code)
}
```

---

## Generate mock

```bash
# Tự động từ tất cả interface trong .mockery.yaml
make mocks

# Thủ công từng interface
mockery --name UserRepository --dir ./internal/repository --output ./internal/repository/mocks
```

## Run tests

```bash
make test          # Tất cả tests + coverage
make coverage      # HTML report
make deps          # go mod tidy all
```

## Coverage target

| Package | Target | Phương pháp |
|---------|:------:|-------------|
| `shared/pkg/jwt` | ≥ 90% | Unit test |
| `*/internal/repository` | ≥ 90% | sqlmock |
| `*/internal/service` | ≥ 90% | Mock repo |
| `*/internal/handler` | ≥ 80% | Mock service + httptest |
| `*/internal/middleware` | ≥ 80% | httptest |
