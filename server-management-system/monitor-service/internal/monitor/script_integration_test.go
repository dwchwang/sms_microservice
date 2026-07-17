package monitor

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/redis/go-redis/v9"
)

// The script is Redis-side logic; only a real Redis proves it. Run:
//
//	docker run -d --rm -p 56379:6379 redis:8-alpine
//	MONITOR_TEST_REDIS_ADDR=localhost:56379 go test ./internal/monitor/ -run Integration
const testRedisEnv = "MONITOR_TEST_REDIS_ADDR"

func setupRedis(t *testing.T) (RedisOps, *redis.Client) {
	t.Helper()
	addr := os.Getenv(testRedisEnv)
	if addr == "" {
		t.Skipf("%s is not set", testRedisEnv)
	}

	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("failed to reach redis: %v", err)
	}
	client.FlushDB(context.Background())
	t.Cleanup(func() {
		client.FlushDB(context.Background())
		client.Close()
	})
	return NewRedisOps(client), client
}

func target(id string) Target {
	return Target{ServerID: id, ServerName: "web-" + id, IPv4: "10.0.0.1", TCPPort: 80}
}

func counters(t *testing.T, c *redis.Client, id string) (total, ons int, pct float64) {
	t.Helper()
	ctx := context.Background()
	fields := c.HGetAll(ctx, statusKey(id)).Val()
	total, _ = strconv.Atoi(fields["total_checks"])
	ons, _ = strconv.Atoi(fields["on_checks"])
	pct = c.ZScore(ctx, uptimeIndexKey, id).Val()
	return
}

func TestIntegration_ScriptReturnCodes(t *testing.T) {
	ops, _ := setupRedis(t)
	ctx := context.Background()
	tg := target("SRV-1")

	// A first check is UNKNOWN -> ON, which is a real transition.
	if code, err := ops.ApplyStatus(ctx, tg, "ON", "2026-07-17T10:00:00Z", 5, 100); err != nil || code != statusChanged {
		t.Fatalf("first check: code = %d, err = %v; want %d", code, err, statusChanged)
	}
	// Same status, newer round: written, but no event.
	if code, _ := ops.ApplyStatus(ctx, tg, "ON", "2026-07-17T10:01:00Z", 5, 101); code != statusUnchanged {
		t.Errorf("unchanged: code = %d, want %d", code, statusUnchanged)
	}
	// Older round arriving late.
	if code, _ := ops.ApplyStatus(ctx, tg, "OFF", "2026-07-17T09:59:00Z", 5, 99); code != statusSkippedStale {
		t.Errorf("stale: code = %d, want %d", code, statusSkippedStale)
	}
	// Exact replay of the round already applied.
	if code, _ := ops.ApplyStatus(ctx, tg, "ON", "2026-07-17T10:01:00Z", 5, 101); code != statusSkippedStale {
		t.Errorf("replay: code = %d, want %d", code, statusSkippedStale)
	}
	// A real transition.
	if code, _ := ops.ApplyStatus(ctx, tg, "OFF", "2026-07-17T10:02:00Z", 3000, 102); code != statusChanged {
		t.Errorf("transition: code = %d, want %d", code, statusChanged)
	}
}

func TestIntegration_CountersTrackLifetimeUptime(t *testing.T) {
	ops, c := setupRedis(t)
	ctx := context.Background()
	tg := target("SRV-1")

	// 3 ON then 1 OFF -> 3/4 = 75%.
	for i, st := range []string{"ON", "ON", "ON", "OFF"} {
		if _, err := ops.ApplyStatus(ctx, tg, st, "2026-07-17T10:00:00Z", 5, int64(100+i)); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}

	total, ons, pct := counters(t, c, "SRV-1")
	if total != 4 || ons != 3 {
		t.Errorf("total/on = %d/%d, want 4/3", total, ons)
	}
	if pct != 75 {
		t.Errorf("uptime index score = %v, want 75", pct)
	}
}

// A stale or replayed check must not inflate the counters, the same reason the
// ES document id is deterministic.
func TestIntegration_CountersIgnoreStaleAndReplay(t *testing.T) {
	ops, c := setupRedis(t)
	ctx := context.Background()
	tg := target("SRV-1")

	ops.ApplyStatus(ctx, tg, "ON", "2026-07-17T10:00:00Z", 5, 100)
	ops.ApplyStatus(ctx, tg, "ON", "2026-07-17T10:00:00Z", 5, 100) // replay
	ops.ApplyStatus(ctx, tg, "OFF", "2026-07-17T09:00:00Z", 5, 50) // stale

	total, ons, pct := counters(t, c, "SRV-1")
	if total != 1 || ons != 1 {
		t.Errorf("total/on = %d/%d, want 1/1 — a replay was counted", total, ons)
	}
	if pct != 100 {
		t.Errorf("score = %v, want 100", pct)
	}
}

func TestIntegration_UptimeIndexServesDistributionAndWorst(t *testing.T) {
	ops, c := setupRedis(t)
	ctx := context.Background()

	// SRV-1 always up, SRV-2 half, SRV-3 always down.
	ops.ApplyStatus(ctx, target("SRV-1"), "ON", "2026-07-17T10:00:00Z", 5, 100)
	ops.ApplyStatus(ctx, target("SRV-1"), "ON", "2026-07-17T10:01:00Z", 5, 101)

	ops.ApplyStatus(ctx, target("SRV-2"), "ON", "2026-07-17T10:00:00Z", 5, 100)
	ops.ApplyStatus(ctx, target("SRV-2"), "OFF", "2026-07-17T10:01:00Z", 5, 101)

	ops.ApplyStatus(ctx, target("SRV-3"), "OFF", "2026-07-17T10:00:00Z", 5, 100)

	// The distribution the dashboard draws, straight out of ZCOUNT.
	if n := c.ZCount(ctx, uptimeIndexKey, "100", "100").Val(); n != 1 {
		t.Errorf("uptime_100 = %d, want 1", n)
	}
	if n := c.ZCount(ctx, uptimeIndexKey, "0", "0").Val(); n != 1 {
		t.Errorf("uptime_0 = %d, want 1", n)
	}
	if n := c.ZCount(ctx, uptimeIndexKey, "(0", "(100").Val(); n != 1 {
		t.Errorf("uptime_partial = %d, want 1", n)
	}

	worst := c.ZRangeWithScores(ctx, uptimeIndexKey, 0, 0).Val()
	if len(worst) != 1 || worst[0].Member != "SRV-3" || worst[0].Score != 0 {
		t.Errorf("worst = %+v, want SRV-3 at 0", worst)
	}
}
