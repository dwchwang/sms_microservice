package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Server represents a monitored server.
type Server struct {
	ID                uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ServerID          string         `gorm:"type:varchar(100);not null" json:"server_id"`
	ServerName        string         `gorm:"type:varchar(255);not null" json:"server_name"`
	Status            string         `gorm:"type:varchar(20);not null;default:'UNKNOWN'" json:"status"`
	StatusChangedAt   *time.Time     `gorm:"type:timestamptz" json:"status_changed_at"`
	StatusVersion     int64          `gorm:"type:bigint;default:0" json:"status_version"`
	LastStatusEventID string         `gorm:"type:varchar(255)" json:"last_status_event_id"`
	IPv4              string         `gorm:"type:inet;not null" json:"ipv4"`
	TCPPort           int            `gorm:"type:int;not null;default:80" json:"tcp_port"`
	OS                string         `gorm:"type:varchar(100)" json:"os"`
	CPUCores          int            `gorm:"type:int" json:"cpu_cores"`
	RAMGB             int            `gorm:"type:int" json:"ram_gb"`
	DiskGB            int            `gorm:"type:int" json:"disk_gb"`
	Location          string         `gorm:"type:varchar(255)" json:"location"`
	Description       string         `gorm:"type:text" json:"description"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName overrides the table name used by Server to `servers`
func (Server) TableName() string {
	return "servers"
}
