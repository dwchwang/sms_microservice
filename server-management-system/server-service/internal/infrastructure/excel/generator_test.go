package excel

import (
	"bytes"
	"testing"
	"time"

	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/xuri/excelize/v2"
)

func readBack(t *testing.T, buf *bytes.Buffer) [][]string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("generated file is not readable: %v", err)
	}
	defer f.Close()

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	return rows
}

func TestGenerate_WritesHeaderAndRow(t *testing.T) {
	checked := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	buf, err := NewGenerator().Generate([]dto.ServerResponse{{
		ServerID:        "SRV-001",
		ServerName:      "web-01",
		Status:          "ON",
		LastStatusCheck: &checked,
		IPv4:            "10.0.0.1",
		TCPPort:         8080,
		OS:              "Ubuntu",
		CPUCores:        4,
	}})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	rows := readBack(t, buf)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want header + 1", len(rows))
	}
	if rows[0][0] != "server_id" || rows[0][3] != "last_status_check" {
		t.Errorf("unexpected header %v", rows[0])
	}
	if rows[1][0] != "SRV-001" || rows[1][2] != "ON" {
		t.Errorf("unexpected data row %v", rows[1])
	}
	if rows[1][3] != "2026-07-16T10:00:00Z" {
		t.Errorf("last_status_check = %q", rows[1][3])
	}
	if rows[1][5] != "8080" {
		t.Errorf("tcp_port = %q, want 8080", rows[1][5])
	}
}

// A server Monitoring has never checked exports an empty cell, not "nil".
func TestGenerate_BlankLastStatusCheckWhenNil(t *testing.T) {
	buf, err := NewGenerator().Generate([]dto.ServerResponse{{
		ServerID: "SRV-001", ServerName: "web-01", Status: "UNKNOWN", IPv4: "10.0.0.1",
	}})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	rows := readBack(t, buf)
	if len(rows[1]) > 3 && rows[1][3] != "" {
		t.Errorf("last_status_check = %q, want empty", rows[1][3])
	}
}

func TestEscapeFormula(t *testing.T) {
	cases := map[string]string{
		"=1+1":               "'=1+1",
		"+1":                 "'+1",
		"-1":                 "'-1",
		"@SUM(A1)":           "'@SUM(A1)",
		`=cmd|' /c calc'!A0`: `'=cmd|' /c calc'!A0`,
		"web-01":             "web-01",
		"":                   "",
		"a=1":                "a=1",
		"10.0.0.1":           "10.0.0.1",
	}

	for in, want := range cases {
		if got := EscapeFormula(in); got != want {
			t.Errorf("EscapeFormula(%q) = %q, want %q", in, got, want)
		}
	}
}

// A malicious server_name must not survive as a live formula in the export.
func TestGenerate_EscapesFormulaInjection(t *testing.T) {
	buf, err := NewGenerator().Generate([]dto.ServerResponse{{
		ServerID:    "SRV-001",
		ServerName:  `=cmd|' /c calc'!A0`,
		Status:      "ON",
		IPv4:        "10.0.0.1",
		Description: "@SUM(1+1)",
		Location:    "-2+3",
	}})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	rows := readBack(t, buf)
	for _, col := range []int{1, 10, 11} {
		if got := rows[1][col]; len(got) > 0 && (got[0] == '=' || got[0] == '@' || got[0] == '-') {
			t.Errorf("column %d = %q is still a live formula", col, got)
		}
	}
}

func TestGenerate_EmptyExportStillHasHeader(t *testing.T) {
	buf, err := NewGenerator().Generate(nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	rows := readBack(t, buf)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want header only", len(rows))
	}
}

func TestFilename(t *testing.T) {
	name := Filename()
	if len(name) < len("servers_export_.xlsx") || name[:15] != "servers_export_" {
		t.Errorf("unexpected filename %q", name)
	}
}
