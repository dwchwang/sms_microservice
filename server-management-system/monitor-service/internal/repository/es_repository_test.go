package repository

import (
	"testing"
	"time"
)

// A deterministic _id is what keeps a retried bulk from double-counting a check.
func TestDocID_IsDeterministic(t *testing.T) {
	first := DocID("SRV-001", 29166666)
	second := DocID("SRV-001", 29166666)

	if first != second {
		t.Fatalf("DocID is not stable: %q vs %q", first, second)
	}
	if first != "SRV-001:29166666" {
		t.Errorf("DocID = %q, want SRV-001:29166666", first)
	}
}

func TestDocID_DiffersPerServerAndRound(t *testing.T) {
	if DocID("SRV-001", 1) == DocID("SRV-002", 1) {
		t.Error("different servers share a doc id")
	}
	if DocID("SRV-001", 1) == DocID("SRV-001", 2) {
		t.Error("different rounds share a doc id")
	}
}

func TestIndexName_IsDailyAndUTC(t *testing.T) {
	// 23:30 in +07:00 is still the same UTC day; the index must follow UTC.
	ts := time.Date(2026, 7, 16, 23, 30, 0, 0, time.FixedZone("ICT", 7*3600))

	if got := IndexName("server-status-logs", ts); got != "server-status-logs-2026.07.16" {
		t.Errorf("IndexName = %q, want server-status-logs-2026.07.16", got)
	}
}

func TestIndexName_RollsOverAtUTCMidnight(t *testing.T) {
	before := time.Date(2026, 7, 16, 23, 59, 59, 0, time.UTC)
	after := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)

	if IndexName("p", before) == IndexName("p", after) {
		t.Error("index did not roll over at UTC midnight")
	}
}
