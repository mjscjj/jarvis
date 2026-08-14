// Package pipeline accelerates the durable M2 -> M3 -> M5 state machine.
// Notifications only wake downstream work; SQLite watermarks, statuses, and
// optimistic versions remain the source of truth and scheduled reconciliation
// repairs any wake-up lost during a crash.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"jarvis/internal/capture"
	"jarvis/internal/domain"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/observability"
)

const queueCapacity = 1024

type extractor interface {
	ExtractChat(context.Context, string) (extract.WorkerStats, []extract.TodoRef, error)
	PendingChatIDs(context.Context) ([]string, error)
}

type todoMaterializer interface {
	MaterializeTodo(context.Context, uint64, int32) (*execute.MaterializationResult, error)
	MaterializeOnce(context.Context) (execute.MaterializationStats, error)
}

type executionStore interface {
	LoadPending(context.Context, int) ([]domain.Task, error)
	FailStaleExecuting(context.Context, time.Duration, time.Time) (execute.StaleSweep, error)
}

type taskExecutor interface {
	Execute(context.Context, execute.ExecuteInput) (*execute.ExecuteResult, error)
}

type Options struct {
	ExtractConcurrency   int
	ExecutionBatchLimit  int
	ExecutionConcurrency int
	StaleExecuting       time.Duration
	Logger               *log.Logger
}

// reconcileMarker is the Marker every reconciliation-scheduled chat carries. It
// is a constant so repeated ticks over a chat that is already queued collapse
// into one item instead of piling up.
const reconcileMarker = "reconcile"

type chatWork struct {
	ChatID string
	Marker string
	LogID  string
	All    bool
}

func (w chatWork) trigger() string {
	if w.Marker == reconcileMarker {
		return "reconcile"
	}
	return "realtime"
}

// m5Work is one unit of M5 work. A Todo is mechanically materialized before its
// Task enters execution. Both use one queue and worker pool.
type m5Work struct {
	TodoID  uint64
	TaskID  uint64
	Version int32
	LogID   string
}

// Coordinator owns every automatic M3/M5 invocation, so real-time wake-ups and
// scheduled reconciliation cannot run separate copies of the same worker.
type Coordinator struct {
	extractor    extractor
	materializer todoMaterializer
	store        executionStore
	executor     taskExecutor
	opts         Options

	chats *keyedQueue[chatWork]
	m5    *keyedQueue[m5Work]

	// chatLocks is the only mutual exclusion extraction needs. Two runs of the
	// same chat would race on its watermark, so they serialize; different chats
	// share nothing and run freely. Scheduled reconciliation goes through the
	// same locks, so a chat that takes ten minutes delays that chat alone.
	chatLocks *keyedLocker

	startMu sync.Mutex
	started bool
	wg      sync.WaitGroup
}

// NewCoordinator wires the concrete process workers. Keeping the public
// constructor concrete also avoids typed-nil interfaces accidentally enabling a
// disabled stage; tests use newCoordinator with small fakes.
func NewCoordinator(extractWorker *extract.Worker, materializer *execute.Materializer, executionTaskStore *execute.Store, agentExecutor *execute.AgentExecutor, opts Options) (*Coordinator, error) {
	var (
		extractStage     extractor
		materializeStage todoMaterializer
		storeStage       executionStore
		executeStage     taskExecutor
	)
	if extractWorker != nil {
		extractStage = extractWorker
	}
	if materializer != nil {
		materializeStage = materializer
	}
	if executionTaskStore != nil {
		storeStage = executionTaskStore
	}
	if agentExecutor != nil {
		executeStage = agentExecutor
	}
	return newCoordinator(extractStage, materializeStage, storeStage, executeStage, opts)
}

func newCoordinator(extractor extractor, materializer todoMaterializer, store executionStore, executor taskExecutor, opts Options) (*Coordinator, error) {
	if opts.Logger == nil {
		return nil, fmt.Errorf("pipeline logger is nil")
	}
	if (store == nil) != (executor == nil) {
		return nil, fmt.Errorf("pipeline execution store and executor must be enabled together")
	}
	if extractor != nil && opts.ExtractConcurrency <= 0 {
		return nil, fmt.Errorf("pipeline extract concurrency must be positive")
	}
	if executor != nil {
		if opts.ExecutionBatchLimit <= 0 {
			return nil, fmt.Errorf("pipeline execution batch limit must be positive")
		}
		if opts.ExecutionConcurrency <= 0 {
			return nil, fmt.Errorf("pipeline execution concurrency must be positive")
		}
		if opts.StaleExecuting <= 0 {
			return nil, fmt.Errorf("pipeline stale executing threshold must be positive")
		}
	}
	chats, err := newKeyedQueue(queueCapacity, func(work chatWork) string {
		if work.All {
			return "all"
		}
		return work.ChatID + ":" + work.Marker
	})
	if err != nil {
		return nil, err
	}
	m5, err := newKeyedQueue(queueCapacity, func(work m5Work) string {
		version := strconv.FormatInt(int64(work.Version), 10)
		switch {
		case work.TodoID != 0:
			return "todo:" + strconv.FormatUint(work.TodoID, 10) + ":" + version
		default:
			return "task:" + strconv.FormatUint(work.TaskID, 10) + ":" + version
		}
	})
	if err != nil {
		return nil, err
	}
	return &Coordinator{
		extractor: extractor, materializer: materializer, store: store, executor: executor, opts: opts,
		chats: chats, m5: m5, chatLocks: newKeyedLocker(),
	}, nil
}

func (c *Coordinator) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("pipeline context is nil")
	}
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return fmt.Errorf("pipeline coordinator is already started")
	}
	c.started = true
	if c.extractor != nil {
		for range c.opts.ExtractConcurrency {
			c.wg.Add(1)
			go c.runChats(ctx)
		}
	}
	// One pool drains mechanical Todo materialization and M5 execution.
	if c.materializer != nil || c.executor != nil {
		workers := 1
		if c.executor != nil {
			workers = c.opts.ExecutionConcurrency
		}
		for range workers {
			c.wg.Add(1)
			go c.runM5(ctx)
		}
	}
	return nil
}

func (c *Coordinator) Wait() { c.wg.Wait() }

// ChatScanned implements capture.ScanObserver. M2 invokes it only after the chat
// and checkpoint commit; the high-water marker distinguishes later scans of the
// same chat while exact duplicate notifications coalesce.
func (c *Coordinator) ChatScanned(ctx context.Context, result capture.ChatScanResult) error {
	if c.extractor == nil || result.InsertedCount == 0 {
		return nil
	}
	marker := strconv.FormatInt(result.HighWater, 10)
	if result.LastMessageID != nil && strings.TrimSpace(*result.LastMessageID) != "" {
		marker += ":" + *result.LastMessageID
	}
	ctx = observability.EnsureLogID(ctx)
	return c.chats.enqueue(ctx, chatWork{ChatID: result.ChatID, Marker: marker, LogID: observability.LogID(ctx)})
}

// TodoReady and TaskReady implement execute.LifecycleNotifier.
func (c *Coordinator) TodoReady(ctx context.Context, todoID uint64, version int32) error {
	if c.materializer == nil {
		return execute.ErrLifecycleStageDisabled
	}
	if todoID == 0 || version < 0 {
		return fmt.Errorf("pipeline Todo ID/version is invalid")
	}
	ctx = observability.EnsureLogID(ctx)
	return c.m5.enqueue(ctx, m5Work{TodoID: todoID, Version: version, LogID: observability.LogID(ctx)})
}

func (c *Coordinator) TaskReady(ctx context.Context, taskID uint64, version int32) error {
	if c.executor == nil {
		return execute.ErrLifecycleStageDisabled
	}
	if taskID == 0 || version < 0 {
		return fmt.Errorf("pipeline Task ID/version is invalid")
	}
	ctx = observability.EnsureLogID(ctx)
	return c.m5.enqueue(ctx, m5Work{TaskID: taskID, Version: version, LogID: observability.LogID(ctx)})
}

func (c *Coordinator) ReconcileExtract(ctx context.Context) error {
	if c.extractor == nil {
		return nil
	}
	ctx = observability.EnsureLogID(ctx)
	return c.chats.enqueue(ctx, chatWork{All: true, LogID: observability.LogID(ctx)})
}

// ReconcileExecute runs every compensation step even when an earlier one fails.
// Materialization used to abort the whole round, so a single Todo that cannot
// become a Task also stopped the stale-executing sweep and the pending-Task
// requeue — two recoveries that have nothing to do with that Todo. Failures are
// collected and returned together instead.
func (c *Coordinator) ReconcileExecute(ctx context.Context) error {
	var errs []error
	if c.materializer != nil {
		// Loop on progress, not on backlog: Todos that failed stay `extracted`
		// and would be loaded again forever.
		for {
			stats, err := c.materializer.MaterializeOnce(ctx)
			if err != nil {
				errs = append(errs, err)
			}
			if stats.Materialized > 0 {
				c.logf(ctx, "stage=m5 step=materialize trigger=reconcile status=ok materialized=%d failed=%d", stats.Materialized, stats.Failed)
				continue
			}
			if stats.Failed > 0 {
				c.logf(ctx, "stage=m5 step=materialize trigger=reconcile status=error loaded=%d failed=%d", stats.Loaded, stats.Failed)
			}
			break
		}
	}
	if c.executor == nil {
		return errors.Join(errs...)
	}
	sweep, err := c.store.FailStaleExecuting(ctx, c.opts.StaleExecuting, time.Now())
	if err != nil {
		errs = append(errs, err)
	} else if sweep.Failed > 0 || sweep.Requeued > 0 {
		c.logf(ctx, "stage=m5 step=execute trigger=reconcile stale_failed=%d stale_requeued=%d", sweep.Failed, sweep.Requeued)
	}
	if err := c.enqueuePendingTasks(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (c *Coordinator) ReconcileAll(ctx context.Context) error {
	if err := c.ReconcileExtract(ctx); err != nil {
		return err
	}
	return c.ReconcileExecute(ctx)
}

func (c *Coordinator) runChats(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-c.chats.items:
			c.chats.received(work)
			c.processChat(ctx, work)
		}
	}
}

func (c *Coordinator) processChat(ctx context.Context, work chatWork) {
	ctx = observability.WithLogID(ctx, work.LogID)
	if work.All {
		c.fanOutPendingChats(ctx)
		if err := c.ReconcileExecute(ctx); err != nil {
			c.logf(ctx, "stage=m3 trigger=reconcile notify=m5 status=error error=%+v", err)
		}
		return
	}
	unlock, err := c.chatLocks.lock(work.ChatID)
	if err != nil {
		c.logf(ctx, "stage=m3 trigger=%s chat_id=%s status=error error=%+v", work.trigger(), work.ChatID, err)
		return
	}
	defer unlock()
	for {
		stats, todos, err := c.extractor.ExtractChat(ctx, work.ChatID)
		if err != nil {
			c.logf(ctx, "stage=m3 trigger=%s chat_id=%s status=error error=%+v", work.trigger(), work.ChatID, err)
			return
		}
		if stats.ChatsLoaded == 0 {
			return
		}
		c.logf(ctx, "stage=m3 trigger=%s chat_id=%s status=ok created=%d updated=%d", work.trigger(), work.ChatID, stats.Created, stats.Updated)
		if c.materializer == nil {
			continue
		}
		for _, todo := range todos {
			if todo.Status != "extracted" {
				continue
			}
			if err := c.TodoReady(ctx, todo.ID, todo.Version); err != nil {
				c.logf(ctx, "stage=m3 trigger=%s notify=m5 todo_id=%d status=error error=%+v", work.trigger(), todo.ID, err)
			}
		}
	}
}

// fanOutPendingChats turns one reconciliation tick into per-chat work items on
// the same queue real-time wake-ups use. Reconciliation deliberately extracts
// nothing itself: a round that held the whole stage while it worked through
// every chat meant one slow chat blocked every incoming message behind it.
func (c *Coordinator) fanOutPendingChats(ctx context.Context) {
	chatIDs, err := c.extractor.PendingChatIDs(ctx)
	if err != nil {
		c.logf(ctx, "stage=m3 trigger=reconcile status=error error=%+v", err)
		return
	}
	queued := 0
	for _, chatID := range chatIDs {
		work := chatWork{ChatID: chatID, Marker: reconcileMarker, LogID: observability.LogID(ctx)}
		if err := c.chats.enqueue(ctx, work); err != nil {
			c.logf(ctx, "stage=m3 trigger=reconcile chat_id=%s status=error error=%+v", chatID, err)
			continue
		}
		queued++
	}
	c.logf(ctx, "stage=m3 trigger=reconcile status=ok pending_chats=%d queued=%d", len(chatIDs), queued)
}

func (c *Coordinator) runM5(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-c.m5.items:
			c.m5.received(work)
			c.processM5(ctx, work)
		}
	}
}

func (c *Coordinator) processM5(ctx context.Context, work m5Work) {
	ctx = observability.WithLogID(ctx, work.LogID)
	switch {
	case work.TodoID != 0:
		c.materializeTodo(ctx, work)
	default:
		c.executeTask(ctx, work)
	}
}

func (c *Coordinator) materializeTodo(ctx context.Context, work m5Work) {
	result, err := c.materializer.MaterializeTodo(ctx, work.TodoID, work.Version)
	if err != nil {
		if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) || errors.Is(err, execute.ErrTodoNotFound) {
			c.logf(ctx, "stage=m5 step=materialize trigger=realtime todo_id=%d version=%d status=stale", work.TodoID, work.Version)
			return
		}
		c.logf(ctx, "stage=m5 step=materialize trigger=realtime todo_id=%d version=%d status=error error=%+v", work.TodoID, work.Version, err)
		return
	}
	c.logf(ctx, "stage=m5 step=materialize trigger=realtime todo_id=%d status=ok task_id=%d", result.TodoID, result.TaskID)
	if c.executor != nil {
		if err := c.TaskReady(ctx, result.TaskID, result.TaskVersion); err != nil {
			c.logf(ctx, "stage=m5 step=materialize trigger=realtime notify=execute task_id=%d status=error error=%+v", result.TaskID, err)
		}
	}
}

func (c *Coordinator) executeTask(ctx context.Context, work m5Work) {
	result, err := c.executor.Execute(ctx, execute.ExecuteInput{TaskID: work.TaskID})
	if err != nil {
		if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) || errors.Is(err, execute.ErrTaskNotFound) {
			c.logf(ctx, "stage=m5 step=execute trigger=queue task_id=%d version=%d status=stale", work.TaskID, work.Version)
			return
		}
		c.logf(ctx, "stage=m5 step=execute trigger=queue task_id=%d version=%d status=error error=%+v", work.TaskID, work.Version, err)
		return
	}
	if result == nil {
		c.logf(ctx, "stage=m5 step=execute trigger=queue task_id=%d version=%d status=error error=nil_result", work.TaskID, work.Version)
		return
	}
	c.logf(ctx, "stage=m5 step=execute trigger=queue task_id=%d status=ok result=%s", work.TaskID, result.Status)
}

func (c *Coordinator) logf(ctx context.Context, format string, args ...any) {
	ctx = observability.EnsureLogID(ctx)
	c.opts.Logger.Printf("logid=%s "+format, append([]any{observability.LogID(ctx)}, args...)...)
}

func (c *Coordinator) enqueuePendingTasks(ctx context.Context) error {
	tasks, err := c.store.LoadPending(ctx, c.opts.ExecutionBatchLimit)
	if err != nil {
		return err
	}
	for i := range tasks {
		if err := c.TaskReady(ctx, tasks[i].ID, tasks[i].Version); err != nil {
			return err
		}
	}
	return nil
}
