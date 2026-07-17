package dto

import (
	"time"

	"github.com/vcs-sms/report-service/internal/repository"
)

// SummaryResponse is the report body.
//
// Two shapes of number live here on purpose. ServersOnAtEndAt/ServersOffAtEndAt
// are a snapshot at one instant, which is what the brief asks for. The uptime
// distribution describes the whole window, which is what actually answers "how
// did the system do". The long names keep the two from being confused.
type SummaryResponse struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`

	TotalServers         int64 `json:"total_servers"`
	ServersOnAtEndAt     int64 `json:"servers_on_at_end_at"`
	ServersOffAtEndAt    int64 `json:"servers_off_at_end_at"`
	ServersUptime100     int64 `json:"servers_uptime_100"`
	ServersUptimePartial int64 `json:"servers_uptime_partial"`
	ServersUptime0       int64 `json:"servers_uptime_0"`
	ServersNoData        int64 `json:"servers_no_data"`

	// AvgUptimePct is null when no server in the window had any data.
	AvgUptimePct *float64 `json:"avg_uptime_pct"`

	ExpectedChecks int64   `json:"expected_checks"`
	ActualChecks   int64   `json:"actual_checks"`
	CoveragePct    float64 `json:"coverage_pct"`
	// Degraded is true when coverage fell below the threshold, so thin data
	// cannot hide behind a pretty uptime number.
	Degraded bool `json:"degraded"`

	Top10LowestUptime []repository.LowUptimeServer `json:"top_10_lowest_uptime"`
}

// JobResponse is a report job's public state.
type JobResponse struct {
	ID             string           `json:"id"`
	ReportType     string           `json:"report_type"`
	State          string           `json:"state"`
	StartDate      string           `json:"start_date"`
	EndDate        string           `json:"end_date"`
	RecipientEmail string           `json:"recipient_email"`
	SMTPMessageID  string           `json:"smtp_message_id,omitempty"`
	ErrorMessage   string           `json:"error_message,omitempty"`
	Summary        *SummaryResponse `json:"summary,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	SentAt         *time.Time       `json:"sent_at,omitempty"`
}
