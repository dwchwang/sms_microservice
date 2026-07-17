package excel

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// MaxDataRows caps an import so it can finish inside one HTTP request.
const MaxDataRows = 10000

// Column order of the import template.
var expectedHeaders = []string{
	"server_id", "server_name", "ipv4", "tcp_port", "os",
	"cpu_cores", "ram_gb", "disk_gb", "location", "description",
}

// ParsedRow is one data row of the import file.
type ParsedRow struct {
	RowNumber   int
	ServerID    string
	ServerName  string
	IPv4        string
	TCPPort     int
	OS          string
	CPUCores    int
	RAMGB       int
	DiskGB      int
	Location    string
	Description string
	IsValid     bool
	ErrorCode   string
}

// Parser reads import files.
type Parser interface {
	Parse(r io.Reader) ([]ParsedRow, error)
}

type parser struct{}

// NewParser creates a Parser.
func NewParser() Parser {
	return &parser{}
}

// Parse streams the sheet row by row rather than loading it all into memory.
// A returned error is a file-level rejection; bad rows come back marked invalid.
func (p *parser) Parse(r io.Reader) ([]ParsedRow, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("cannot read xlsx: %w", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	if sheet == "" {
		return nil, fmt.Errorf("file has no sheet")
	}

	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, fmt.Errorf("cannot iterate sheet %q: %w", sheet, err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("file is empty")
	}
	header, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("cannot read header row: %w", err)
	}
	if err := validateHeaders(header); err != nil {
		return nil, err
	}

	var parsed []ParsedRow
	rowNumber := 1
	for rows.Next() {
		rowNumber++
		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("cannot read row %d: %w", rowNumber, err)
		}
		if isRowEmpty(cols) {
			continue
		}
		if len(parsed) == MaxDataRows {
			return nil, fmt.Errorf("file exceeds %d data rows", MaxDataRows)
		}
		parsed = append(parsed, parseRow(rowNumber, cols))
	}
	if err := rows.Error(); err != nil {
		return nil, fmt.Errorf("cannot read sheet: %w", err)
	}

	return parsed, nil
}

func parseRow(rowNumber int, cols []string) ParsedRow {
	row := ParsedRow{RowNumber: rowNumber, IsValid: true}

	row.ServerID = cell(cols, 0)
	row.ServerName = cell(cols, 1)
	row.IPv4 = cell(cols, 2)
	rawPort := cell(cols, 3)
	row.OS = cell(cols, 4)
	row.CPUCores = parseInt(cell(cols, 5))
	row.RAMGB = parseInt(cell(cols, 6))
	row.DiskGB = parseInt(cell(cols, 7))
	row.Location = cell(cols, 8)
	row.Description = cell(cols, 9)

	switch {
	case row.ServerID == "":
		return invalid(row, "MISSING_SERVER_ID")
	case row.ServerName == "":
		return invalid(row, "MISSING_SERVER_NAME")
	case row.IPv4 == "":
		return invalid(row, "MISSING_IPV4")
	case !isValidIPv4(row.IPv4):
		return invalid(row, "INVALID_IPV4")
	case rawPort == "":
		return invalid(row, "MISSING_TCP_PORT")
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return invalid(row, "INVALID_TCP_PORT")
	}
	row.TCPPort = port

	return row
}

func invalid(row ParsedRow, code string) ParsedRow {
	row.IsValid = false
	row.ErrorCode = code
	return row
}

func validateHeaders(headers []string) error {
	if len(headers) < len(expectedHeaders) {
		return fmt.Errorf("expected %d columns, got %d", len(expectedHeaders), len(headers))
	}
	for i, want := range expectedHeaders {
		got := strings.ToLower(strings.TrimSpace(headers[i]))
		if got != want {
			return fmt.Errorf("column %d: expected %q, got %q", i+1, want, got)
		}
	}
	return nil
}

func cell(cols []string, i int) string {
	if i >= len(cols) {
		return ""
	}
	return strings.TrimSpace(cols[i])
}

func isRowEmpty(cols []string) bool {
	for _, c := range cols {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func isValidIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil
}

func parseInt(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}
