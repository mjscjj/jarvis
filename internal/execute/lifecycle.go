package execute

import (
	"context"
	"errors"
)

var (
	ErrTodoNotFound           = errors.New("Todo not found")
	ErrTaskExists             = errors.New("Task already exists for Todo")
	ErrLifecycleStageDisabled = errors.New("lifecycle pipeline stage is disabled")
)

// LifecycleNotifier accelerates durable transitions after their source commit.
// Scheduled reconciliation remains the recovery path for lost notifications.
type LifecycleNotifier interface {
	TodoReady(context.Context, uint64, int32) error
	TaskReady(context.Context, uint64, int32) error
}
