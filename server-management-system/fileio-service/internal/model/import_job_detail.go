package model

import (
	"time"

	"github.com/google/uuid"
)

// ImportJobDetail represents a single row result within an import job.
type ImportJobDetail struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ImportJobID uuid.UUID `gorm:"type:uuid;not null" json:"import_job_id"`
	RowNumber   int       `gorm:"type:integer;not null" json:"row_number"`
	ServerID    string    `gorm:"type:varchar(100)" json:"server_id,omitempty"`
	ServerName  string    `gorm:"type:varchar(255)" json:"server_name,omitempty"`
	Status      string    `gorm:"type:varchar(20);not null" json:"status"` // success | failed
	ErrorReason string    `gorm:"type:text" json:"error_reason,omitempty"`
	CreatedAt   time.Time `gorm:"not null;default:now()" json:"created_at"`
}

// TableName overrides the default table name for GORM.
func (ImportJobDetail) TableName() string {
	return "fileio_schema.import_job_details"
}
