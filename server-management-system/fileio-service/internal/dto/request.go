package dto

// ExportFilter represents the filter and sort options for server export.
type ExportFilter struct {
	Status     string `json:"status,omitempty"`
	ServerName string `json:"server_name,omitempty"`
	IPv4       string `json:"ipv4,omitempty"`
	Location   string `json:"location,omitempty"`
	OS         string `json:"os,omitempty"`
	SortBy     string `json:"sort_by,omitempty"`
	SortOrder  string `json:"sort_order,omitempty"`
}
