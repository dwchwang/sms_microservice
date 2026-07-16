package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User represents a system user with authentication and role information.
type User struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Email        string         `gorm:"type:varchar(255);uniqueIndex;not null" json:"email"`
	PasswordHash string         `gorm:"type:varchar(255);not null" json:"-"`
	FullName     string         `gorm:"type:varchar(255)" json:"full_name"`
	RoleID       uuid.UUID      `gorm:"type:uuid;not null" json:"role_id"`
	Role         *Role          `gorm:"foreignKey:RoleID" json:"role,omitempty"`
	IsActive     bool           `gorm:"not null;default:true" json:"is_active"`
	LastLoginAt  *time.Time     `json:"last_login_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName overrides the default table name for GORM.
func (User) TableName() string { return "users" }
