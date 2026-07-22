package excel

import (
	"bytes"
	"testing"

	"github.com/vcs-sms/report-service/internal/repository"
	"github.com/xuri/excelize/v2"
)

func ptr(v float64) *float64 { return &v }

func TestGenerate_ColumnsAndNoDataBlank(t *testing.T) {
	rows := []repository.ServerUptimeRow{
		{ServerID: "SRV-00001", ServerName: "web-01", TotalChecks: 1440, UptimePct: ptr(99.5)},
		{ServerID: "SRV-00002", ServerName: "no-data", TotalChecks: 0, UptimePct: nil},
	}

	buf, err := NewGenerator().Generate(rows)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("cannot reopen xlsx: %v", err)
	}
	defer f.Close()
	sheet := f.GetSheetName(0)

	// Header row.
	for col, want := range []string{"server_id", "server_name", "uptime_pct", "total_checks"} {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		if got, _ := f.GetCellValue(sheet, cell); got != want {
			t.Errorf("header %s = %q, want %q", cell, got, want)
		}
	}

	// Row 1: uptime present, checks written.
	if got, _ := f.GetCellValue(sheet, "A2"); got != "SRV-00001" {
		t.Errorf("A2 = %q, want SRV-00001", got)
	}
	if got, _ := f.GetCellValue(sheet, "C2"); got != "99.5" {
		t.Errorf("C2 = %q, want 99.5", got)
	}
	if got, _ := f.GetCellValue(sheet, "D2"); got != "1440" {
		t.Errorf("D2 = %q, want 1440", got)
	}

	// Row 2: no-data leaves uptime blank but still shows 0 checks.
	if got, _ := f.GetCellValue(sheet, "C3"); got != "" {
		t.Errorf("C3 = %q, want blank for no-data", got)
	}
	if got, _ := f.GetCellValue(sheet, "D3"); got != "0" {
		t.Errorf("D3 = %q, want 0 checks for no-data", got)
	}
}

// A server name that looks like a formula must not be evaluated by a spreadsheet.
func TestGenerate_EscapesFormulaInjection(t *testing.T) {
	rows := []repository.ServerUptimeRow{
		{ServerID: "SRV-00003", ServerName: "=1+2", UptimePct: ptr(50)},
	}

	buf, err := NewGenerator().Generate(rows)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	f, _ := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	defer f.Close()
	sheet := f.GetSheetName(0)

	got, _ := f.GetCellValue(sheet, "B2")
	if got != "'=1+2" && got != "=1+2" {
		// excelize may strip the leading quote on read; the point is it is not a formula.
		t.Errorf("B2 = %q, want the name stored as text, not a formula", got)
	}
	if formula, _ := f.GetCellFormula(sheet, "B2"); formula != "" {
		t.Errorf("B2 has a formula %q; injection was not neutralised", formula)
	}
}

func TestFilename(t *testing.T) {
	if got := Filename("2026-07-17", "2026-07-17"); got != "uptime_2026-07-17.xlsx" {
		t.Errorf("single day = %q", got)
	}
	if got := Filename("2026-07-10", "2026-07-16"); got != "uptime_2026-07-10_2026-07-16.xlsx" {
		t.Errorf("range = %q", got)
	}
}
