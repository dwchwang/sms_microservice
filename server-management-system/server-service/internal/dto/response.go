package dto

import (
	"time"

	"github.com/vcs-sms/server-service/internal/infrastructure/status"
)

// ServerResponse is the public representation of a server.
type ServerResponse struct {
	ServerID        string     `json:"server_id"`
	ServerName      string     `json:"server_name"`
	Status          string     `json:"status"`
	StatusChangedAt *time.Time `json:"status_changed_at,omitempty"`
	// Read from Redis at serialize time; display-only, null if unavailable.
	LastStatusCheck *time.Time `json:"last_status_check"`
	IPv4            string     `json:"ipv4"`
	TCPPort         int        `json:"tcp_port"`
	OS              string     `json:"os,omitempty"`
	CPUCores        int        `json:"cpu_cores,omitempty"`
	RAMGB           int        `json:"ram_gb,omitempty"`
	DiskGB          int        `json:"disk_gb,omitempty"`
	Location        string     `json:"location,omitempty"`
	Description     string     `json:"description,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// StatsResponse is the current status breakdown for the dashboard.
type StatsResponse struct {
	Total   int64 `json:"total"`
	On      int64 `json:"on"`
	Off     int64 `json:"off"`
	Unknown int64 `json:"unknown"`
}

// UptimeResponse is the dashboard's live uptime picture, built from the lifetime
// counters Monitoring keeps in Redis. Unlike the report, it needs no snapshot and
// is answerable at any moment.
type UptimeResponse struct {
	TotalServers   int64 `json:"total_servers"`
	ServersOn      int64 `json:"servers_on"`
	ServersOff     int64 `json:"servers_off"`
	ServersUnknown int64 `json:"servers_unknown"`

	// ServersNoData are active servers Monitoring has never checked.
	ServersNoData        int64 `json:"servers_no_data"`
	ServersUptime100     int64 `json:"servers_uptime_100"`
	ServersUptimePartial int64 `json:"servers_uptime_partial"`
	ServersUptime0       int64 `json:"servers_uptime_0"`

	// AvgUptimePct is null when no server has been checked yet.
	AvgUptimePct *float64 `json:"avg_uptime_pct"`

	Top10LowestUptime []status.ServerUptime `json:"top_10_lowest_uptime"`
}

// PopulationServer is one server in the reporting population.
type PopulationServer struct {
	ServerID   string     `json:"server_id"`
	ServerName string     `json:"server_name"`
	CreatedAt  time.Time  `json:"created_at"`
	DeletedAt  *time.Time `json:"deleted_at"`
}

// PopulationResponse is a cursor page of the reporting population.
type PopulationResponse struct {
	Servers    []PopulationServer `json:"servers"`
	NextCursor string             `json:"next_cursor"`
}

// ImportResponse is the outcome of an Excel import.
type ImportResponse struct {
	TotalRows        int             `json:"total_rows"`
	Succeeded        ImportSucceeded `json:"succeeded"`
	Failed           ImportFailed    `json:"failed"`
	SkippedDuplicate ImportSkipped   `json:"skipped_duplicate"`
}

type ImportSucceeded struct {
	Count int      `json:"count"`
	Items []string `json:"items"`
}

type ImportFailed struct {
	Count int                `json:"count"`
	Items []ImportFailedItem `json:"items"`
}

// ImportFailedItem names the row so the user can fix the file.
type ImportFailedItem struct {
	Row      int    `json:"row"`
	ServerID string `json:"server_id"`
	Reason   string `json:"reason"`
}

type ImportSkipped struct {
	Count int      `json:"count"`
	Items []string `json:"items"`
}

// ListServerResponse represents a paginated list of servers.
type ListServerResponse struct {
	Servers    []ServerResponse `json:"servers"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
}
