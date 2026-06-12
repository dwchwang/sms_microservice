package mocks

import (
	"context"

	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/model"
)

// ServerWriterMock is a mock implementation of repository.ServerWriter.
type ServerWriterMock struct {
	FindByServerIDOrNameFunc func(ctx context.Context, serverID, serverName string) (*model.Server, error)
	CreateFunc               func(ctx context.Context, server *model.Server) error
	FindAllWithFilterFunc    func(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error)
}

func (m *ServerWriterMock) FindByServerIDOrName(ctx context.Context, serverID, serverName string) (*model.Server, error) {
	if m.FindByServerIDOrNameFunc != nil {
		return m.FindByServerIDOrNameFunc(ctx, serverID, serverName)
	}
	return nil, nil
}

func (m *ServerWriterMock) Create(ctx context.Context, server *model.Server) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, server)
	}
	return nil
}

func (m *ServerWriterMock) FindAllWithFilter(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error) {
	if m.FindAllWithFilterFunc != nil {
		return m.FindAllWithFilterFunc(ctx, filter)
	}
	return nil, nil
}
