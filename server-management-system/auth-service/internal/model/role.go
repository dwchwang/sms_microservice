package model

import (
	"time"

	"github.com/google/uuid"
)

// Role represents a user role with associated permissions.
type Role struct {
	ID          uuid.UUID        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name        string           `gorm:"type:varchar(50);uniqueIndex;not null" json:"name"`
	Description string           `gorm:"type:text" json:"description"`
	Permissions []RolePermission `gorm:"foreignKey:RoleID" json:"permissions,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// TableName overrides the default table name for GORM.
func (Role) TableName() string { return "roles" }
