package excel

import (
	"os"
	"testing"

	"github.com/xuri/excelize/v2"
)

// createTestXLSX creates an in-memory .xlsx file and returns its path.
func createTestXLSX(t *testing.T, headers []string, rows [][]string) string {
	t.Helper()

	f := excelize.NewFile()
	sheet := "Sheet1"

	// Write headers
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	// Write data rows
	for rowIdx, row := range rows {
		for colIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			f.SetCellValue(sheet, cell, val)
		}
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "test-import-*.xlsx")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	if err := f.Write(tmpFile); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to write xlsx: %v", err)
	}
	tmpFile.Close()

	return tmpFile.Name()
}

func TestParse_ValidFile(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	rows := [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu 22.04", "8", "16", "500", "DC-HN", "Web server"},
		{"SRV-002", "db-01", "10.0.1.2", "CentOS 9", "16", "64", "2000", "DC-HCM", "Database"},
	}

	filePath := createTestXLSX(t, headers, rows)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	parsed, err := parser.Parse(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(parsed))
	}

	// Check first row
	if parsed[0].ServerID != "SRV-001" {
		t.Errorf("expected server_id SRV-001, got %s", parsed[0].ServerID)
	}
	if parsed[0].ServerName != "web-01" {
		t.Errorf("expected server_name web-01, got %s", parsed[0].ServerName)
	}
	if parsed[0].IPv4 != "10.0.1.1" {
		t.Errorf("expected ipv4 10.0.1.1, got %s", parsed[0].IPv4)
	}
	if parsed[0].OS != "Ubuntu 22.04" {
		t.Errorf("expected OS Ubuntu 22.04, got %s", parsed[0].OS)
	}
	if parsed[0].CPUCores == nil || *parsed[0].CPUCores != 8 {
		t.Error("expected cpu_cores 8")
	}
	if parsed[0].RAMGB == nil || *parsed[0].RAMGB != 16 {
		t.Error("expected ram_gb 16")
	}
	if parsed[0].DiskGB == nil || *parsed[0].DiskGB != 500 {
		t.Error("expected disk_gb 500")
	}
	if parsed[0].Location != "DC-HN" {
		t.Errorf("expected location DC-HN, got %s", parsed[0].Location)
	}
	if !parsed[0].IsValid {
		t.Error("expected row to be valid")
	}
}

func TestParse_InvalidHeaders(t *testing.T) {
	// Wrong header names
	headers := []string{"id", "name", "ip", "os", "cpu", "ram", "disk", "loc", "desc"}
	rows := [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
	}

	filePath := createTestXLSX(t, headers, rows)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	_, err := parser.Parse(filePath)
	if err == nil {
		t.Fatal("expected error for invalid headers")
	}
}

func TestParse_EmptyFile(t *testing.T) {
	f := excelize.NewFile()

	tmpFile, err := os.CreateTemp("", "test-empty-*.xlsx")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if err := f.Write(tmpFile); err != nil {
		tmpFile.Close()
		t.Fatalf("failed to write xlsx: %v", err)
	}
	tmpFile.Close()

	parser := NewExcelParser()
	_, err = parser.Parse(tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestParse_HeaderOnly(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}

	filePath := createTestXLSX(t, headers, nil)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	_, err := parser.Parse(filePath)
	if err == nil {
		t.Fatal("expected error for header-only file")
	}
}

func TestParse_MissingRequiredFields(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	rows := [][]string{
		{"", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},    // missing server_id
		{"SRV-002", "", "10.0.1.2", "CentOS", "16", "64", "2000", "DC-HCM", "DB"}, // missing server_name
		{"SRV-003", "web-03", "", "Ubuntu", "4", "8", "250", "DC-HN", "Cache"},    // missing ipv4
	}

	filePath := createTestXLSX(t, headers, rows)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	parsed, err := parser.Parse(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(parsed))
	}

	// Row 1: missing server_id
	if parsed[0].IsValid {
		t.Error("expected row 1 to be invalid (missing server_id)")
	}
	if parsed[0].ErrorMsg != "server_id is required" {
		t.Errorf("expected 'server_id is required', got %q", parsed[0].ErrorMsg)
	}

	// Row 2: missing server_name
	if parsed[1].IsValid {
		t.Error("expected row 2 to be invalid (missing server_name)")
	}
	if parsed[1].ErrorMsg != "server_name is required" {
		t.Errorf("expected 'server_name is required', got %q", parsed[1].ErrorMsg)
	}

	// Row 3: missing ipv4
	if parsed[2].IsValid {
		t.Error("expected row 3 to be invalid (missing ipv4)")
	}
	if parsed[2].ErrorMsg != "ipv4 is required" {
		t.Errorf("expected 'ipv4 is required', got %q", parsed[2].ErrorMsg)
	}
}

func TestParse_InvalidIPv4(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	rows := [][]string{
		{"SRV-001", "web-01", "not-an-ip", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
		{"SRV-002", "db-01", "999.999.999.999", "CentOS", "16", "64", "2000", "DC-HCM", "DB"},
		{"SRV-003", "cache-01", "10.0.1.3", "Ubuntu", "4", "8", "250", "DC-HN", "Cache"}, // valid
	}

	filePath := createTestXLSX(t, headers, rows)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	parsed, err := parser.Parse(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(parsed))
	}

	if parsed[0].IsValid {
		t.Error("expected row 1 to be invalid (invalid ipv4)")
	}
	if parsed[1].IsValid {
		t.Error("expected row 2 to be invalid (invalid ipv4)")
	}
	if !parsed[2].IsValid {
		t.Error("expected row 3 to be valid")
	}
}

func TestParse_OptionalFieldsMissing(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	rows := [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "", "", "", "", "", ""},
	}

	filePath := createTestXLSX(t, headers, rows)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	parsed, err := parser.Parse(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !parsed[0].IsValid {
		t.Error("expected row to be valid even without optional fields")
	}
	if parsed[0].CPUCores != nil {
		t.Error("expected nil cpu_cores")
	}
	if parsed[0].RAMGB != nil {
		t.Error("expected nil ram_gb")
	}
	if parsed[0].DiskGB != nil {
		t.Error("expected nil disk_gb")
	}
}

func TestParse_FileNotFound(t *testing.T) {
	parser := NewExcelParser()
	_, err := parser.Parse("/nonexistent/path/file.xlsx")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParse_EmptyRowsSkipped(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	rows := [][]string{
		{"SRV-001", "web-01", "10.0.1.1", "Ubuntu", "8", "16", "500", "DC-HN", "Web"},
		{"", "", "", "", "", "", "", "", ""}, // empty row should be skipped
	}

	filePath := createTestXLSX(t, headers, rows)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	parsed, err := parser.Parse(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 row (empty skipped), got %d", len(parsed))
	}
	if parsed[0].ServerID != "SRV-001" {
		t.Errorf("expected SRV-001, got %s", parsed[0].ServerID)
	}
}

func TestValidateHeaders_Success(t *testing.T) {
	headers := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}

	filePath := createTestXLSX(t, headers, nil)
	defer os.Remove(filePath)

	parser := NewExcelParser()
	err := parser.ValidateHeaders(filePath)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
