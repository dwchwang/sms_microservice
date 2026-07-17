package model

import (
	"time"

	"github.com/google/uuid"
)

// Report job states.
const (
	StateProcessing = "processing"
	StateGenerated  = "generated"
	StateSending    = "sending"
	StateSent       = "sent"
	StateFailed     = "failed"
	// StateDeliveryUnknown marks a send whose outcome the SMTP exchange never
	// revealed. It is never retried blindly; an operator checks the Message-ID.
	StateDeliveryUnknown = "delivery_unknown"
)

// Report types.
const (
	TypeDaily    = "daily"
	TypeOnDemand = "on-demand"
)

// ReportJob tracks one report from generation through delivery.
type ReportJob struct {
	ID             uuid.UUID  `gorm:"column:id;primaryKey;type:uuid;default:gen_random_uuid()"`
	ReportType     string     `gorm:"column:report_type;not null"`
	RequesterID    string     `gorm:"column:requester_id"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	StartAt        time.Time  `gorm:"column:start_at;type:date;not null"`
	EndAt          time.Time  `gorm:"column:end_at;type:date;not null"`
	RecipientEmail string     `gorm:"column:recipient_email;not null"`
	State          string     `gorm:"column:state;not null"`
	ResponseJSON   []byte     `gorm:"column:response_json;type:jsonb"`
	SMTPMessageID  string     `gorm:"column:smtp_message_id"`
	ErrorMessage   string     `gorm:"column:error_message"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime"`
	SentAt         *time.Time `gorm:"column:sent_at"`
}

func (ReportJob) TableName() string { return "report_jobs" }
