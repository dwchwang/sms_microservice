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

// SnapshotRepository owns the daily_snapshots table.
type SnapshotRepository interface {
	Upsert(ctx context.Context, snapshots []model.DailySnapshot) error
	// MissingDates lists days in [start, end] that have no snapshot at all.
	MissingDates(ctx context.Context, start, end time.Time) ([]time.Time, error)
	Totals(ctx context.Context, start, end time.Time) (*Totals, error)
	CountByLastStatus(ctx context.Context, date time.Time) (map[string]int64, error)
	LowestUptime(ctx context.Context, start, end time.Time, limit int) ([]LowUptimeServer, error)
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

// Totals aggregates the window. AVG skips NULL uptime_pct, so servers with no
// data cannot drag the average down.
func (r *snapshotRepository) Totals(ctx context.Context, start, end time.Time) (*Totals, error) {
	var out Totals
	err := r.db.WithContext(ctx).
		Model(&model.DailySnapshot{}).
		Select(`
			COUNT(DISTINCT server_id) AS total_servers,
			AVG(uptime_pct) AS avg_uptime_pct,
			COUNT(*) FILTER (WHERE uptime_pct = 100) AS servers_uptime100,
			COUNT(*) FILTER (WHERE uptime_pct = 0) AS servers_uptime0,
			COUNT(*) FILTER (WHERE uptime_pct IS NULL) AS servers_no_data,
			COALESCE(SUM(actual_checks), 0) AS sum_actual_checks,
			COALESCE(SUM(expected_checks), 0) AS sum_expected_checks`).
		Where("date BETWEEN ? AND ?", start, end).
		Scan(&out).Error
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

// LowestUptime ranks the worst servers. Rows with no data are excluded: they
// are counted separately, and ranking them worst would assert something about
// what was never measured.
func (r *snapshotRepository) LowestUptime(ctx context.Context, start, end time.Time, limit int) ([]LowUptimeServer, error) {
	var out []LowUptimeServer
	err := r.db.WithContext(ctx).
		Model(&model.DailySnapshot{}).
		Select("server_id, server_name, uptime_pct").
		Where("date BETWEEN ? AND ?", start, end).
		Where("uptime_pct IS NOT NULL").
		Order("uptime_pct ASC, server_id ASC").
		Limit(limit).
		Scan(&out).Error
	return out, err
}
