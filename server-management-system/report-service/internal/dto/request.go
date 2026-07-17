package dto

// SummaryRequest is the query for GET /reports/summary.
type SummaryRequest struct {
	StartDate string `form:"start_date" binding:"required"`
	EndDate   string `form:"end_date" binding:"required"`
}

// SendReportRequest is the body of POST /reports.
type SendReportRequest struct {
	StartDate      string `json:"start_date" binding:"required"`
	EndDate        string `json:"end_date" binding:"required"`
	RecipientEmail string `json:"recipient_email" binding:"required,email"`
}
