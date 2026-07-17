package model

import "time"

// Idempotency states.
const (
	IdempotencyProcessing = "processing"
	IdempotencyCompleted  = "completed"
	IdempotencyFailed     = "failed"
)

// Idempotency records a mutation so a retried request replays its response.
type Idempotency struct {
	ActorID        string    `gorm:"primaryKey;column:actor_id"`
	Endpoint       string    `gorm:"primaryKey;column:endpoint"`
	IdempotencyKey string    `gorm:"primaryKey;column:idempotency_key"`
	RequestHash    string    `gorm:"column:request_hash"`
	State          string    `gorm:"column:state"`
	StatusCode     int       `gorm:"column:status_code"`
	ResponseBody   []byte    `gorm:"column:response_body;type:jsonb"`
	ExpiresAt      time.Time `gorm:"column:expires_at"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (Idempotency) TableName() string { return "api_idempotency" }
