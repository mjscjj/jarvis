package pipeline

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"jarvis/internal/capture"
	"jarvis/internal/domain"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/observability"
)

type fakeExtractor struct {
	mu        sync.Mutex
	chatCalls int
	todos     []extract.TodoRef
}

func (f *fakeExtractor) ExtractChat(context.Context, string) (extract.WorkerStats, []extract.TodoRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatCalls++
	if f.chatCalls > 1 {
		return extract.WorkerStats{}, nil, nil
	}
	return extract.WorkerStats{ChatsLoaded: 1, ChatsProcessed: 1, Created: len(f.todos)}, append([]extract.TodoRef(nil), f.todos...), nil
}

func (f *fakeExtractor) ExtractOnce(context.Context) (extract.WorkerStats, error) {
	return extract.WorkerStats{}, nil
}

type fakeMaterializer struct {
	calls  chan m5Work
	result *execute.MaterializationResult
}

func (f *fakeMaterializer) MaterializeTodo(_ context.Context, todoID uint64, version int32) (*execute.MaterializationResult, error) {
	f.calls <- m5Work{TodoID: todoID, Version: version}
	result := *f.result
	return &result, nil
}

func (f *fakeMaterializer) MaterializeOnce(context.Context) (execute.MaterializationStats, error) {
	return execute.MaterializationStats{}, nil
}

type fakeExecutionStore struct {
	mu      sync.Mutex
	pending []domain.Task
}

func (f *fakeExecutionStore) LoadPending(context.Context, int) ([]domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := append([]domain.Task(nil), f.pending...)
	f.pending = nil
	return result, nil
}

func (*fakeExecutionStore) FailStaleExecuting(context.Context, time.Duration, time.Time) (int, error) {
	return 0, nil
}

type fakeTaskExecutor struct {
	calls  chan execute.ExecuteInput
	logIDs chan string
	err    error
}

func (f *fakeTaskExecutor) Execute(ctx context.Context, input execute.ExecuteInput) (*execute.ExecuteResult, error) {
	f.calls <- input
	if f.logIDs != nil {
		f.logIDs <- observability.LogID(ctx)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &execute.ExecuteResult{TaskID: input.TaskID, Status: "done"}, nil
}

func TestCoordinatorPreservesLogIDIntoM5(t *testing.T) {
	executor := &fakeTaskExecutor{
		calls:  make(chan execute.ExecuteInput, 1),
		logIDs: make(chan string, 1),
	}
	coordinator, err := newCoordinator(nil, nil, &fakeExecutionStore{}, executor, pipelineTestOptions())
	if err != nil {
		t.Fatalf("newCoordinator() error = %v", err)
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	if err := coordinator.Start(runtimeCtx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		cancel()
		coordinator.Wait()
	}()

	const requestLogID = "02-request-chain"
	if err := coordinator.TaskReady(observability.WithLogID(context.Background(), requestLogID), 61, 0); err != nil {
		t.Fatalf("TaskReady() error = %v", err)
	}
	select {
	case got := <-executor.logIDs:
		if got != requestLogID {
			t.Fatalf("M5 LogID = %q, want %q", got, requestLogID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("M5 was not triggered")
	}
}

func pipelineTestOptions() Options {
	return Options{
		ExecutionBatchLimit: 5, ExecutionConcurrency: 1, StaleExecuting: time.Minute,
		Logger: log.New(io.Discard, "", 0),
	}
}

func TestCoordinatorDrivesRealtimeM3M5(t *testing.T) {
	taskID := uint64(31)
	extractor := &fakeExtractor{todos: []extract.TodoRef{{ID: 21, Version: 3, Status: "extracted"}}}
	materializer := &fakeMaterializer{
		calls: make(chan m5Work, 1),
		result: &execute.MaterializationResult{
			TodoID: 21, TodoVersion: 4, TaskID: taskID, TaskVersion: 0,
		},
	}
	executor := &fakeTaskExecutor{calls: make(chan execute.ExecuteInput, 1)}
	coordinator, err := newCoordinator(extractor, materializer, &fakeExecutionStore{}, executor, pipelineTestOptions())
	if err != nil {
		t.Fatalf("newCoordinator() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := coordinator.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		cancel()
		coordinator.Wait()
	}()

	if err := coordinator.ChatScanned(ctx, capture.ChatScanResult{
		ChatID: "oc_1", InsertedCount: 1, HighWater: 100,
	}); err != nil {
		t.Fatalf("ChatScanned() error = %v", err)
	}
	select {
	case work := <-materializer.calls:
		if work.TodoID != 21 || work.Version != 3 {
			t.Fatalf("Todo materialization work = %#v", work)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Todo materialization was not triggered")
	}
	select {
	case input := <-executor.calls:
		if input.TaskID != taskID {
			t.Fatalf("M5 input = %#v", input)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("M5 was not triggered")
	}
}

func TestCoordinatorReconcileExecuteLoadsDurablePendingTasks(t *testing.T) {
	store := &fakeExecutionStore{pending: []domain.Task{{ID: 41, Version: 2, Status: "pending"}}}
	executor := &fakeTaskExecutor{calls: make(chan execute.ExecuteInput, 1)}
	coordinator, err := newCoordinator(nil, nil, store, executor, pipelineTestOptions())
	if err != nil {
		t.Fatalf("newCoordinator() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := coordinator.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		cancel()
		coordinator.Wait()
	}()

	if err := coordinator.ReconcileExecute(ctx); err != nil {
		t.Fatalf("ReconcileExecute() error = %v", err)
	}
	select {
	case input := <-executor.calls:
		if input.TaskID != 41 {
			t.Fatalf("M5 input = %#v", input)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending Task was not recovered")
	}
}

func TestCoordinatorDoesNotHotRetryFailedRealtimeTask(t *testing.T) {
	executor := &fakeTaskExecutor{calls: make(chan execute.ExecuteInput, 2), err: errors.New("claim failed")}
	coordinator, err := newCoordinator(nil, nil, &fakeExecutionStore{}, executor, pipelineTestOptions())
	if err != nil {
		t.Fatalf("newCoordinator() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := coordinator.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		cancel()
		coordinator.Wait()
	}()

	if err := coordinator.TaskReady(ctx, 51, 0); err != nil {
		t.Fatalf("TaskReady() error = %v", err)
	}
	select {
	case <-executor.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("M5 was not triggered")
	}
	select {
	case input := <-executor.calls:
		t.Fatalf("failed Task was retried immediately: %#v", input)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestKeyedQueueCoalescesWaitingWorkButAllowsInflightRetrigger(t *testing.T) {
	queue, err := newKeyedQueue(2, func(value int) string { return "same" })
	if err != nil {
		t.Fatalf("newKeyedQueue() error = %v", err)
	}
	ctx := context.Background()
	if err := queue.enqueue(ctx, 1); err != nil {
		t.Fatalf("first enqueue error = %v", err)
	}
	if err := queue.enqueue(ctx, 2); err != nil {
		t.Fatalf("duplicate enqueue error = %v", err)
	}
	if got := len(queue.items); got != 1 {
		t.Fatalf("queued items = %d, want 1", got)
	}
	item := <-queue.items
	queue.received(item)
	if err := queue.enqueue(ctx, 3); err != nil {
		t.Fatalf("inflight retrigger error = %v", err)
	}
	if got := len(queue.items); got != 1 {
		t.Fatalf("retriggered items = %d, want 1", got)
	}
}
