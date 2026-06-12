package dto

// ImportJobResponse is returned after initiating an import.
type ImportJobResponse struct {
	JobID    string `json:"job_id"`
	Status   string `json:"status"`
	FileName string `json:"file_name"`
	Message  string `json:"message"`
}

// ImportJobStatusResponse is returned when checking import progress.
type ImportJobStatusResponse struct {
	JobID        string            `json:"job_id"`
	Status       string            `json:"status"`
	FileName     string            `json:"file_name"`
	TotalRows    int               `json:"total_rows"`
	SuccessCount int               `json:"success_count"`
	FailedCount  int               `json:"failed_count"`
	SuccessList  []ImportRowResult `json:"success_list"`
	FailedList   []ImportRowResult `json:"failed_list"`
	ErrorMessage *string           `json:"error_message,omitempty"`
	StartedAt    *string           `json:"started_at,omitempty"`
	CompletedAt  *string           `json:"completed_at,omitempty"`
	CreatedAt    string            `json:"created_at"`
}

// ImportRowResult represents a single row result.
type ImportRowResult struct {
	RowNumber  int    `json:"row_number"`
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Status     string `json:"status"` // "success" or "failed"
	Reason     string `json:"reason,omitempty"`
}

// ServerExportRow represents a server row for Excel export.
type ServerExportRow struct {
	ServerID    string
	ServerName  string
	Status      string
	IPv4        string
	OS          string
	CPUCores    *int
	RAMGB       *float64
	DiskGB      *float64
	Location    string
	Description string
	CreatedAt   string
	UpdatedAt   string
}
