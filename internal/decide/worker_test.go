package decide

import (
	"context"
	"errors"
	"testing"

	"jarvis/internal/domain"
)

type fakeEvaluationSource struct {
	limit int
	todos []domain.Todo
	err   error
}

func (f *fakeEvaluationSource) LoadExtracted(_ context.Context, limit int) ([]domain.Todo, error) {
	f.limit = limit
	return append([]domain.Todo(nil), f.todos...), f.err
}

type fakeTodoEvaluator struct {
	calls []uint64
	errAt uint64
}

func (f *fakeTodoEvaluator) Evaluate(_ context.Context, todo *domain.Todo) (*EvaluationInput, error) {
	f.calls = append(f.calls, todo.ID)
	if todo.ID == f.errAt {
		return nil, errors.New("synthetic evaluation failure")
	}
	input := fixtureEvaluationInput()
	input.TodoID = todo.ID
	input.ExpectedVersion = todo.Version
	if todo.ID%2 == 0 {
		input.Route = RouteNeedInfo
		input.RouteReason = "missing_required_slot"
	}
	return &input, nil
}

type fakeEvaluationWriter struct {
	inputs []EvaluationInput
}

func (f *fakeEvaluationWriter) Apply(_ context.Context, input EvaluationInput) (*EvaluationResult, error) {
	f.inputs = append(f.inputs, input)
	return &EvaluationResult{TodoID: input.TodoID, Status: input.Route, Version: input.ExpectedVersion + 1}, nil
}

func TestDecisionWorkerEvaluateOnce(t *testing.T) {
	source := &fakeEvaluationSource{todos: []domain.Todo{
		{ID: 1, Status: "extracted", Version: 2},
		{ID: 2, Status: "extracted", Version: 0},
	}}
	evaluator := &fakeTodoEvaluator{}
	writer := &fakeEvaluationWriter{}
	worker, err := NewDecisionWorker(source, evaluator, writer, WorkerOptions{BatchLimit: 20})
	if err != nil {
		t.Fatalf("NewDecisionWorker() error = %v", err)
	}
	stats, err := worker.EvaluateOnce(context.Background())
	if err != nil {
		t.Fatalf("EvaluateOnce() error = %v", err)
	}
	if stats.Loaded != 2 || stats.Evaluated != 2 || stats.NeedInfo != 1 || stats.NeedDecision != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if source.limit != 20 || len(evaluator.calls) != 2 || len(writer.inputs) != 2 {
		t.Fatalf("limit=%d calls=%v writes=%d", source.limit, evaluator.calls, len(writer.inputs))
	}
}

func TestDecisionWorkerStopsOnFirstError(t *testing.T) {
	source := &fakeEvaluationSource{todos: []domain.Todo{
		{ID: 1, Status: "extracted"}, {ID: 2, Status: "extracted"}, {ID: 3, Status: "extracted"},
	}}
	evaluator := &fakeTodoEvaluator{errAt: 2}
	writer := &fakeEvaluationWriter{}
	worker, err := NewDecisionWorker(source, evaluator, writer, WorkerOptions{BatchLimit: 20})
	if err != nil {
		t.Fatalf("NewDecisionWorker() error = %v", err)
	}
	stats, err := worker.EvaluateOnce(context.Background())
	if err == nil {
		t.Fatal("EvaluateOnce() succeeded")
	}
	if stats.Evaluated != 1 || len(evaluator.calls) != 2 || len(writer.inputs) != 1 {
		t.Fatalf("stats=%#v calls=%v writes=%d", stats, evaluator.calls, len(writer.inputs))
	}
}

func TestDecisionWorkerRejectsSourceAndEvaluatorDrift(t *testing.T) {
	tests := []struct {
		name  string
		todos []domain.Todo
	}{
		{name: "zero id", todos: []domain.Todo{{Status: "extracted"}}},
		{name: "duplicate", todos: []domain.Todo{{ID: 1, Status: "extracted"}, {ID: 1, Status: "extracted"}}},
		{name: "wrong status", todos: []domain.Todo{{ID: 1, Status: "need_info"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			worker, err := NewDecisionWorker(&fakeEvaluationSource{todos: test.todos}, &fakeTodoEvaluator{}, &fakeEvaluationWriter{}, WorkerOptions{BatchLimit: 1})
			if err != nil {
				t.Fatalf("NewDecisionWorker() error = %v", err)
			}
			if _, err := worker.EvaluateOnce(context.Background()); err == nil {
				t.Fatal("EvaluateOnce() succeeded")
			}
		})
	}
}

func TestNewDecisionWorkerValidation(t *testing.T) {
	source := &fakeEvaluationSource{}
	evaluator := &fakeTodoEvaluator{}
	writer := &fakeEvaluationWriter{}
	for _, test := range []struct {
		source    evaluationSource
		evaluator todoEvaluator
		writer    evaluationWriter
		opts      WorkerOptions
	}{
		{evaluator: evaluator, writer: writer, opts: WorkerOptions{BatchLimit: 1}},
		{source: source, writer: writer, opts: WorkerOptions{BatchLimit: 1}},
		{source: source, evaluator: evaluator, opts: WorkerOptions{BatchLimit: 1}},
		{source: source, evaluator: evaluator, writer: writer},
	} {
		if _, err := NewDecisionWorker(test.source, test.evaluator, test.writer, test.opts); err == nil {
			t.Fatal("NewDecisionWorker() succeeded")
		}
	}
}
