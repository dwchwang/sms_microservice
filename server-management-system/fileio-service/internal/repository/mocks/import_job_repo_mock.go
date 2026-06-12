package mocks

import (
	"context"

	"github.com/vcs-sms/fileio-service/internal/model"
)

// ImportJobRepoMock is a mock implementation of repository.ImportJobRepo.
type ImportJobRepoMock struct {
	CreateFunc                 func(ctx context.Context, job *model.ImportJob) error
	FindByIDFunc               func(ctx context.Context, jobID string) (*model.ImportJob, error)
	UpdateStatusFunc           func(ctx context.Context, jobID string, status string) error
	UpdateCompletedFunc        func(ctx context.Context, jobID string, totalRows, successCount, failedCount int) error
	UpdateFailedFunc           func(ctx context.Context, jobID string, errMsg string) error
	SaveDetailFunc             func(ctx context.Context, detail *model.ImportJobDetail) error
	CreateServerWithDetailFunc func(ctx context.Context, server *model.Server, detail *model.ImportJobDetail) error
	SaveDetailsBatchFunc       func(ctx context.Context, details []model.ImportJobDetail) error
	GetDetailsByJobIDFunc      func(ctx context.Context, jobID string) ([]model.ImportJobDetail, error)
}

func (m *ImportJobRepoMock) Create(ctx context.Context, job *model.ImportJob) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, job)
	}
	return nil
}

func (m *ImportJobRepoMock) FindByID(ctx context.Context, jobID string) (*model.ImportJob, error) {
	if m.FindByIDFunc != nil {
		return m.FindByIDFunc(ctx, jobID)
	}
	return nil, nil
}

func (m *ImportJobRepoMock) UpdateStatus(ctx context.Context, jobID string, status string) error {
	if m.UpdateStatusFunc != nil {
		return m.UpdateStatusFunc(ctx, jobID, status)
	}
	return nil
}

func (m *ImportJobRepoMock) UpdateCompleted(ctx context.Context, jobID string, totalRows, successCount, failedCount int) error {
	if m.UpdateCompletedFunc != nil {
		return m.UpdateCompletedFunc(ctx, jobID, totalRows, successCount, failedCount)
	}
	return nil
}

func (m *ImportJobRepoMock) UpdateFailed(ctx context.Context, jobID string, errMsg string) error {
	if m.UpdateFailedFunc != nil {
		return m.UpdateFailedFunc(ctx, jobID, errMsg)
	}
	return nil
}

func (m *ImportJobRepoMock) SaveDetail(ctx context.Context, detail *model.ImportJobDetail) error {
	if m.SaveDetailFunc != nil {
		return m.SaveDetailFunc(ctx, detail)
	}
	return nil
}

func (m *ImportJobRepoMock) CreateServerWithDetail(ctx context.Context, server *model.Server, detail *model.ImportJobDetail) error {
	if m.CreateServerWithDetailFunc != nil {
		return m.CreateServerWithDetailFunc(ctx, server, detail)
	}
	return nil
}

func (m *ImportJobRepoMock) SaveDetailsBatch(ctx context.Context, details []model.ImportJobDetail) error {
	if m.SaveDetailsBatchFunc != nil {
		return m.SaveDetailsBatchFunc(ctx, details)
	}
	return nil
}

func (m *ImportJobRepoMock) GetDetailsByJobID(ctx context.Context, jobID string) ([]model.ImportJobDetail, error) {
	if m.GetDetailsByJobIDFunc != nil {
		return m.GetDetailsByJobIDFunc(ctx, jobID)
	}
	return nil, nil
}
