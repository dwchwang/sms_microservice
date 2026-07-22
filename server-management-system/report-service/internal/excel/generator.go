package excel

import (
	"bytes"
	"fmt"

	"github.com/vcs-sms/report-service/internal/repository"
	"github.com/xuri/excelize/v2"
)

// ContentType is the MIME type of the generated attachment.
const ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

var headers = []string{"server_id", "server_name", "uptime_pct", "total_checks"}

// Generator writes the per-server uptime attachment.
type Generator interface {
	Generate(rows []repository.ServerUptimeRow) (*bytes.Buffer, error)
}

type generator struct{}

// NewGenerator creates a Generator.
func NewGenerator() Generator {
	return &generator{}
}

// escapeFormula neutralises cells a spreadsheet would evaluate as a formula.
func escapeFormula(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

// Generate writes server_id, server_name and uptime_pct into an xlsx buffer.
// A no-data server (nil uptime) leaves the uptime cell blank.
func (g *generator) Generate(rows []repository.ServerUptimeRow) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Uptime"
	f.SetSheetName(f.GetSheetName(0), sheet)

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"4472C4"}, Pattern: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create header style: %w", err)
	}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellStr(sheet, cell, h); err != nil {
			return nil, fmt.Errorf("failed to write header: %w", err)
		}
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	for i, row := range rows {
		r := i + 2
		idCell, _ := excelize.CoordinatesToCellName(1, r)
		nameCell, _ := excelize.CoordinatesToCellName(2, r)
		uptimeCell, _ := excelize.CoordinatesToCellName(3, r)
		checksCell, _ := excelize.CoordinatesToCellName(4, r)

		if err := f.SetCellStr(sheet, idCell, escapeFormula(row.ServerID)); err != nil {
			return nil, fmt.Errorf("failed to write cell %s: %w", idCell, err)
		}
		if err := f.SetCellStr(sheet, nameCell, escapeFormula(row.ServerName)); err != nil {
			return nil, fmt.Errorf("failed to write cell %s: %w", nameCell, err)
		}
		if row.UptimePct != nil {
			if err := f.SetCellFloat(sheet, uptimeCell, *row.UptimePct, 2, 64); err != nil {
				return nil, fmt.Errorf("failed to write cell %s: %w", uptimeCell, err)
			}
		}
		if err := f.SetCellInt(sheet, checksCell, row.TotalChecks); err != nil {
			return nil, fmt.Errorf("failed to write cell %s: %w", checksCell, err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write xlsx: %w", err)
	}
	return &buf, nil
}

// Filename builds the attachment name for a report window.
func Filename(startDate, endDate string) string {
	if startDate == endDate {
		return fmt.Sprintf("uptime_%s.xlsx", endDate)
	}
	return fmt.Sprintf("uptime_%s_%s.xlsx", startDate, endDate)
}
