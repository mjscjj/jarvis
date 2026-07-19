package decide

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrTodoNotFound      = errors.New("confirmation Todo not found")
	ErrVersionConflict   = errors.New("confirmation version conflict")
	ErrInvalidTransition = errors.New("invalid confirmation transition")
	ErrTaskExists        = errors.New("Task already exists for Todo")
	ErrInvalidInput      = errors.New("invalid confirmation input")
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
	Slots        json.RawMessage `json:"slots"`
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
