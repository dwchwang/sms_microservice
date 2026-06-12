package service

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/excel"
	"github.com/vcs-sms/fileio-service/internal/repository"
)

// Sentinel errors
var (
	ErrExportFailed = fmt.Errorf("export failed")
)

// ExportService defines the interface for export business logic.
type ExportService interface {
	// ExportServers queries servers by filter, generates an Excel file, and returns the buffer.
	ExportServers(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error)
}

// exportServiceImpl implements ExportService.
type exportServiceImpl struct {
	serverWriter repository.ServerWriter
	generator    excel.ExcelGenerator
	log          zerolog.Logger
}

// NewExportService creates a new ExportService instance.
func NewExportService(
	serverWriter repository.ServerWriter,
	generator excel.ExcelGenerator,
	log zerolog.Logger,
) ExportService {
	return &exportServiceImpl{
		serverWriter: serverWriter,
		generator:    generator,
		log:          log,
	}
}

// ExportServers queries servers from server_schema, generates Excel, and returns the buffer.
func (s *exportServiceImpl) ExportServers(ctx context.Context, filter *dto.ExportFilter) (*bytes.Buffer, string, error) {
	// 1. Query servers with the given filter
	servers, err := s.serverWriter.FindAllWithFilter(ctx, filter)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}

	s.log.Info().Int("count", len(servers)).Msg("Exporting servers")

	// 2. Map to export rows
	exportRows := make([]dto.ServerExportRow, 0, len(servers))
	for _, srv := range servers {
		row := dto.ServerExportRow{
			ServerID:    srv.ServerID,
			ServerName:  srv.ServerName,
			Status:      srv.Status,
			IPv4:        srv.IPv4,
			OS:          srv.OS,
			CPUCores:    srv.CPUCores,
			RAMGB:       srv.RAMGB,
			DiskGB:      srv.DiskGB,
			Location:    srv.Location,
			Description: srv.Description,
			CreatedAt:   srv.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   srv.UpdatedAt.Format(time.RFC3339),
		}
		exportRows = append(exportRows, row)
	}

	// 3. Generate Excel file
	buf, err := s.generator.Generate(exportRows)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate excel: %w", err)
	}

	// 4. Generate filename
	filename := excel.GenerateFilename()

	return buf, filename, nil
}

// Ensure interface compliance.
var _ ExportService = (*exportServiceImpl)(nil)
