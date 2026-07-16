package dto

// CreateServerRequest is the request body for creating a server.
type CreateServerRequest struct {
	ServerID    string `json:"server_id" binding:"required,max=100"`
	ServerName  string `json:"server_name" binding:"required,max=255"`
	IPv4        string `json:"ipv4" binding:"required,ipv4"`
	TCPPort     int    `json:"tcp_port" binding:"omitempty,min=1,max=65535"`
	OS          string `json:"os,omitempty"`
	CPUCores    int    `json:"cpu_cores,omitempty" binding:"omitempty,gt=0"`
	RAMGB       int    `json:"ram_gb,omitempty" binding:"omitempty,gt=0"`
	DiskGB      int    `json:"disk_gb,omitempty" binding:"omitempty,gt=0"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
}

// UpdateServerRequest is the request body for updating a server.
// NOTE: server_id is NOT included — it cannot be changed.
type UpdateServerRequest struct {
	ServerName  *string `json:"server_name,omitempty" binding:"omitempty,max=255"`
	IPv4        *string `json:"ipv4,omitempty" binding:"omitempty,ipv4"`
	TCPPort     *int    `json:"tcp_port,omitempty" binding:"omitempty,min=1,max=65535"`
	OS          *string `json:"os,omitempty"`
	CPUCores    *int    `json:"cpu_cores,omitempty" binding:"omitempty,gt=0"`
	RAMGB       *int    `json:"ram_gb,omitempty" binding:"omitempty,gt=0"`
	DiskGB      *int    `json:"disk_gb,omitempty" binding:"omitempty,gt=0"`
	Location    *string `json:"location,omitempty"`
	Description *string `json:"description,omitempty"`
}

// ServerFilter holds query parameters for listing servers.
type ServerFilter struct {
	Status     string `form:"status"`
	ServerID   string `form:"server_id"`
	ServerName string `form:"server_name"`
	IPv4       string `form:"ipv4"`
	OS         string `form:"os"`
	Location   string `form:"location"`
	SortBy     string `form:"sort_by"`
	SortOrder  string `form:"sort_order"`
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
}
