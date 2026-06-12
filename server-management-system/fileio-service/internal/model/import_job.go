package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ImportJob represents an import job record in the database.
type ImportJob struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Status       string         `gorm:"type:varchar(20);not null;default:'pending'" json:"status"`
	FileName     string         `gorm:"type:varchar(255);not null" json:"file_name"`
	FilePath     string         `gorm:"type:varchar(500);not null" json:"file_path"`
	TotalRows    int            `gorm:"type:integer;default:0" json:"total_rows"`
	SuccessCount int            `gorm:"type:integer;default:0" json:"success_count"`
	FailedCount  int            `gorm:"type:integer;default:0" json:"failed_count"`
	ErrorMessage *string        `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uuid.UUID     `gorm:"type:uuid" json:"created_by,omitempty"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	CreatedAt    time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName overrides the default table name for GORM.
func (ImportJob) TableName() string {
	return "fileio_schema.import_jobs"
}
