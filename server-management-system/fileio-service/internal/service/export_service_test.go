package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/excel"
	"github.com/vcs-sms/fileio-service/internal/model"
	"github.com/vcs-sms/fileio-service/internal/repository/mocks"
)

func TestExportServers_NoFilter(t *testing.T) {
	now := time.Now().UTC()
	serverWriter := &mocks.ServerWriterMock{
		FindAllWithFilterFunc: func(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error) {
			return []model.Server{
				{
					ServerID:    "SRV-001",
					ServerName:  "web-01",
					Status:      "on",
					IPv4:        "10.0.1.1",
					OS:          "Ubuntu 22.04",
					CPUCores:    intPtr(8),
					RAMGB:       floatPtr(16),
					DiskGB:      floatPtr(500),
					Location:    "DC-HN",
					Description: "Web server",
					CreatedAt:   now,
					UpdatedAt:   now,
				},
			}, nil
		},
	}

	gen := excel.NewExcelGenerator()
	log := zerolog.New(nil)

	svc := NewExportService(serverWriter, gen, log)

	buf, filename, err := svc.ExportServers(context.Background(), &dto.ExportFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}
	if filename == "" {
		t.Error("expected non-empty filename")
	}
}

func TestExportServers_WithFilter(t *testing.T) {
	serverWriter := &mocks.ServerWriterMock{
		FindAllWithFilterFunc: func(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error) {
			// Verify filter is passed through
			if filter.Status != "on" {
				t.Errorf("expected filter status 'on', got %q", filter.Status)
			}
			if filter.SortBy != "server_name" {
				t.Errorf("expected sort_by 'server_name', got %q", filter.SortBy)
			}
			return []model.Server{}, nil
		},
	}

	gen := excel.NewExcelGenerator()
	log := zerolog.New(nil)

	svc := NewExportService(serverWriter, gen, log)

	filter := &dto.ExportFilter{
		Status:    "on",
		SortBy:    "server_name",
		SortOrder: "asc",
	}

	buf, _, err := svc.ExportServers(context.Background(), filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer (at least headers)")
	}
}

func TestExportServers_EmptyResult(t *testing.T) {
	serverWriter := &mocks.ServerWriterMock{
		FindAllWithFilterFunc: func(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error) {
			return nil, nil // empty result
		},
	}

	gen := excel.NewExcelGenerator()
	log := zerolog.New(nil)

	svc := NewExportService(serverWriter, gen, log)

	buf, _, err := svc.ExportServers(context.Background(), &dto.ExportFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer (headers only)")
	}
}

func TestExportServers_DBError(t *testing.T) {
	serverWriter := &mocks.ServerWriterMock{
		FindAllWithFilterFunc: func(ctx context.Context, filter *dto.ExportFilter) ([]model.Server, error) {
			return nil, fmt.Errorf("database connection error")
		},
	}

	gen := excel.NewExcelGenerator()
	log := zerolog.New(nil)

	svc := NewExportService(serverWriter, gen, log)

	_, _, err := svc.ExportServers(context.Background(), &dto.ExportFilter{})
	if err == nil {
		t.Fatal("expected error for DB failure")
	}
}

// ---- helpers ----

func intPtr(v int) *int {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}
