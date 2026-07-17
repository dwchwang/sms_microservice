package excel

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func buildXLSX(t *testing.T, rows [][]string) *bytes.Reader {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()

	sheet := f.GetSheetName(0)
	for r, row := range rows {
		for c, val := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("bad coordinates: %v", err)
			}
			if err := f.SetCellStr(sheet, cell, val); err != nil {
				t.Fatalf("SetCellStr: %v", err)
			}
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

func headerRow() []string {
	return append([]string(nil), expectedHeaders...)
}

func validRow() []string {
	return []string{"SRV-001", "web-01", "10.0.0.1", "8080", "Ubuntu", "4", "16", "500", "DC-HN", "primary"}
}

func TestParse_ValidRow(t *testing.T) {
	r := buildXLSX(t, [][]string{headerRow(), validRow()})

	rows, err := NewParser().Parse(r)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	got := rows[0]
	if !got.IsValid {
		t.Fatalf("row marked invalid: %s", got.ErrorCode)
	}
	if got.ServerID != "SRV-001" || got.ServerName != "web-01" || got.IPv4 != "10.0.0.1" {
		t.Errorf("unexpected row %#v", got)
	}
	if got.TCPPort != 8080 {
		t.Errorf("TCPPort = %d, want 8080", got.TCPPort)
	}
	if got.CPUCores != 4 || got.RAMGB != 16 || got.DiskGB != 500 {
		t.Errorf("numeric fields = %d/%d/%d, want 4/16/500", got.CPUCores, got.RAMGB, got.DiskGB)
	}
	if got.RowNumber != 2 {
		t.Errorf("RowNumber = %d, want 2", got.RowNumber)
	}
}

func TestParse_RejectsWrongTemplate(t *testing.T) {
	// The old template had no tcp_port column.
	legacy := []string{"server_id", "server_name", "ipv4", "os", "cpu_cores", "ram_gb", "disk_gb", "location", "description"}
	r := buildXLSX(t, [][]string{legacy, validRow()})

	if _, err := NewParser().Parse(r); err == nil {
		t.Fatal("expected the legacy template to be rejected")
	}
}

func TestParse_RejectsNonXLSX(t *testing.T) {
	if _, err := NewParser().Parse(strings.NewReader("this is not a spreadsheet")); err == nil {
		t.Fatal("expected a non-xlsx file to be rejected")
	}
}

func TestParse_RejectsEmptyFile(t *testing.T) {
	r := buildXLSX(t, [][]string{})

	if _, err := NewParser().Parse(r); err == nil {
		t.Fatal("expected an empty file to be rejected")
	}
}

// A header with no data rows is a valid file that imports nothing.
func TestParse_HeaderOnlyReturnsNoRows(t *testing.T) {
	r := buildXLSX(t, [][]string{headerRow()})

	rows, err := NewParser().Parse(r)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
}

func TestParse_MarksBadRowsInvalid(t *testing.T) {
	cases := map[string]struct {
		mutate   func(r []string)
		wantCode string
	}{
		"missing server_id":     {func(r []string) { r[0] = "" }, "MISSING_SERVER_ID"},
		"missing server_name":   {func(r []string) { r[1] = "" }, "MISSING_SERVER_NAME"},
		"missing ipv4":          {func(r []string) { r[2] = "" }, "MISSING_IPV4"},
		"malformed ipv4":        {func(r []string) { r[2] = "10.0.0" }, "INVALID_IPV4"},
		"ipv6 in ipv4 column":   {func(r []string) { r[2] = "2001:db8::1" }, "INVALID_IPV4"},
		"missing tcp_port":      {func(r []string) { r[3] = "" }, "MISSING_TCP_PORT"},
		"tcp_port zero":         {func(r []string) { r[3] = "0" }, "INVALID_TCP_PORT"},
		"tcp_port too high":     {func(r []string) { r[3] = "65536" }, "INVALID_TCP_PORT"},
		"tcp_port not a number": {func(r []string) { r[3] = "http" }, "INVALID_TCP_PORT"},
	}

	for name, tc := range cases {
		row := validRow()
		tc.mutate(row)
		r := buildXLSX(t, [][]string{headerRow(), row})

		rows, err := NewParser().Parse(r)
		if err != nil {
			t.Fatalf("%s: Parse failed: %v", name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: got %d rows, want 1", name, len(rows))
		}
		if rows[0].IsValid {
			t.Errorf("%s: expected the row to be invalid", name)
			continue
		}
		if rows[0].ErrorCode != tc.wantCode {
			t.Errorf("%s: ErrorCode = %q, want %q", name, rows[0].ErrorCode, tc.wantCode)
		}
	}
}

// One bad row must not stop the good ones — that is the whole point of partial success.
func TestParse_KeepsGoodRowsAlongsideBad(t *testing.T) {
	bad := validRow()
	bad[0] = "SRV-002"
	bad[2] = "not-an-ip"
	good := validRow()
	good[0] = "SRV-003"
	good[1] = "web-03"

	r := buildXLSX(t, [][]string{headerRow(), validRow(), bad, good})

	rows, err := NewParser().Parse(r)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if !rows[0].IsValid || rows[1].IsValid || !rows[2].IsValid {
		t.Errorf("validity = %v/%v/%v, want true/false/true",
			rows[0].IsValid, rows[1].IsValid, rows[2].IsValid)
	}
	if rows[1].RowNumber != 3 {
		t.Errorf("bad row RowNumber = %d, want 3", rows[1].RowNumber)
	}
}

func TestParse_SkipsBlankRows(t *testing.T) {
	blank := make([]string, 10)
	r := buildXLSX(t, [][]string{headerRow(), validRow(), blank, validRow()})

	rows, err := NewParser().Parse(r)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 blank-skipped", len(rows))
	}
}

func TestParse_OptionalFieldsMayBeBlank(t *testing.T) {
	row := validRow()
	for i := 4; i < 10; i++ {
		row[i] = ""
	}
	r := buildXLSX(t, [][]string{headerRow(), row})

	rows, err := NewParser().Parse(r)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if !rows[0].IsValid {
		t.Fatalf("expected row valid with blank optionals, got %s", rows[0].ErrorCode)
	}
	if rows[0].CPUCores != 0 || rows[0].OS != "" {
		t.Errorf("unexpected optional values %#v", rows[0])
	}
}

func TestParse_RejectsTooManyRows(t *testing.T) {
	rows := [][]string{headerRow()}
	for i := range MaxDataRows + 1 {
		row := validRow()
		row[0] = "SRV-" + strconv.Itoa(i)
		rows = append(rows, row)
	}
	r := buildXLSX(t, rows)

	if _, err := NewParser().Parse(r); err == nil {
		t.Fatalf("expected a file over %d rows to be rejected", MaxDataRows)
	}
}
