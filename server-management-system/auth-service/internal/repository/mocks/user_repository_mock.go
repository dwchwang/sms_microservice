package mocks

import (
	"context"

	"github.com/google/uuid"
	"github.com/vcs-sms/auth-service/internal/model"
)

// UserRepositoryMock is a test mock for UserRepository.
type UserRepositoryMock struct {
	CreateFunc           func(ctx context.Context, user *model.User) error
	FindByUsernameFunc   func(ctx context.Context, username string) (*model.User, error)
	FindByEmailFunc      func(ctx context.Context, email string) (*model.User, error)
	FindByIDFunc         func(ctx context.Context, id uuid.UUID) (*model.User, error)
	FindByIDWithRoleFunc func(ctx context.Context, id uuid.UUID) (*model.User, error)
	FindByIDFullFunc     func(ctx context.Context, id uuid.UUID) (*model.User, error)
	FindAllUsersFunc     func(ctx context.Context, page, pageSize int) ([]model.User, int64, error)
	UpdateLastLoginFunc  func(ctx context.Context, id uuid.UUID) error
	UpdateRoleFunc       func(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error
	FindRoleByNameFunc   func(ctx context.Context, name string) (*model.Role, error)
}

func (m *UserRepositoryMock) Create(ctx context.Context, user *model.User) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, user)
	}
	return nil
}

func (m *UserRepositoryMock) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	if m.FindByUsernameFunc != nil {
		return m.FindByUsernameFunc(ctx, username)
	}
	return nil, nil
}

func (m *UserRepositoryMock) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	if m.FindByEmailFunc != nil {
		return m.FindByEmailFunc(ctx, email)
	}
	return nil, nil
}

func (m *UserRepositoryMock) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	if m.FindByIDFunc != nil {
		return m.FindByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *UserRepositoryMock) FindByIDWithRole(ctx context.Context, id uuid.UUID) (*model.User, error) {
	if m.FindByIDWithRoleFunc != nil {
		return m.FindByIDWithRoleFunc(ctx, id)
	}
	return nil, nil
}

func (m *UserRepositoryMock) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	if m.UpdateLastLoginFunc != nil {
		return m.UpdateLastLoginFunc(ctx, id)
	}
	return nil
}

func (m *UserRepositoryMock) FindRoleByName(ctx context.Context, name string) (*model.Role, error) {
	if m.FindRoleByNameFunc != nil {
		return m.FindRoleByNameFunc(ctx, name)
	}
	return nil, nil
}

func (m *UserRepositoryMock) FindByIDFull(ctx context.Context, id uuid.UUID) (*model.User, error) {
	if m.FindByIDFullFunc != nil {
		return m.FindByIDFullFunc(ctx, id)
	}
	return nil, nil
}

func (m *UserRepositoryMock) FindAllUsers(ctx context.Context, page, pageSize int) ([]model.User, int64, error) {
	if m.FindAllUsersFunc != nil {
		return m.FindAllUsersFunc(ctx, page, pageSize)
	}
	return nil, 0, nil
}

func (m *UserRepositoryMock) UpdateRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
	if m.UpdateRoleFunc != nil {
		return m.UpdateRoleFunc(ctx, userID, roleID)
	}
	return nil
}
