package repository

import (
	"context"
	"time"

	"github.com/vcs-sms/report-service/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const upsertBatch = 500

// Totals aggregates a report window across servers.
type Totals struct {
	TotalServers      int64
	AvgUptimePct      *float64
	ServersUptime100  int64
	ServersUptime0    int64
	ServersNoData     int64
	SumActualChecks   int64
	SumExpectedChecks int64
}

// LowUptimeServer is one row of the worst-uptime ranking.
type LowUptimeServer struct {
	ServerID   string  `gorm:"column:server_id" json:"server_id"`
	ServerName string  `gorm:"column:server_name" json:"server_name"`
	UptimePct  float64 `gorm:"column:uptime_pct" json:"uptime_pct"`
}

// ServerUptimeRow is one server's uptime over the window. UptimePct is nil for a
// server the window has a snapshot for but no measured checks (no-data);
// TotalChecks is that server's number of checks over the window.
type ServerUptimeRow struct {
	ServerID    string   `gorm:"column:server_id"`
	ServerName  string   `gorm:"column:server_name"`
	TotalChecks int64    `gorm:"column:total_checks"`
	UptimePct   *float64 `gorm:"column:uptime_pct"`
}

// SnapshotRepository owns the daily_snapshots table.
type SnapshotRepository interface {
	Upsert(ctx context.Context, snapshots []model.DailySnapshot) error
	// MissingDates lists days in [start, end] that have no snapshot at all.
	MissingDates(ctx context.Context, start, end time.Time) ([]time.Time, error)
	Totals(ctx context.Context, start, end time.Time) (*Totals, error)
	CountByLastStatus(ctx context.Context, date time.Time) (map[string]int64, error)
	LowestUptime(ctx context.Context, start, end time.Time, limit int) ([]LowUptimeServer, error)
	// AllServerUptime returns every server the window has a snapshot for, ordered
	// by server_id, with a nil uptime for no-data servers.
	AllServerUptime(ctx context.Context, start, end time.Time) ([]ServerUptimeRow, error)
}

type snapshotRepository struct {
	db *gorm.DB
}

// NewSnapshotRepository creates a SnapshotRepository.
func NewSnapshotRepository(db *gorm.DB) SnapshotRepository {
	return &snapshotRepository{db: db}
}

// Upsert writes snapshots in batches, overwriting a previous run of the same
// day so the job can be re-run safely.
func (r *snapshotRepository) Upsert(ctx context.Context, snapshots []model.DailySnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "server_id"}, {Name: "date"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"server_name", "on_checks", "actual_checks",
				"expected_checks", "uptime_pct", "last_status",
			}),
		}).
		CreateInBatches(snapshots, upsertBatch).Error
}

// MissingDates reports which days never got a snapshot, so a report can refuse
// rather than quietly average over a hole.
func (r *snapshotRepository) MissingDates(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	var present []time.Time
	err := r.db.WithContext(ctx).
		Model(&model.DailySnapshot{}).
		Distinct().
		Where("date BETWEEN ? AND ?", start, end).
		Order("date").
		Pluck("date", &present).Error
	if err != nil {
		return nil, err
	}

	have := make(map[string]bool, len(present))
	for _, d := range present {
		have[d.Format(time.DateOnly)] = true
	}

	var missing []time.Time
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if !have[d.Format(time.DateOnly)] {
			missing = append(missing, d)
		}
	}
	return missing, nil
}

// Folds each server's days into one row first: counting rows counts server-days.
const totalsQuery = `
WITH per_server AS (
	SELECT server_id,
	       SUM(on_checks)       AS on_checks,
	       SUM(actual_checks)   AS actual_checks,
	       SUM(expected_checks) AS expected_checks
	FROM daily_snapshots
	WHERE date BETWEEN ? AND ?
	GROUP BY server_id
), classified AS (
	SELECT actual_checks, expected_checks,
	       -- NULL not 0: keeps no-data out of AVG and inside servers_no_data.
	       CASE WHEN actual_checks > 0
	            THEN on_checks::numeric / actual_checks * 100
	       END AS uptime_pct
	FROM per_server
)
SELECT COUNT(*) AS total_servers,
       AVG(uptime_pct) AS avg_uptime_pct,
       COUNT(*) FILTER (WHERE uptime_pct = 100) AS servers_uptime100,
       COUNT(*) FILTER (WHERE uptime_pct = 0) AS servers_uptime0,
       COUNT(*) FILTER (WHERE uptime_pct IS NULL) AS servers_no_data,
       COALESCE(SUM(actual_checks), 0) AS sum_actual_checks,
       COALESCE(SUM(expected_checks), 0) AS sum_expected_checks
FROM classified`

// Totals aggregates the window per server.
func (r *snapshotRepository) Totals(ctx context.Context, start, end time.Time) (*Totals, error) {
	var out Totals
	err := r.db.WithContext(ctx).Raw(totalsQuery, start, end).Scan(&out).Error
	return &out, err
}

// CountByLastStatus counts the closing status on the report's final day.
func (r *snapshotRepository) CountByLastStatus(ctx context.Context, date time.Time) (map[string]int64, error) {
	var rows []struct {
		LastStatus *string
		Count      int64
	}
	err := r.db.WithContext(ctx).
		Model(&model.DailySnapshot{}).
		Select("last_status, COUNT(*) as count").
		Where("date = ?", date).
		Group("last_status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		if row.LastStatus == nil {
			continue
		}
		out[*row.LastStatus] = row.Count
	}
	return out, nil
}

// Ranks servers, not server-days; server_name comes from the window's last snapshot.
const lowestUptimeQuery = `
WITH per_server AS (
	SELECT server_id,
	       SUM(on_checks)::numeric / NULLIF(SUM(actual_checks), 0) * 100 AS uptime_pct
	FROM daily_snapshots
	WHERE date BETWEEN ? AND ?
	GROUP BY server_id
), latest_name AS (
	SELECT DISTINCT ON (server_id) server_id, server_name
	FROM daily_snapshots
	WHERE date BETWEEN ? AND ?
	ORDER BY server_id, date DESC
)
SELECT p.server_id, n.server_name, p.uptime_pct
FROM per_server p
JOIN latest_name n USING (server_id)
WHERE p.uptime_pct IS NOT NULL
ORDER BY p.uptime_pct ASC, p.server_id ASC
LIMIT ?`

// LowestUptime ranks the worst servers, excluding those with no data.
func (r *snapshotRepository) LowestUptime(ctx context.Context, start, end time.Time, limit int) ([]LowUptimeServer, error) {
	var out []LowUptimeServer
	err := r.db.WithContext(ctx).
		Raw(lowestUptimeQuery, start, end, start, end, limit).
		Scan(&out).Error
	return out, err
}

// Same per-server fold as the ranking, but keeps no-data (NULL uptime) rows and
// returns everyone, so the attachment matches total_servers in the report body.
const allServerUptimeQuery = `
WITH per_server AS (
	SELECT server_id,
	       COALESCE(SUM(actual_checks), 0) AS total_checks,
	       SUM(on_checks)::numeric / NULLIF(SUM(actual_checks), 0) * 100 AS uptime_pct
	FROM daily_snapshots
	WHERE date BETWEEN ? AND ?
	GROUP BY server_id
), latest_name AS (
	SELECT DISTINCT ON (server_id) server_id, server_name
	FROM daily_snapshots
	WHERE date BETWEEN ? AND ?
	ORDER BY server_id, date DESC
)
SELECT p.server_id, n.server_name, p.total_checks, p.uptime_pct
FROM per_server p
JOIN latest_name n USING (server_id)
ORDER BY p.server_id ASC`

// AllServerUptime returns the whole population with per-server uptime.
func (r *snapshotRepository) AllServerUptime(ctx context.Context, start, end time.Time) ([]ServerUptimeRow, error) {
	var out []ServerUptimeRow
	err := r.db.WithContext(ctx).
		Raw(allServerUptimeQuery, start, end, start, end).
		Scan(&out).Error
	return out, err
}
