package excel

import (
	"bytes"
	"fmt"
	"time"

	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/xuri/excelize/v2"
)

var exportHeaders = []string{
	"server_id", "server_name", "status", "last_status_check", "ipv4", "tcp_port",
	"os", "cpu_cores", "ram_gb", "disk_gb", "location", "description",
	"created_at", "updated_at",
}

// Generator writes export files.
type Generator interface {
	Generate(servers []dto.ServerResponse) (*bytes.Buffer, error)
}

type generator struct{}

// NewGenerator creates a Generator.
func NewGenerator() Generator {
	return &generator{}
}

// EscapeFormula neutralises cells a spreadsheet would evaluate as a formula.
func EscapeFormula(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

// Generate writes the servers into an xlsx buffer.
func (g *generator) Generate(servers []dto.ServerResponse) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Servers"
	f.SetSheetName(f.GetSheetName(0), sheet)

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"4472C4"}, Pattern: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create header style: %w", err)
	}

	for i, h := range exportHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellStr(sheet, cell, h); err != nil {
			return nil, fmt.Errorf("failed to write header: %w", err)
		}
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	for i, srv := range servers {
		row := i + 2
		if err := g.writeRow(f, sheet, row, srv); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write xlsx: %w", err)
	}
	return &buf, nil
}

func (g *generator) writeRow(f *excelize.File, sheet string, row int, srv dto.ServerResponse) error {
	lastCheck := ""
	if srv.LastStatusCheck != nil {
		lastCheck = srv.LastStatusCheck.Format(time.RFC3339)
	}

	text := []string{
		srv.ServerID, srv.ServerName, srv.Status, lastCheck, srv.IPv4, "",
		srv.OS, "", "", "", srv.Location, srv.Description,
		srv.CreatedAt.Format(time.RFC3339), srv.UpdatedAt.Format(time.RFC3339),
	}
	numbers := map[int]int{6: srv.TCPPort, 8: srv.CPUCores, 9: srv.RAMGB, 10: srv.DiskGB}

	for col, val := range text {
		if _, isNumber := numbers[col+1]; isNumber {
			continue
		}
		cell, _ := excelize.CoordinatesToCellName(col+1, row)
		if err := f.SetCellStr(sheet, cell, EscapeFormula(val)); err != nil {
			return fmt.Errorf("failed to write cell %s: %w", cell, err)
		}
	}
	for col, val := range numbers {
		if val == 0 {
			continue
		}
		cell, _ := excelize.CoordinatesToCellName(col, row)
		if err := f.SetCellInt(sheet, cell, int64(val)); err != nil {
			return fmt.Errorf("failed to write cell %s: %w", cell, err)
		}
	}
	return nil
}

// Filename builds a timestamped export filename.
func Filename() string {
	return fmt.Sprintf("servers_export_%s.xlsx", time.Now().UTC().Format("20060102_150405"))
}
