package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The claim depends on ON CONFLICT ... WHERE and make_interval, neither of which
// sqlmock can evaluate. Same setup as the snapshot integration test.
func setupCronDB(t *testing.T) (CronRunRepository, *gorm.DB) {
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
	db.Exec(`DROP TABLE IF EXISTS cron_runs`)
	if err := db.Exec(`CREATE TABLE cron_runs (
		job_name VARCHAR(50) NOT NULL, run_date DATE NOT NULL,
		state VARCHAR(20) NOT NULL CHECK (state IN ('running','done','failed')),
		owner VARCHAR(255) NOT NULL,
		started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		finished_at TIMESTAMPTZ, error_message TEXT,
		PRIMARY KEY (job_name, run_date))`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS cron_runs`) })

	return NewCronRunRepository(db), db
}

const (
	testJob  = "snapshot"
	testDate = "2026-07-23"
	stale    = 3 * time.Minute
)

func TestTryClaim_OnlyOneWinnerPerDate(t *testing.T) {
	repo, _ := setupCronDB(t)
	ctx := context.Background()

	won, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if !won {
		t.Fatal("the first claim was refused")
	}

	won, err = repo.TryClaim(ctx, testJob, testDate, "node-b", stale)
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if won {
		t.Error("a second instance won a claim already held")
	}
}

func TestTryClaim_DoneIsNeverReclaimed(t *testing.T) {
	repo, _ := setupCronDB(t)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if err := repo.MarkDone(ctx, testJob, testDate, "node-a"); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}

	// Even with no staleness protection at all, done must stay done.
	won, err := repo.TryClaim(ctx, testJob, testDate, "node-b", 0)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if won {
		t.Error("a finished job was claimed again")
	}
}

func TestTryClaim_FailedIsRetried(t *testing.T) {
	repo, _ := setupCronDB(t)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if err := repo.MarkFailed(ctx, testJob, testDate, "node-a", "boom"); err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	won, err := repo.TryClaim(ctx, testJob, testDate, "node-b", stale)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if !won {
		t.Error("a failed job was not retried")
	}
}

// The HA case: the owner stopped heartbeating, so someone else takes over.
func TestTryClaim_StaleRunIsStolen(t *testing.T) {
	repo, db := setupCronDB(t)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	db.Exec(`UPDATE cron_runs SET heartbeat_at = NOW() - INTERVAL '10 minutes'`)

	won, err := repo.TryClaim(ctx, testJob, testDate, "node-b", stale)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if !won {
		t.Fatal("a stale claim was not stolen")
	}

	var owner string
	db.Raw(`SELECT owner FROM cron_runs WHERE job_name = ?`, testJob).Scan(&owner)
	if owner != "node-b" {
		t.Errorf("owner = %q, want node-b", owner)
	}
}

func TestTryClaim_FreshHeartbeatIsNotStolen(t *testing.T) {
	repo, _ := setupCronDB(t)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	held, err := repo.Heartbeat(ctx, testJob, testDate, "node-a")
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	if !held {
		t.Fatal("the owner lost its own claim")
	}

	won, _ := repo.TryClaim(ctx, testJob, testDate, "node-b", stale)
	if won {
		t.Error("a job that is still heartbeating was stolen")
	}
}

// After being stolen, the old owner must learn it no longer owns the run.
func TestHeartbeat_ReportsALostClaim(t *testing.T) {
	repo, db := setupCronDB(t)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	db.Exec(`UPDATE cron_runs SET heartbeat_at = NOW() - INTERVAL '10 minutes'`)
	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-b", stale); err != nil {
		t.Fatalf("steal failed: %v", err)
	}

	held, err := repo.Heartbeat(ctx, testJob, testDate, "node-a")
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	if held {
		t.Error("the evicted owner still believes it holds the claim")
	}
}

// The loser finishing late must not overwrite the new owner's run.
func TestMarkDone_IsIgnoredForANonOwner(t *testing.T) {
	repo, db := setupCronDB(t)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	db.Exec(`UPDATE cron_runs SET heartbeat_at = NOW() - INTERVAL '10 minutes'`)
	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-b", stale); err != nil {
		t.Fatalf("steal failed: %v", err)
	}

	if err := repo.MarkDone(ctx, testJob, testDate, "node-a"); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}

	done, err := repo.IsDone(ctx, testJob, testDate)
	if err != nil {
		t.Fatalf("IsDone failed: %v", err)
	}
	if done {
		t.Error("a non-owner marked the run done")
	}
}

func TestIsDone_TracksState(t *testing.T) {
	repo, _ := setupCronDB(t)
	ctx := context.Background()

	done, err := repo.IsDone(ctx, testJob, testDate)
	if err != nil {
		t.Fatalf("IsDone failed: %v", err)
	}
	if done {
		t.Error("an unclaimed job reported done")
	}

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if done, _ = repo.IsDone(ctx, testJob, testDate); done {
		t.Error("a running job reported done")
	}

	if err := repo.MarkDone(ctx, testJob, testDate, "node-a"); err != nil {
		t.Fatalf("MarkDone failed: %v", err)
	}
	if done, _ = repo.IsDone(ctx, testJob, testDate); !done {
		t.Error("a finished job did not report done")
	}
}

// Different days are independent claims.
func TestTryClaim_IsPerDate(t *testing.T) {
	repo, _ := setupCronDB(t)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, testJob, testDate, "node-a", stale); err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	won, err := repo.TryClaim(ctx, testJob, "2026-07-24", "node-b", stale)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if !won {
		t.Error("a claim on one day blocked another day")
	}
}
