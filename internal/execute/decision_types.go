package execute

import (
	"context"
	"errors"
)

// Todo-side judgment errors. Version/transition/input errors are shared with the
// Task-side execution store (see store.go), so the judgment step reuses those
// sentinels rather than declaring near-duplicates.
var (
	ErrTodoNotFound           = errors.New("decision Todo not found")
	ErrTaskExists             = errors.New("Task already exists for Todo")
	ErrLifecycleStageDisabled = errors.New("lifecycle pipeline stage is disabled")
)

// LifecycleNotifier is implemented by the process-level pipeline. Notifications
// accelerate durable state transitions; scheduled reconciliation remains the
// recovery path if the process exits after a database commit.
type LifecycleNotifier interface {
	TodoReady(context.Context, uint64, int32) error
	TaskReady(context.Context, uint64, int32) error
}
