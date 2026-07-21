package decide

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrTodoNotFound           = errors.New("confirmation Todo not found")
	ErrVersionConflict        = errors.New("confirmation version conflict")
	ErrInvalidTransition      = errors.New("invalid confirmation transition")
	ErrTaskExists             = errors.New("Task already exists for Todo")
	ErrInvalidInput           = errors.New("invalid confirmation input")
	ErrLifecycleStageDisabled = errors.New("lifecycle pipeline stage is disabled")
)

type ApproveInput struct {
	TodoID          uint64
	ExpectedVersion int32
	Plan            json.RawMessage
	Channel         string
}

type RejectInput struct {
	TodoID          uint64
	ExpectedVersion int32
	Reason          string
	Channel         string
}

type TaskView struct {
	ID           uint64          `json:"id"`
	TodoID       uint64          `json:"todo_id"`
	Title        string          `json:"title"`
	ActionType   string          `json:"action_type"`
	Background   json.RawMessage `json:"background"`
	Plan         json.RawMessage `json:"plan"`
	ConfirmedBy  string          `json:"confirmed_by"`
	ConfirmedAt  time.Time       `json:"confirmed_at"`
	ActionHash   string          `json:"action_hash"`
	Status       string          `json:"status"`
	AutonomyMode string          `json:"autonomy_mode"`
	ProjectID    *uint64         `json:"project_id"`
	Version      int32           `json:"version"`
}

type RejectResult struct {
	TodoID  uint64 `json:"todo_id"`
	Status  string `json:"status"`
	Version int32  `json:"version"`
}

type ConfirmationService interface {
	Approve(context.Context, ApproveInput) (*TaskView, error)
	Reject(context.Context, RejectInput) (*RejectResult, error)
	Supplement(context.Context, SupplementInput) (*SupplementResult, error)
}

// LifecycleNotifier is implemented by the process-level pipeline. Notifications
// accelerate durable state transitions; scheduled reconciliation remains the
// recovery path if the process exits after a database commit.
type LifecycleNotifier interface {
	TodoReady(context.Context, uint64, int32) error
	TaskReady(context.Context, uint64, int32) error
}
