package taskcreate

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"jarvis/internal/domain"
)

type ReadyNotifier interface {
	TaskReady(context.Context, uint64, int32) error
}

// Submitter creates a durable Task before it sends the best-effort real-time
// notification. The existing M5 reconciliation scan recovers a pending Task if
// the process exits between those steps.
type Submitter struct {
	factory  *Factory
	mu       sync.RWMutex
	notifier ReadyNotifier
}

func NewSubmitter(factory *Factory) (*Submitter, error) {
	if factory == nil {
		return nil, fmt.Errorf("Task submitter factory is nil")
	}
	return &Submitter{factory: factory}, nil
}

func (s *Submitter) SetNotifier(notifier ReadyNotifier) error {
	if notifier == nil {
		return fmt.Errorf("Task submitter notifier is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifier != nil {
		return fmt.Errorf("Task submitter notifier is already set")
	}
	s.notifier = notifier
	return nil
}

func (s *Submitter) Submit(ctx context.Context, input Input) (*domain.Task, error) {
	s.mu.RLock()
	notifier := s.notifier
	s.mu.RUnlock()
	if notifier == nil {
		return nil, fmt.Errorf("Task submitter notifier is not configured")
	}
	task, err := s.factory.Create(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := notifier.TaskReady(ctx, task.ID, task.Version); err != nil {
		return task, fmt.Errorf("notify Task ready task_id=%d: %w", task.ID, err)
	}
	return task, nil
}

// SubmitIdempotent is the interactive ingress contract. A redelivered external
// message resolves to the one existing Task; if that Task is still pending we
// re-emit the ready signal so a previously failed notification is recoverable.
func (s *Submitter) SubmitIdempotent(ctx context.Context, input Input) (*domain.Task, bool, error) {
	task, err := s.Submit(ctx, input)
	if err == nil {
		return task, true, nil
	}
	if !errors.Is(err, ErrExists) {
		return task, false, err
	}
	if input.SourceID == nil || input.OccurrenceKey == nil {
		return nil, false, fmt.Errorf("resolve existing Task: source identity is incomplete")
	}
	existing, findErr := s.factory.FindOccurrence(ctx, input.SourceType, *input.SourceID, *input.OccurrenceKey)
	if findErr != nil {
		return nil, false, findErr
	}
	if existing.Status == "pending" {
		s.mu.RLock()
		notifier := s.notifier
		s.mu.RUnlock()
		if notifier == nil {
			return nil, false, fmt.Errorf("Task submitter notifier is not configured")
		}
		if notifyErr := notifier.TaskReady(ctx, existing.ID, existing.Version); notifyErr != nil {
			return existing, false, fmt.Errorf("re-notify existing Task ready task_id=%d: %w", existing.ID, notifyErr)
		}
	}
	return existing, false, nil
}
