package service

import (
	"bytes"
	"context"
	"fmt"

	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/excel"
	"github.com/vcs-sms/server-service/internal/repository"
	"github.com/vcs-sms/server-service/internal/status"
)

// Matches the page size cap FindAll enforces; a larger value is silently clamped.
const exportPageSize = 100

// ExportService renders the server catalog as an Excel file.
type ExportService interface {
	Export(ctx context.Context, filter *dto.ServerFilter) (*bytes.Buffer, string, error)
}

type exportServiceImpl struct {
	repo      repository.ServerRepository
	generator excel.Generator
	lastCheck status.LastCheckReader
}

// NewExportService creates an ExportService.
func NewExportService(
	repo repository.ServerRepository,
	generator excel.Generator,
	lastCheck status.LastCheckReader,
) ExportService {
	return &exportServiceImpl{repo: repo, generator: generator, lastCheck: lastCheck}
}

// Export reuses the list filter so export and list can never disagree, then
// pages through the matching servers.
func (s *exportServiceImpl) Export(ctx context.Context, filter *dto.ServerFilter) (*bytes.Buffer, string, error) {
	var all []dto.ServerResponse

	page := 1
	for {
		spec := *filter
		spec.Page = page
		spec.PageSize = exportPageSize

		servers, total, err := s.repo.FindAll(ctx, &spec)
		if err != nil {
			return nil, "", fmt.Errorf("failed to query servers: %w", err)
		}
		for i := range servers {
			all = append(all, *mapServerToResponse(&servers[i]))
		}
		if len(servers) == 0 || int64(len(all)) >= total {
			break
		}
		page++
	}

	s.enrichLastCheck(ctx, all)

	buf, err := s.generator.Generate(all)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate xlsx: %w", err)
	}
	return buf, excel.Filename(), nil
}

// enrichLastCheck fills last_status_check in one pipelined pass.
func (s *exportServiceImpl) enrichLastCheck(ctx context.Context, servers []dto.ServerResponse) {
	if s.lastCheck == nil || len(servers) == 0 {
		return
	}
	ids := make([]string, len(servers))
	for i := range servers {
		ids[i] = servers[i].ServerID
	}
	checked := s.lastCheck.LastCheckedAt(ctx, ids)
	for i := range servers {
		if ts, ok := checked[servers[i].ServerID]; ok {
			servers[i].LastStatusCheck = &ts
		}
	}
}
