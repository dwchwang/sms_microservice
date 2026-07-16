package dto

import "time"

// ServerResponse is the public representation of a server.
type ServerResponse struct {
	ServerID        string     `json:"server_id"`
	ServerName      string     `json:"server_name"`
	Status          string     `json:"status"`
	StatusChangedAt *time.Time `json:"status_changed_at,omitempty"`
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

// ListServerResponse represents a paginated list of servers.
type ListServerResponse struct {
	Servers    []ServerResponse `json:"servers"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
}
