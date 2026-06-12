package excel

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ParsedRow represents a single parsed row from the import Excel file.
type ParsedRow struct {
	RowNumber   int
	ServerID    string
	ServerName  string
	IPv4        string
	OS          string
	CPUCores    *int
	RAMGB       *float64
	DiskGB      *float64
	Location    string
	Description string
	IsValid     bool
	ErrorMsg    string
}

// ExcelParser defines the interface for parsing Excel import files.
type ExcelParser interface {
	Parse(filePath string) ([]ParsedRow, error)
	ValidateHeaders(filePath string) error
}

// Expected column headers (row 1) in order.
var expectedHeaders = []string{
	"server_id", "server_name", "ipv4", "os",
	"cpu_cores", "ram_gb", "disk_gb", "location", "description",
}

type excelParser struct{}

// NewExcelParser creates a new ExcelParser instance.
func NewExcelParser() ExcelParser {
	return &excelParser{}
}

// Parse reads an Excel file and returns parsed rows with validation.
func (p *excelParser) Parse(filePath string) ([]ParsedRow, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open excel file: %w", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("cannot read sheet %q: %w", sheet, err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("file must have at least a header row and one data row")
	}

	// Validate headers
	if err := p.validateHeaders(rows[0]); err != nil {
		return nil, err
	}

	// Parse data rows (skip header)
	var parsed []ParsedRow
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		pr := ParsedRow{
			RowNumber: i + 1, // 1-indexed for user display
			IsValid:   true,
		}

		// Skip completely empty rows
		if isRowEmpty(row) {
			continue
		}

		if len(row) > 0 {
			pr.ServerID = strings.TrimSpace(row[0])
		}
		if len(row) > 1 {
			pr.ServerName = strings.TrimSpace(row[1])
		}
		if len(row) > 2 {
			pr.IPv4 = strings.TrimSpace(row[2])
		}
		if len(row) > 3 {
			pr.OS = strings.TrimSpace(row[3])
		}
		if len(row) > 4 {
			pr.CPUCores = parseIntPtr(row[4])
		}
		if len(row) > 5 {
			pr.RAMGB = parseFloatPtr(row[5])
		}
		if len(row) > 6 {
			pr.DiskGB = parseFloatPtr(row[6])
		}
		if len(row) > 7 {
			pr.Location = strings.TrimSpace(row[7])
		}
		if len(row) > 8 {
			pr.Description = strings.TrimSpace(row[8])
		}

		// Validate required fields
		if pr.ServerID == "" {
			pr.IsValid = false
			pr.ErrorMsg = "server_id is required"
		} else if pr.ServerName == "" {
			pr.IsValid = false
			pr.ErrorMsg = "server_name is required"
		} else if pr.IPv4 == "" {
			pr.IsValid = false
			pr.ErrorMsg = "ipv4 is required"
		} else if !isValidIPv4(pr.IPv4) {
			pr.IsValid = false
			pr.ErrorMsg = fmt.Sprintf("invalid ipv4 format: %s", pr.IPv4)
		}

		parsed = append(parsed, pr)
	}

	return parsed, nil
}

// ValidateHeaders checks if the header row matches expected column names.
func (p *excelParser) ValidateHeaders(filePath string) error {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return fmt.Errorf("cannot open excel file: %w", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		return fmt.Errorf("cannot read sheet: %w", err)
	}

	if len(rows) == 0 {
		return fmt.Errorf("file is empty")
	}

	return p.validateHeaders(rows[0])
}

// validateHeaders checks raw header values against expected headers (case-insensitive).
func (p *excelParser) validateHeaders(headers []string) error {
	if len(headers) < len(expectedHeaders) {
		return fmt.Errorf("expected at least %d columns, got %d", len(expectedHeaders), len(headers))
	}

	for i, expected := range expectedHeaders {
		actual := strings.ToLower(strings.TrimSpace(headers[i]))
		if actual != expected {
			return fmt.Errorf("column %d: expected %q, got %q", i+1, expected, actual)
		}
	}

	return nil
}

// ---- helpers ----

func isRowEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func isValidIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil
}

func parseIntPtr(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}

func parseFloatPtr(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}
