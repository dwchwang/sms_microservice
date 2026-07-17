package model

import "time"

// DailySnapshot is one server's uptime for one day. The snapshot job writes
// 10.000 of these per day, and every report reads from here rather than from
// Elasticsearch.
type DailySnapshot struct {
	ServerID   string    `gorm:"column:server_id;primaryKey"`
	Date       time.Time `gorm:"column:date;primaryKey;type:date"`
	ServerName string    `gorm:"column:server_name;not null"`
	OnChecks   int       `gorm:"column:on_checks;not null"`
	// ActualChecks is 0 when the server produced no health facts that day.
	ActualChecks int `gorm:"column:actual_checks;not null"`
	// ExpectedChecks follows the server's own lifecycle, so one created at
	// 18:00 expects fewer checks than one that existed all day.
	ExpectedChecks int `gorm:"column:expected_checks;not null"`
	// UptimePct is NULL when there is no data: a server nobody measured has an
	// unknown uptime, not 0%, and averaging 0 in would turn a collection
	// failure into a false report.
	UptimePct *float64 `gorm:"column:uptime_pct;type:numeric(5,2)"`
	// LastStatus is the final status of the day, NULL when no data.
	LastStatus *string   `gorm:"column:last_status"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (DailySnapshot) TableName() string { return "daily_snapshots" }
