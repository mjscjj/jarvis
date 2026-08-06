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
// It is intentionally in-memory: SQLite state and the reconciliation schedules
// recover any notification lost with the process.
type keyedQueue[T any] struct {
	items chan T
	key   func(T) string

	mu     sync.Mutex
	queued map[string]struct{}
}

// keyedLocker serializes work for one durable entity while allowing unrelated
// keys to run concurrently. Entries are reference-counted so a long-running
// process does not retain every chat ID it has ever seen.
type keyedLocker struct {
	mu      sync.Mutex
	entries map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

func newKeyedLocker() *keyedLocker {
	return &keyedLocker{entries: make(map[string]*keyedLock)}
}

func (l *keyedLocker) lock(key string) (func(), error) {
	if key == "" {
		return nil, fmt.Errorf("pipeline lock key is empty")
	}
	l.mu.Lock()
	entry := l.entries[key]
	if entry == nil {
		entry = &keyedLock{}
		l.entries[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.entries, key)
		}
		l.mu.Unlock()
	}, nil
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
