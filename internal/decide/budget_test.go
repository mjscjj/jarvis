package decide

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexBudgetSlidingWindows(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	budget, err := newCodexBudgetWithClock(BudgetOptions{MaxCallsPerHour: 2, MaxCallsPerDay: 3}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newCodexBudgetWithClock() error = %v", err)
	}
	for call := 1; call <= 2; call++ {
		usage, err := budget.Acquire()
		if err != nil {
			t.Fatalf("Acquire() call=%d error = %v", call, err)
		}
		if usage.HourUsed != call || usage.DayUsed != call {
			t.Fatalf("usage call=%d = %#v", call, usage)
		}
	}
	_, err = budget.Acquire()
	var exceeded *BudgetExceededError
	if !errors.As(err, &exceeded) || exceeded.Window != "hour" {
		t.Fatalf("third Acquire() error = %v", err)
	}

	now = now.Add(time.Hour)
	usage, err := budget.Acquire()
	if err != nil {
		t.Fatalf("Acquire() after hour boundary error = %v", err)
	}
	if usage.HourUsed != 1 || usage.DayUsed != 3 {
		t.Fatalf("usage after hour boundary = %#v", usage)
	}
	_, err = budget.Acquire()
	if !errors.As(err, &exceeded) || exceeded.Window != "day" {
		t.Fatalf("Acquire() at daily limit error = %v", err)
	}

	now = now.Add(23 * time.Hour)
	usage, err = budget.Acquire()
	if err != nil {
		t.Fatalf("Acquire() after day boundary error = %v", err)
	}
	if usage.DayUsed != 2 {
		t.Fatalf("usage after day boundary = %#v", usage)
	}
}

func TestCodexBudgetConcurrentAcquire(t *testing.T) {
	budget, err := NewCodexBudget(BudgetOptions{MaxCallsPerHour: 7, MaxCallsPerDay: 20})
	if err != nil {
		t.Fatalf("NewCodexBudget() error = %v", err)
	}
	var allowed atomic.Int32
	var unexpected atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 20; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := budget.Acquire(); err == nil {
				allowed.Add(1)
			} else if !errors.Is(err, ErrCodexBudgetExceeded) {
				unexpected.Add(1)
			}
		}()
	}
	group.Wait()
	if allowed.Load() != 7 || unexpected.Load() != 0 {
		t.Fatalf("allowed=%d unexpected=%d", allowed.Load(), unexpected.Load())
	}
}

func TestCodexBudgetRejectsClockRegression(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	budget, err := newCodexBudgetWithClock(BudgetOptions{MaxCallsPerHour: 1, MaxCallsPerDay: 1}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newCodexBudgetWithClock() error = %v", err)
	}
	if _, err := budget.Acquire(); err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	now = now.Add(-time.Second)
	if _, err := budget.Acquire(); !errors.Is(err, ErrBudgetClockRegressed) {
		t.Fatalf("regressed Acquire() error = %v", err)
	}
}

func TestNewCodexBudgetValidation(t *testing.T) {
	for _, opts := range []BudgetOptions{
		{},
		{MaxCallsPerHour: 1},
		{MaxCallsPerHour: 2, MaxCallsPerDay: 1},
	} {
		if _, err := NewCodexBudget(opts); err == nil {
			t.Fatalf("NewCodexBudget(%#v) succeeded", opts)
		}
	}
}
