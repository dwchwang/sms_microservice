package status

import (
	"context"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const (
	uptimeIndexKey = "monitor:uptime:index"
	dayTotalItem   = "day_total"
	dayOnItem      = "day_on"
)

// ServerUptime is one server's uptime for the current day, as counted by
// Monitoring. TotalChecks/OnChecks are today's counts; the JSON keys are kept
// for the frontend contract.
type ServerUptime struct {
	ServerID    string  `json:"server_id"`
	ServerName  string  `json:"server_name"`
	UptimePct   float64 `json:"uptime_pct"`
	TotalChecks int64   `json:"total_checks"`
	OnChecks    int64   `json:"on_checks"`
}

// UptimeStats is the whole-fleet uptime picture.
type UptimeStats struct {
	Measured int64
	AvgPct   *float64
	Full     int64
	Partial  int64
	Zero     int64
	Worst    []ServerUptime
}

// UptimeReader reads the current-day uptime counters Monitoring keeps in Redis.
type UptimeReader interface {
	Stats(ctx context.Context, worstN int) (*UptimeStats, error)
}

type redisUptimeReader struct {
	client *redis.Client
}

// NewUptimeReader creates a Redis-backed UptimeReader.
func NewUptimeReader(rdb *redis.Client) UptimeReader {
	return &redisUptimeReader{client: rdb}
}

// Stats reads the sorted set Monitoring maintains. The distribution comes from
// ZCOUNT and the worst servers from ZRANGE, so neither scales with fleet size.
func (r *redisUptimeReader) Stats(ctx context.Context, worstN int) (*UptimeStats, error) {
	pipe := r.client.Pipeline()
	card := pipe.ZCard(ctx, uptimeIndexKey)
	full := pipe.ZCount(ctx, uptimeIndexKey, "100", "100")
	zero := pipe.ZCount(ctx, uptimeIndexKey, "0", "0")
	partial := pipe.ZCount(ctx, uptimeIndexKey, "(0", "(100")
	worst := pipe.ZRangeWithScores(ctx, uptimeIndexKey, 0, int64(worstN-1))
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	out := &UptimeStats{
		Measured: card.Val(),
		Full:     full.Val(),
		Partial:  partial.Val(),
		Zero:     zero.Val(),
	}

	if avg, err := r.average(ctx); err == nil {
		out.AvgPct = avg
	}

	rows := worst.Val()
	out.Worst = make([]ServerUptime, 0, len(rows))
	for _, z := range rows {
		id, ok := z.Member.(string)
		if !ok {
			continue
		}
		out.Worst = append(out.Worst, ServerUptime{ServerID: id, UptimePct: z.Score})
	}
	r.fillCounts(ctx, out.Worst)
	return out, nil
}

// average is the mean of the per-server percentages, which is what the report
// means by average uptime — not the fleet-wide on/total ratio.
func (r *redisUptimeReader) average(ctx context.Context) (*float64, error) {
	rows, err := r.client.ZRangeWithScores(ctx, uptimeIndexKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var sum float64
	for _, z := range rows {
		sum += z.Score
	}
	avg := sum / float64(len(rows))
	return &avg, nil
}

// fillCounts adds the raw counters behind each percentage.
func (r *redisUptimeReader) fillCounts(ctx context.Context, servers []ServerUptime) {
	if len(servers) == 0 {
		return
	}
	pipe := r.client.Pipeline()
	cmds := make([]*redis.SliceCmd, len(servers))
	for i, s := range servers {
		cmds[i] = pipe.HMGet(ctx, statusKeyPrefix+s.ServerID, dayTotalItem, dayOnItem)
	}
	_, _ = pipe.Exec(ctx)

	for i, cmd := range cmds {
		vals, err := cmd.Result()
		if err != nil || len(vals) != 2 {
			continue
		}
		servers[i].TotalChecks = toInt64(vals[0])
		servers[i].OnChecks = toInt64(vals[1])
	}
}

func toInt64(v any) int64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
