package models

import (
	"time"

	"github.com/google/uuid"
)

// Scheduled server task types.
const (
	TaskTypeRestart   = "restart"
	TaskTypeCommand   = "command"
	TaskTypeBroadcast = "broadcast"
)

// Scheduled server task run outcomes, stored in server_tasks.last_status.
const (
	TaskStatusOK      = "ok"
	TaskStatusPending = "pending" // restart waiting out its warning period
	TaskStatusSkipped = "skipped"
	TaskStatusFailed  = "failed"
)

// ServerTaskPayload holds the type-specific settings of a scheduled task.
// Only the field matching the task type is used.
type ServerTaskPayload struct {
	Command     string `json:"command,omitempty"`
	Message     string `json:"message,omitempty"`
	WarnMinutes int    `json:"warn_minutes,omitempty"`
}

// ServerTask is a cron-scheduled action on one server.
type ServerTask struct {
	ID         uuid.UUID         `json:"id"`
	ServerID   uuid.UUID         `json:"server_id"`
	CreatedBy  *uuid.UUID        `json:"created_by,omitempty"`
	Type       string            `json:"type"`
	CronExpr   string            `json:"cron_expr"`
	Payload    ServerTaskPayload `json:"payload"`
	Enabled    bool              `json:"enabled"`
	NextRunAt  *time.Time        `json:"next_run_at,omitempty"`
	LastRunAt  *time.Time        `json:"last_run_at,omitempty"`
	LastStatus *string           `json:"last_status,omitempty"`
	LastError  *string           `json:"last_error,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}
