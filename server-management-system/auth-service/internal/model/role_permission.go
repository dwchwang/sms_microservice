package model

import (
	"time"

	"github.com/google/uuid"
)

// RolePermission maps a permission scope to a role.
type RolePermission struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	RoleID    uuid.UUID `gorm:"type:uuid;not null;index" json:"role_id"`
	Scope     string    `gorm:"type:varchar(100);not null" json:"scope"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName overrides the default table name for GORM.
func (RolePermission) TableName() string { return "role_permissions" }
