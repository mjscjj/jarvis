package pipeline

import (
	"context"
	"fmt"
	"sync"
)

// keyedQueue coalesces exact duplicate work while it is waiting in the queue and
// preserves a newer entity version as distinct work. The key is released when a
// worker receives the item, so another notification during a long-running model
// call schedules one follow-up pass instead of being lost.
//
// It is intentionally in-memory: MySQL state and the reconciliation schedules
// recover any notification lost with the process.
type keyedQueue[T any] struct {
	items chan T
	key   func(T) string

	mu     sync.Mutex
	queued map[string]struct{}
}

func newKeyedQueue[T any](capacity int, key func(T) string) (*keyedQueue[T], error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("pipeline queue capacity must be positive")
	}
	if key == nil {
		return nil, fmt.Errorf("pipeline queue key function is nil")
	}
	return &keyedQueue[T]{items: make(chan T, capacity), key: key, queued: make(map[string]struct{})}, nil
}

func (q *keyedQueue[T]) enqueue(ctx context.Context, item T) error {
	key := q.key(item)
	if key == "" {
		return fmt.Errorf("pipeline queue item key is empty")
	}
	q.mu.Lock()
	if _, exists := q.queued[key]; exists {
		q.mu.Unlock()
		return nil
	}
	q.queued[key] = struct{}{}
	q.mu.Unlock()

	select {
	case q.items <- item:
		return nil
	case <-ctx.Done():
		q.discard(item)
		return ctx.Err()
	}
}

func (q *keyedQueue[T]) received(item T) {
	q.discard(item)
}

func (q *keyedQueue[T]) discard(item T) {
	q.mu.Lock()
	delete(q.queued, q.key(item))
	q.mu.Unlock()
}
