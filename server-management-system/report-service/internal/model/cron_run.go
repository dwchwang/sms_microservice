package model

import "time"

// Cron run states.
const (
	CronRunning = "running"
	CronDone    = "done"
	CronFailed  = "failed"
)

// Job names claimed in cron_runs.
const (
	JobSnapshot    = "snapshot"
	JobDailyReport = "daily_report"
)

// CronRun is one instance's claim on one scheduled job for one day. The row is
// what stops every replica from running the same job.
type CronRun struct {
	JobName      string    `gorm:"column:job_name;primaryKey"`
	RunDate      time.Time `gorm:"column:run_date;primaryKey;type:date"`
	State        string    `gorm:"column:state;not null"`
	Owner        string    `gorm:"column:owner;not null"`
	StartedAt    time.Time `gorm:"column:started_at;not null"`
	HeartbeatAt  time.Time `gorm:"column:heartbeat_at;not null"`
	FinishedAt   *time.Time
	ErrorMessage string `gorm:"column:error_message"`
}

func (CronRun) TableName() string { return "cron_runs" }
