package excel

import (
	"testing"

	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/xuri/excelize/v2"
)

func TestGenerate_ValidData(t *testing.T) {
	servers := []dto.ServerExportRow{
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
			CreatedAt:   "2026-06-01T00:00:00Z",
			UpdatedAt:   "2026-06-11T00:00:00Z",
		},
	}

	gen := NewExcelGenerator()
	buf, err := gen.Generate(servers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}

	// Verify it's a valid xlsx by opening it
	f, err := excelize.OpenReader(buf)
	if err != nil {
		t.Fatalf("generated file is not valid xlsx: %v", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("cannot read generated sheet: %v", err)
	}

	// Header row + 1 data row
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (header + data), got %d", len(rows))
	}

	// Check header
	if rows[0][0] != "Server ID" {
		t.Errorf("expected header 'Server ID', got %q", rows[0][0])
	}

	// Check data
	if rows[1][0] != "SRV-001" {
		t.Errorf("expected data 'SRV-001', got %q", rows[1][0])
	}
	if rows[1][1] != "web-01" {
		t.Errorf("expected data 'web-01', got %q", rows[1][1])
	}
	if rows[1][2] != "on" {
		t.Errorf("expected data 'on', got %q", rows[1][2])
	}
}

func TestGenerate_EmptyList(t *testing.T) {
	gen := NewExcelGenerator()
	buf, err := gen.Generate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer (at least headers)")
	}

	// Verify it has headers only
	f, err := excelize.OpenReader(buf)
	if err != nil {
		t.Fatalf("generated file is not valid xlsx: %v", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("cannot read generated sheet: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row (header only), got %d", len(rows))
	}
}

func TestGenerate_NilOptionalFields(t *testing.T) {
	servers := []dto.ServerExportRow{
		{
			ServerID:    "SRV-001",
			ServerName:  "minimal-01",
			Status:      "off",
			IPv4:        "10.0.1.1",
			OS:          "",
			CPUCores:    nil,
			RAMGB:       nil,
			DiskGB:      nil,
			Location:    "",
			Description: "",
			CreatedAt:   "2026-06-01T00:00:00Z",
			UpdatedAt:   "2026-06-11T00:00:00Z",
		},
	}

	gen := NewExcelGenerator()
	buf, err := gen.Generate(servers)
	if err != nil {
		t.Fatalf("unexpected error with nil fields: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}
}

func TestGenerate_LargeDataset(t *testing.T) {
	servers := make([]dto.ServerExportRow, 1000)
	for i := range servers {
		servers[i] = dto.ServerExportRow{
			ServerID:    "SRV-" + string(rune('A'+i%26)),
			ServerName:  "server-name",
			Status:      "on",
			IPv4:        "10.0.0.1",
			OS:          "Ubuntu",
			CPUCores:    intPtr(4),
			RAMGB:       floatPtr(8),
			DiskGB:      floatPtr(250),
			Location:    "DC-HN",
			Description: "Test server",
			CreatedAt:   "2026-06-01T00:00:00Z",
			UpdatedAt:   "2026-06-11T00:00:00Z",
		}
	}

	gen := NewExcelGenerator()
	buf, err := gen.Generate(servers)
	if err != nil {
		t.Fatalf("unexpected error with large dataset: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}

	// Verify row count
	f, err := excelize.OpenReader(buf)
	if err != nil {
		t.Fatalf("generated file is not valid xlsx: %v", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("cannot read generated sheet: %v", err)
	}

	// Header + 1000 data rows
	if len(rows) != 1001 {
		t.Fatalf("expected 1001 rows, got %d", len(rows))
	}
}

func TestGenerate_VerifyHeaders(t *testing.T) {
	gen := NewExcelGenerator()
	buf, err := gen.Generate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f, err := excelize.OpenReader(buf)
	if err != nil {
		t.Fatalf("generated file is not valid xlsx: %v", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("cannot read generated sheet: %v", err)
	}

	expectedHeaders := []string{
		"Server ID", "Server Name", "Status", "IPv4", "OS",
		"CPU Cores", "RAM (GB)", "Disk (GB)", "Location", "Description",
		"Created At", "Updated At",
	}

	for i, expected := range expectedHeaders {
		if rows[0][i] != expected {
			t.Errorf("header column %d: expected %q, got %q", i, expected, rows[0][i])
		}
	}
}

func TestGenerateFilename(t *testing.T) {
	filename := GenerateFilename()
	if filename == "" {
		t.Error("expected non-empty filename")
	}
	if len(filename) < 20 {
		t.Error("filename too short")
	}
}

// ---- helpers ----

func intPtr(v int) *int {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}
