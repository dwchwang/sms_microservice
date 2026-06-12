package excel

import (
	"bytes"
	"fmt"
	"time"

	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/xuri/excelize/v2"
)

// ExcelGenerator defines the interface for generating Excel export files.
type ExcelGenerator interface {
	Generate(servers []dto.ServerExportRow) (*bytes.Buffer, error)
}

type excelGenerator struct{}

// NewExcelGenerator creates a new ExcelGenerator instance.
func NewExcelGenerator() ExcelGenerator {
	return &excelGenerator{}
}

// Generate creates an Excel file from server data and returns it as a buffer.
func (g *excelGenerator) Generate(servers []dto.ServerExportRow) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	sheet := "Servers"

	// Rename default sheet
	f.SetSheetName("Sheet1", sheet)

	// Define headers
	headers := []string{
		"Server ID", "Server Name", "Status", "IPv4", "OS",
		"CPU Cores", "RAM (GB)", "Disk (GB)", "Location", "Description",
		"Created At", "Updated At",
	}

	// Header style — bold white text on blue background
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:  true,
			Size:  11,
			Color: "FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"4472C4"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: "000000", Style: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create header style: %w", err)
	}

	// Write headers
	for colIdx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheet, cell, header)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	// Write data rows
	for rowIdx, srv := range servers {
		row := rowIdx + 2 // data starts at row 2
		setCell(f, sheet, 1, row, srv.ServerID)
		setCell(f, sheet, 2, row, srv.ServerName)
		setCell(f, sheet, 3, row, srv.Status)
		setCell(f, sheet, 4, row, srv.IPv4)
		setCell(f, sheet, 5, row, srv.OS)

		if srv.CPUCores != nil {
			f.SetCellValue(sheet, cellName(6, row), *srv.CPUCores)
		}
		if srv.RAMGB != nil {
			f.SetCellValue(sheet, cellName(7, row), *srv.RAMGB)
		}
		if srv.DiskGB != nil {
			f.SetCellValue(sheet, cellName(8, row), *srv.DiskGB)
		}

		setCell(f, sheet, 9, row, srv.Location)
		setCell(f, sheet, 10, row, srv.Description)
		setCell(f, sheet, 11, row, srv.CreatedAt)
		setCell(f, sheet, 12, row, srv.UpdatedAt)
	}

	// Auto-fit columns (set sensible defaults)
	colWidths := []float64{16, 22, 10, 16, 18, 12, 12, 12, 14, 30, 22, 22}
	for colIdx, width := range colWidths {
		colName, _ := excelize.ColumnNumberToName(colIdx + 1)
		f.SetColWidth(sheet, colName, colName, width)
	}

	// Set row height for header
	f.SetRowHeight(sheet, 1, 24)

	// Write to buffer
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write excel to buffer: %w", err)
	}

	return &buf, nil
}

// GenerateFilename creates a timestamped filename for export.
func GenerateFilename() string {
	return fmt.Sprintf("servers_export_%s.xlsx", time.Now().Format("20060102_150405"))
}

// ---- helpers ----

func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

func setCell(f *excelize.File, sheet string, col, row int, value string) {
	cell, _ := excelize.CoordinatesToCellName(col, row)
	f.SetCellValue(sheet, cell, value)
}
