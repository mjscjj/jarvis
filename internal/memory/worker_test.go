package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	pending []PendingMessage
	marked  [][]uint64
}

func (s *fakeStore) ListPending(_ context.Context, _ int) ([]PendingMessage, error) {
	return s.pending, nil
}

func (s *fakeStore) MarkProcessed(_ context.Context, ids []uint64, _ time.Time) error {
	s.marked = append(s.marked, ids)
	return nil
}

type fakeAdder struct {
	inputs []AddInput
	err    error
}

func (a *fakeAdder) Add(_ context.Context, input AddInput) error {
	if a.err != nil {
		return a.err
	}
	a.inputs = append(a.inputs, input)
	return nil
}

func TestWorkerWindowingFilteringAndMarking(t *testing.T) {
	base := time.Date(2026, 7, 19, 10, 0, 0, 0, time.Local).UnixMilli()
	store := &fakeStore{pending: []PendingMessage{
		{ID: 1, MessageID: "om_1", ChatID: "oc_a", ChatName: "A", SenderName: "Alice", SenderType: "user", Content: "first", CreateTime: base, RenderOK: true},
		{ID: 2, MessageID: "om_2", ChatID: "oc_a", ChatName: "A", SenderName: "Bot", SenderType: "app", Content: "noise", CreateTime: base + time.Minute.Milliseconds(), RenderOK: true},
		{ID: 3, MessageID: "om_3", ChatID: "oc_a", ChatName: "A", SenderName: "Bob", SenderType: "user", Content: "second", CreateTime: base + (40 * time.Minute).Milliseconds(), RenderOK: true},
	}}
	adder := &fakeAdder{}
	worker, err := NewWorker(store, adder, WorkerOptions{
		BatchLimit: 10, WindowGap: 30 * time.Minute, WindowMaxMessages: 40, Location: time.Local,
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	stats, err := worker.MemorizeOnce(context.Background())
	if err != nil {
		t.Fatalf("MemorizeOnce() error = %v", err)
	}
	if stats.Loaded != 3 || stats.Processed != 3 || stats.MemorizedMessages != 2 || stats.SkippedMessages != 1 || stats.Windows != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if len(adder.inputs) != 2 || !strings.Contains(adder.inputs[0].Transcript, "Alice: first") {
		t.Fatalf("memory inputs = %#v", adder.inputs)
	}
	if len(store.marked) != 2 || len(store.marked[0]) != 2 || store.marked[0][1] != 2 {
		t.Fatalf("marked = %#v", store.marked)
	}
}

func TestWorkerDoesNotMarkFailedWindow(t *testing.T) {
	store := &fakeStore{pending: []PendingMessage{{
		ID: 1, MessageID: "om_1", ChatID: "oc_a", SenderName: "Alice", SenderType: "user", Content: "first", CreateTime: 1, RenderOK: true,
	}}}
	worker, err := NewWorker(store, &fakeAdder{err: errors.New("sidecar down")}, WorkerOptions{
		BatchLimit: 10, WindowGap: time.Minute, WindowMaxMessages: 40, Location: time.UTC,
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := worker.MemorizeOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "sidecar down") {
		t.Fatalf("MemorizeOnce() error = %v", err)
	}
	if len(store.marked) != 0 {
		t.Fatalf("failed window marked = %#v", store.marked)
	}
}

func TestWorkerMarksNoiseWithoutCallingSidecar(t *testing.T) {
	store := &fakeStore{pending: []PendingMessage{
		{ID: 1, MessageID: "om_1", ChatID: "oc_a", SenderName: "Bot", SenderType: "app", Content: "alert", CreateTime: 1, RenderOK: true},
		{ID: 2, MessageID: "om_2", ChatID: "oc_a", SenderName: "Alice", SenderType: "user", Content: "[图片]", CreateTime: 2, RenderOK: true},
		{ID: 3, MessageID: "om_3", ChatID: "oc_a", SenderName: "Alice", SenderType: "user", Content: "[表情]", CreateTime: 3, RenderOK: true},
		{ID: 4, MessageID: "om_4", ChatID: "oc_a", SenderName: "Alice", SenderType: "user", Content: "[文件:demo.zip]", CreateTime: 4, RenderOK: true},
	}}
	adder := &fakeAdder{}
	worker, err := NewWorker(store, adder, WorkerOptions{
		BatchLimit: 10, WindowGap: time.Minute, WindowMaxMessages: 40, Location: time.UTC,
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	stats, err := worker.MemorizeOnce(context.Background())
	if err != nil {
		t.Fatalf("MemorizeOnce() error = %v", err)
	}
	if len(adder.inputs) != 0 || stats.SkippedMessages != 4 || len(store.marked) != 1 || len(store.marked[0]) != 4 {
		t.Fatalf("inputs=%#v stats=%#v marked=%#v", adder.inputs, stats, store.marked)
	}
}
