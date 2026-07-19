package decide

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrCodexBudgetExceeded  = errors.New("codex decision budget exceeded")
	ErrBudgetClockRegressed = errors.New("codex budget clock regressed")
)

type BudgetOptions struct {
	MaxCallsPerHour int
	MaxCallsPerDay  int
}

type BudgetUsage struct {
	AcquiredAt time.Time `json:"acquired_at"`
	HourUsed   int       `json:"hour_used"`
	HourLimit  int       `json:"hour_limit"`
	DayUsed    int       `json:"day_used"`
	DayLimit   int       `json:"day_limit"`
}

type BudgetExceededError struct {
	Window string
	Usage  BudgetUsage
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("%s: window=%s hour=%d/%d day=%d/%d", ErrCodexBudgetExceeded,
		e.Window, e.Usage.HourUsed, e.Usage.HourLimit, e.Usage.DayUsed, e.Usage.DayLimit)
}

func (e *BudgetExceededError) Unwrap() error { return ErrCodexBudgetExceeded }

type CodexBudget struct {
	mu      sync.Mutex
	opts    BudgetOptions
	entries []time.Time
	now     func() time.Time
	lastNow time.Time
}

func NewCodexBudget(opts BudgetOptions) (*CodexBudget, error) {
	return newCodexBudgetWithClock(opts, time.Now)
}

func newCodexBudgetWithClock(opts BudgetOptions, now func() time.Time) (*CodexBudget, error) {
	if opts.MaxCallsPerHour <= 0 {
		return nil, fmt.Errorf("codex budget max calls per hour must be positive")
	}
	if opts.MaxCallsPerDay <= 0 {
		return nil, fmt.Errorf("codex budget max calls per day must be positive")
	}
	if opts.MaxCallsPerDay < opts.MaxCallsPerHour {
		return nil, fmt.Errorf("codex budget daily limit must not be smaller than hourly limit")
	}
	if now == nil {
		return nil, fmt.Errorf("codex budget clock is nil")
	}
	return &CodexBudget{opts: opts, now: now}, nil
}

// Acquire reserves one Codex call immediately. Calls that reach the CLI count
// even if Codex later times out or returns invalid output.
func (b *CodexBudget) Acquire() (BudgetUsage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now().UTC()
	if !b.lastNow.IsZero() && now.Before(b.lastNow) {
		return BudgetUsage{}, fmt.Errorf("%w: previous=%s current=%s", ErrBudgetClockRegressed, b.lastNow, now)
	}
	b.lastNow = now
	dayCutoff := now.Add(-24 * time.Hour)
	firstActive := 0
	for firstActive < len(b.entries) && !b.entries[firstActive].After(dayCutoff) {
		firstActive++
	}
	if firstActive != 0 {
		copy(b.entries, b.entries[firstActive:])
		b.entries = b.entries[:len(b.entries)-firstActive]
	}

	hourCutoff := now.Add(-time.Hour)
	hourUsed := 0
	for index := len(b.entries) - 1; index >= 0; index-- {
		if !b.entries[index].After(hourCutoff) {
			break
		}
		hourUsed++
	}
	usage := BudgetUsage{
		AcquiredAt: now, HourUsed: hourUsed, HourLimit: b.opts.MaxCallsPerHour,
		DayUsed: len(b.entries), DayLimit: b.opts.MaxCallsPerDay,
	}
	if usage.DayUsed >= usage.DayLimit {
		return usage, &BudgetExceededError{Window: "day", Usage: usage}
	}
	if usage.HourUsed >= usage.HourLimit {
		return usage, &BudgetExceededError{Window: "hour", Usage: usage}
	}
	b.entries = append(b.entries, now)
	usage.HourUsed++
	usage.DayUsed++
	return usage, nil
}
