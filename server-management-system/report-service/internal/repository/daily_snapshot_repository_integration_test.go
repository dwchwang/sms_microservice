package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vcs-sms/report-service/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqlmock only matches SQL text, so these need a real PostgreSQL.
// Run: docker run -d --rm -e POSTGRES_PASSWORD=test -e POSTGRES_DB=report_db \
//   -p 55433:5432 postgres:16-alpine, then set REPORT_TEST_DATABASE_URL.
const testDBEnv = "REPORT_TEST_DATABASE_URL"

func setupRealDB(t *testing.T) SnapshotRepository {
	t.Helper()
	dsn := os.Getenv(testDBEnv)
	if dsn == "" {
		t.Skipf("%s is not set", testDBEnv)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Mirrors init.sql.
	db.Exec(`DROP TABLE IF EXISTS daily_snapshots`)
	if err := db.Exec(`CREATE TABLE daily_snapshots (
		server_id VARCHAR(100) NOT NULL, date DATE NOT NULL, server_name VARCHAR(255) NOT NULL,
		on_checks INT NOT NULL DEFAULT 0, actual_checks INT NOT NULL DEFAULT 0,
		expected_checks INT NOT NULL DEFAULT 0, uptime_pct NUMERIC(5,2), last_status VARCHAR(10),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (server_id, date))`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS daily_snapshots`) })

	return NewSnapshotRepository(db)
}

func day(d int) time.Time {
	return time.Date(2026, 7, d, 0, 0, 0, 0, time.UTC)
}

func pct(v float64) *float64 { return &v }
func str(v string) *string   { return &v }

// SRV-1 100% | SRV-2 50% partial | SRV-3 no_data | SRV-4 0% (measured one day).
func seedThreeDays(t *testing.T, repo SnapshotRepository) {
	t.Helper()
	snaps := []model.DailySnapshot{
		{ServerID: "SRV-1", Date: day(10), ServerName: "web-1", OnChecks: 1440, ActualChecks: 1440, ExpectedChecks: 1440, UptimePct: pct(100), LastStatus: str("ON")},
		{ServerID: "SRV-1", Date: day(11), ServerName: "web-1", OnChecks: 1440, ActualChecks: 1440, ExpectedChecks: 1440, UptimePct: pct(100), LastStatus: str("ON")},
		{ServerID: "SRV-1", Date: day(12), ServerName: "web-1", OnChecks: 1440, ActualChecks: 1440, ExpectedChecks: 1440, UptimePct: pct(100), LastStatus: str("ON")},

		{ServerID: "SRV-2", Date: day(10), ServerName: "web-2", OnChecks: 720, ActualChecks: 1440, ExpectedChecks: 1440, UptimePct: pct(50), LastStatus: str("OFF")},
		{ServerID: "SRV-2", Date: day(11), ServerName: "web-2", OnChecks: 720, ActualChecks: 1440, ExpectedChecks: 1440, UptimePct: pct(50), LastStatus: str("OFF")},
		{ServerID: "SRV-2", Date: day(12), ServerName: "web-2", OnChecks: 720, ActualChecks: 1440, ExpectedChecks: 1440, UptimePct: pct(50), LastStatus: str("OFF")},

		{ServerID: "SRV-3", Date: day(10), ServerName: "web-3", ExpectedChecks: 1440},
		{ServerID: "SRV-3", Date: day(11), ServerName: "web-3", ExpectedChecks: 1440},
		{ServerID: "SRV-3", Date: day(12), ServerName: "web-3", ExpectedChecks: 1440},

		{ServerID: "SRV-4", Date: day(10), ServerName: "web-4", ExpectedChecks: 1440},
		{ServerID: "SRV-4", Date: day(11), ServerName: "web-4", OnChecks: 0, ActualChecks: 1440, ExpectedChecks: 1440, UptimePct: pct(0), LastStatus: str("OFF")},
		{ServerID: "SRV-4", Date: day(12), ServerName: "web-4", ExpectedChecks: 1440},
	}
	if err := repo.Upsert(context.Background(), snaps); err != nil {
		t.Fatalf("failed to seed: %v", err)
	}
}

// The §9.4 invariant must hold over a window of any width.
func TestIntegration_TotalsCountsServersNotServerDays(t *testing.T) {
	repo := setupRealDB(t)
	seedThreeDays(t, repo)

	got, err := repo.Totals(context.Background(), day(10), day(12))
	if err != nil {
		t.Fatalf("Totals failed: %v", err)
	}

	if got.TotalServers != 4 {
		t.Errorf("TotalServers = %d, want 4", got.TotalServers)
	}
	if got.ServersUptime100 != 1 {
		t.Errorf("ServersUptime100 = %d, want 1 (SRV-1 only, not its 3 days)", got.ServersUptime100)
	}
	if got.ServersUptime0 != 1 {
		t.Errorf("ServersUptime0 = %d, want 1 (SRV-4)", got.ServersUptime0)
	}
	if got.ServersNoData != 1 {
		t.Errorf("ServersNoData = %d, want 1 (SRV-3)", got.ServersNoData)
	}

	partial := got.TotalServers - got.ServersUptime100 - got.ServersUptime0 - got.ServersNoData
	if partial != 1 {
		t.Errorf("servers_uptime_partial = %d, want 1 (SRV-2); the §9.4 invariant is broken", partial)
	}

	if got.AvgUptimePct == nil {
		t.Fatal("AvgUptimePct is nil")
	}
	if *got.AvgUptimePct != 50 {
		t.Errorf("AvgUptimePct = %v, want 50", *got.AvgUptimePct)
	}

	if got.SumActualChecks != 10080 {
		t.Errorf("SumActualChecks = %d, want 10080", got.SumActualChecks)
	}
	if got.SumExpectedChecks != 17280 {
		t.Errorf("SumExpectedChecks = %d, want 17280", got.SumExpectedChecks)
	}
}

// The single-day case must stay correct.
func TestIntegration_TotalsSingleDayUnchanged(t *testing.T) {
	repo := setupRealDB(t)
	seedThreeDays(t, repo)

	got, err := repo.Totals(context.Background(), day(11), day(11))
	if err != nil {
		t.Fatalf("Totals failed: %v", err)
	}

	if got.TotalServers != 4 {
		t.Errorf("TotalServers = %d, want 4", got.TotalServers)
	}
	if got.ServersUptime100 != 1 || got.ServersUptime0 != 1 || got.ServersNoData != 1 {
		t.Errorf("got 100=%d 0=%d no_data=%d, want 1/1/1",
			got.ServersUptime100, got.ServersUptime0, got.ServersNoData)
	}
}

func TestIntegration_LowestUptimeRanksEachServerOnce(t *testing.T) {
	repo := setupRealDB(t)
	seedThreeDays(t, repo)

	got, err := repo.LowestUptime(context.Background(), day(10), day(12), 10)
	if err != nil {
		t.Fatalf("LowestUptime failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3 (one per server with data)", len(got))
	}

	seen := map[string]bool{}
	for _, row := range got {
		if seen[row.ServerID] {
			t.Errorf("%s appears more than once; its days were ranked separately", row.ServerID)
		}
		seen[row.ServerID] = true
	}

	if got[0].ServerID != "SRV-4" || got[0].UptimePct != 0 {
		t.Errorf("worst = %s at %v, want SRV-4 at 0", got[0].ServerID, got[0].UptimePct)
	}
	if got[0].ServerName != "web-4" {
		t.Errorf("ServerName = %q, want web-4", got[0].ServerName)
	}
	if got[2].ServerID != "SRV-1" {
		t.Errorf("best = %s, want SRV-1", got[2].ServerID)
	}
}
