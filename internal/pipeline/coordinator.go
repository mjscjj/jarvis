// Package pipeline accelerates the durable M2 -> M3 -> M4 -> M5 state machine.
// Notifications only wake downstream work; MySQL watermarks, statuses, and
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
	"jarvis/internal/decide"
	"jarvis/internal/domain"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
)

const queueCapacity = 1024

type extractor interface {
	ExtractChat(context.Context, string) (extract.WorkerStats, []extract.TodoRef, error)
	ExtractOnce(context.Context) (extract.WorkerStats, error)
}

type decider interface {
	EvaluateTodo(context.Context, uint64, int32) (*decide.EvaluationResult, error)
	EvaluateOnce(context.Context) (decide.WorkerStats, error)
}

type executionStore interface {
	LoadPending(context.Context, int) ([]domain.Task, error)
	FailStaleExecuting(context.Context, time.Duration, time.Time) (int, error)
}

type taskExecutor interface {
	Execute(context.Context, execute.ExecuteInput) (*execute.ExecuteResult, error)
}

type Options struct {
	ExecutionBatchLimit  int
	ExecutionConcurrency int
	StaleExecuting       time.Duration
	Logger               *log.Logger
}

type chatWork struct {
	ChatID string
	Marker string
	All    bool
}

type todoWork struct {
	TodoID  uint64
	Version int32
	All     bool
}

type taskWork struct {
	TaskID  uint64
	Version int32
}

// Coordinator owns every automatic M3/M4/M5 invocation, so real-time wake-ups
// and scheduled reconciliation cannot run separate copies of the same worker.
type Coordinator struct {
	extractor extractor
	decider   decider
	store     executionStore
	executor  taskExecutor
	opts      Options

	chats *keyedQueue[chatWork]
	todos *keyedQueue[todoWork]
	tasks *keyedQueue[taskWork]

	startMu sync.Mutex
	started bool
	wg      sync.WaitGroup
}

// NewCoordinator wires the concrete process workers. Keeping the public
// constructor concrete also avoids typed-nil interfaces accidentally enabling a
// disabled stage; tests use newCoordinator with small fakes.
func NewCoordinator(extractWorker *extract.Worker, decisionWorker *decide.DecisionWorker, executionTaskStore *execute.Store, agentExecutor *execute.AgentExecutor, opts Options) (*Coordinator, error) {
	var (
		extractStage extractor
		decideStage  decider
		storeStage   executionStore
		executeStage taskExecutor
	)
	if extractWorker != nil {
		extractStage = extractWorker
	}
	if decisionWorker != nil {
		decideStage = decisionWorker
	}
	if executionTaskStore != nil {
		storeStage = executionTaskStore
	}
	if agentExecutor != nil {
		executeStage = agentExecutor
	}
	return newCoordinator(extractStage, decideStage, storeStage, executeStage, opts)
}

func newCoordinator(extractor extractor, decider decider, store executionStore, executor taskExecutor, opts Options) (*Coordinator, error) {
	if opts.Logger == nil {
		return nil, fmt.Errorf("pipeline logger is nil")
	}
	if (store == nil) != (executor == nil) {
		return nil, fmt.Errorf("pipeline execution store and executor must be enabled together")
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
	todos, err := newKeyedQueue(queueCapacity, func(work todoWork) string {
		if work.All {
			return "all"
		}
		return strconv.FormatUint(work.TodoID, 10) + ":" + strconv.FormatInt(int64(work.Version), 10)
	})
	if err != nil {
		return nil, err
	}
	tasks, err := newKeyedQueue(queueCapacity, func(work taskWork) string {
		return strconv.FormatUint(work.TaskID, 10) + ":" + strconv.FormatInt(int64(work.Version), 10)
	})
	if err != nil {
		return nil, err
	}
	return &Coordinator{
		extractor: extractor, decider: decider, store: store, executor: executor, opts: opts,
		chats: chats, todos: todos, tasks: tasks,
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
		c.wg.Add(1)
		go c.runChats(ctx)
	}
	if c.decider != nil {
		c.wg.Add(1)
		go c.runTodos(ctx)
	}
	if c.executor != nil {
		for range c.opts.ExecutionConcurrency {
			c.wg.Add(1)
			go c.runTasks(ctx)
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
	return c.chats.enqueue(ctx, chatWork{ChatID: result.ChatID, Marker: marker})
}

// TodoReady and TaskReady implement decide.LifecycleNotifier.
func (c *Coordinator) TodoReady(ctx context.Context, todoID uint64, version int32) error {
	if c.decider == nil {
		return decide.ErrLifecycleStageDisabled
	}
	if todoID == 0 || version < 0 {
		return fmt.Errorf("pipeline Todo ID/version is invalid")
	}
	return c.todos.enqueue(ctx, todoWork{TodoID: todoID, Version: version})
}

func (c *Coordinator) TaskReady(ctx context.Context, taskID uint64, version int32) error {
	if c.executor == nil {
		return decide.ErrLifecycleStageDisabled
	}
	if taskID == 0 || version < 0 {
		return fmt.Errorf("pipeline Task ID/version is invalid")
	}
	return c.tasks.enqueue(ctx, taskWork{TaskID: taskID, Version: version})
}

func (c *Coordinator) ReconcileExtract(ctx context.Context) error {
	if c.extractor == nil {
		return nil
	}
	return c.chats.enqueue(ctx, chatWork{All: true})
}

func (c *Coordinator) ReconcileDecide(ctx context.Context) error {
	if c.decider == nil {
		return nil
	}
	return c.todos.enqueue(ctx, todoWork{All: true})
}

func (c *Coordinator) ReconcileExecute(ctx context.Context) error {
	if c.executor == nil {
		return nil
	}
	failed, err := c.store.FailStaleExecuting(ctx, c.opts.StaleExecuting, time.Now())
	if err != nil {
		return err
	}
	if failed > 0 {
		c.opts.Logger.Printf("stage=m5 trigger=reconcile stale_failed=%d", failed)
	}
	return c.enqueuePendingTasks(ctx)
}

func (c *Coordinator) ReconcileAll(ctx context.Context) error {
	if err := c.ReconcileExtract(ctx); err != nil {
		return err
	}
	if err := c.ReconcileDecide(ctx); err != nil {
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
	if work.All {
		for {
			stats, err := c.extractor.ExtractOnce(ctx)
			if err != nil {
				c.opts.Logger.Printf("stage=m3 trigger=reconcile status=error error=%v", err)
				return
			}
			if stats.ChatsLoaded == 0 {
				break
			}
			c.opts.Logger.Printf("stage=m3 trigger=reconcile status=ok chats=%d created=%d updated=%d", stats.ChatsProcessed, stats.Created, stats.Updated)
		}
		if err := c.ReconcileDecide(ctx); err != nil {
			c.opts.Logger.Printf("stage=m3 trigger=reconcile notify=m4 status=error error=%v", err)
		}
		return
	}
	for {
		stats, todos, err := c.extractor.ExtractChat(ctx, work.ChatID)
		if err != nil {
			c.opts.Logger.Printf("stage=m3 trigger=realtime chat_id=%s status=error error=%v", work.ChatID, err)
			return
		}
		if stats.ChatsLoaded == 0 {
			return
		}
		c.opts.Logger.Printf("stage=m3 trigger=realtime chat_id=%s status=ok created=%d updated=%d", work.ChatID, stats.Created, stats.Updated)
		if c.decider == nil {
			continue
		}
		for _, todo := range todos {
			if todo.Status != "extracted" {
				continue
			}
			if err := c.TodoReady(ctx, todo.ID, todo.Version); err != nil {
				c.opts.Logger.Printf("stage=m3 trigger=realtime notify=m4 todo_id=%d status=error error=%v", todo.ID, err)
			}
		}
	}
}

func (c *Coordinator) runTodos(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-c.todos.items:
			c.todos.received(work)
			c.processTodo(ctx, work)
		}
	}
}

func (c *Coordinator) processTodo(ctx context.Context, work todoWork) {
	if work.All {
		for {
			stats, err := c.decider.EvaluateOnce(ctx)
			if err != nil {
				c.opts.Logger.Printf("stage=m4 trigger=reconcile status=error error=%v", err)
				return
			}
			if stats.Loaded == 0 {
				break
			}
			c.opts.Logger.Printf("stage=m4 trigger=reconcile status=ok evaluated=%d auto=%d need_info=%d need_decision=%d dropped=%d", stats.Evaluated, stats.Auto, stats.NeedInfo, stats.NeedDecision, stats.Dropped)
			if stats.Auto > 0 {
				if err := c.ReconcileExecute(ctx); err != nil {
					c.opts.Logger.Printf("stage=m4 trigger=reconcile notify=m5 status=error error=%v", err)
				}
			}
		}
		return
	}
	result, err := c.decider.EvaluateTodo(ctx, work.TodoID, work.Version)
	if err != nil {
		if errors.Is(err, decide.ErrVersionConflict) || errors.Is(err, decide.ErrInvalidTransition) || errors.Is(err, decide.ErrTodoNotFound) {
			c.opts.Logger.Printf("stage=m4 trigger=realtime todo_id=%d version=%d status=stale", work.TodoID, work.Version)
			return
		}
		c.opts.Logger.Printf("stage=m4 trigger=realtime todo_id=%d version=%d status=error error=%v", work.TodoID, work.Version, err)
		return
	}
	c.opts.Logger.Printf("stage=m4 trigger=realtime todo_id=%d status=ok route=%s", result.TodoID, result.Status)
	if result.TaskID != nil && c.executor != nil {
		if err := c.TaskReady(ctx, *result.TaskID, result.TaskVersion); err != nil {
			c.opts.Logger.Printf("stage=m4 trigger=realtime notify=m5 task_id=%d status=error error=%v", *result.TaskID, err)
		}
	}
}

func (c *Coordinator) runTasks(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-c.tasks.items:
			c.tasks.received(work)
			result, err := c.executor.Execute(ctx, execute.ExecuteInput{TaskID: work.TaskID})
			if err != nil {
				if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) || errors.Is(err, execute.ErrTaskNotFound) {
					c.opts.Logger.Printf("stage=m5 trigger=queue task_id=%d version=%d status=stale", work.TaskID, work.Version)
				} else {
					c.opts.Logger.Printf("stage=m5 trigger=queue task_id=%d version=%d status=error error=%v", work.TaskID, work.Version, err)
				}
			} else if result == nil {
				c.opts.Logger.Printf("stage=m5 trigger=queue task_id=%d version=%d status=error error=nil_result", work.TaskID, work.Version)
			} else {
				c.opts.Logger.Printf("stage=m5 trigger=queue task_id=%d status=ok result=%s", work.TaskID, result.Status)
			}
		}
	}
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
