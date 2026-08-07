package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"jarvis/internal/extract/tools"
	"jarvis/internal/progress"
)

type fakePipelineStore struct {
	batches      []ChatBatch
	loadErr      error
	persistErr   error
	persistCalls int
	results      []UnitExtraction
	nextRunID    uint64
	runFinishes  []ExtractionRunFinish
	// persistedBatch is the batch PersistChat received, so a test can assert on
	// the unit state (including hydrated evidence) persistence actually reads.
	persistedBatch ChatBatch
	// chatMessages is the chat's full message history keyed by message_id, used
	// to answer LoadChatMessages when the model cites evidence outside the unit.
	chatMessages map[string]MessageContext
}

type fakeSystemPromptReader struct{}

func (fakeSystemPromptReader) Content(context.Context, string) (string, error) {
	return "fixture M3 system prompt\n{{WORK_RULES}}", nil
}

func (f *fakePipelineStore) PendingChatIDs(context.Context) ([]string, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	chatIDs := make([]string, 0, len(f.batches))
	for i := range f.batches {
		chatIDs = append(chatIDs, f.batches[i].Group.ChatID)
	}
	return chatIDs, nil
}

// extractAllChats drives the worker the way the coordinator does: enumerate the
// chats with pending work, then extract them one at a time. Extraction has no
// whole-pass entry point of its own — a pass is just a list of chats.
func extractAllChats(ctx context.Context, w *Worker) (WorkerStats, error) {
	chatIDs, err := w.PendingChatIDs(ctx)
	if err != nil {
		return WorkerStats{}, err
	}
	stats := WorkerStats{ChatsLoaded: len(chatIDs)}
	var firstErr error
	for _, chatID := range chatIDs {
		chatStats, _, err := w.ExtractChat(ctx, chatID)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		mergeWorkerStats(&stats, chatStats)
	}
	return stats, firstErr
}

func (f *fakePipelineStore) LoadPendingChat(_ context.Context, chatID string, _ LoadOptions) (*ChatBatch, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	for i := range f.batches {
		if f.batches[i].Group.ChatID == chatID {
			batch := f.batches[i]
			return &batch, nil
		}
	}
	return nil, nil
}

func (f *fakePipelineStore) LoadChatMessages(_ context.Context, _ string, messageIDs []string) ([]MessageContext, error) {
	found := make([]MessageContext, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		if message, ok := f.chatMessages[messageID]; ok {
			message.IsNew = false
			found = append(found, message)
		}
	}
	sort.Slice(found, func(i, j int) bool { return messageBefore(found[i], found[j]) })
	return found, nil
}

func (f *fakePipelineStore) StartExtractionRun(context.Context, string, time.Time) (uint64, error) {
	f.nextRunID++
	return f.nextRunID, nil
}

func (f *fakePipelineStore) FinishExtractionRun(_ context.Context, _ uint64, finish ExtractionRunFinish) error {
	f.runFinishes = append(f.runFinishes, finish)
	return nil
}

func (f *fakePipelineStore) PersistChat(_ context.Context, batch ChatBatch, results []UnitExtraction, _ string) (PersistStats, error) {
	f.persistCalls++
	f.results = results
	f.persistedBatch = batch
	if f.persistErr != nil {
		return PersistStats{}, f.persistErr
	}
	return PersistStats{Created: 2, Updated: 1, Todos: []TodoRef{{ID: 7, Version: 0, Status: "extracted"}}}, nil
}

type fakeModelExtractor struct {
	result  *ExtractionResult
	err     error
	prompts []Prompt
	boxes   []ToolBox
	// results, when non-empty, returns a distinct result per call index (clamped to
	// the last entry once exhausted), letting a test drive validation-feedback retry
	// where the first attempt returns a rewritten quote and a later one returns a
	// verbatim quote. When empty the extractor falls back to result/err.
	results []*ExtractionResult
	// errs pairs with results to drive format-feedback retry, where the first
	// call fails to decode and a later one returns a usable result.
	errs []error
}

func (f *fakeModelExtractor) ExtractWithTools(_ context.Context, prompt Prompt, box ToolBox) (*ExtractionResult, error) {
	f.prompts = append(f.prompts, prompt)
	f.boxes = append(f.boxes, box)
	idx := min(len(f.prompts)-1, max(len(f.results), len(f.errs))-1)
	err := f.err
	if len(f.errs) > 0 {
		err = f.errs[idx]
	}
	if len(f.results) > 0 {
		return f.results[idx], err
	}
	return f.result, err
}

// fakeToolBox is a no-op ToolBox for worker wiring tests.
type fakeToolBox struct{}

func (fakeToolBox) Specs() []tools.Spec { return nil }

func (fakeToolBox) Invoke(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// fakeToolBoxBuilder records the units it built a box for.
type fakeToolBoxBuilder struct {
	err   error
	built int
}

func (f *fakeToolBoxBuilder) Build(ChatBatch, ConversationUnit) (ToolBox, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.built++
	return fakeToolBox{}, nil
}

type fakeFactReader struct {
	filters []progress.FactFilter
	facts   []progress.FactView
	err     error
}

type fakeCandidateDeduplicator struct {
	inputs   []Candidate
	projects []*uint64
	err      error
}

func (f *fakeCandidateDeduplicator) Resolve(_ context.Context, candidate Candidate, projectID *uint64) (SemanticResolution, error) {
	f.inputs = append(f.inputs, candidate)
	f.projects = append(f.projects, projectID)
	if f.err != nil {
		return SemanticResolution{}, f.err
	}
	return SemanticResolution{Vector: []float32{1}}, nil
}

func (f *fakeFactReader) ListFacts(_ context.Context, filter progress.FactFilter) ([]progress.FactView, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return nil, f.err
	}
	return f.facts, nil
}

// fakeSharedMemoryReader 是共享记忆读取的打桩：text 为要注入的文本，err 非空则模拟读表失败。
type fakeSharedMemoryReader struct {
	text string
	err  error
}

func (f fakeSharedMemoryReader) Text(context.Context) (string, error) {
	return f.text, f.err
}

type fakeWorkRuleReader struct{}

func (fakeWorkRuleReader) Block(context.Context, string) (string, error) { return "", nil }

type fakeSkillReader struct{}

func (fakeSkillReader) Catalog(context.Context, string) (string, error) { return "", nil }

func TestWorkerPersistsWholeChat(t *testing.T) {
	projectID := uint64(9)
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group: GroupContext{ID: 1, ChatID: "oc_1", ProjectID: &projectID},
		Units: []ConversationUnit{{Key: "chat", Messages: []MessageContext{{
			MessageID: "om_1", Content: "连通性消息，无行动项", CreateTime: 1_700_000_000_000,
			IsNew: true, Extractable: true,
		}}}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true, CreateTime: 1_700_000_000_000},
	}}}
	model := &fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{}}}
	facts := &fakeFactReader{}
	toolBox := &fakeToolBoxBuilder{}
	worker, err := NewWorker(store, model, facts, &fakeCandidateDeduplicator{}, toolBox, fakeSharedMemoryReader{}, validWorkerOptions())
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	stats, err := extractAllChats(context.Background(), worker)
	if err != nil {
		t.Fatalf("extractAllChats() error = %v", err)
	}
	if stats.ChatsLoaded != 1 || stats.ChatsProcessed != 1 || stats.Units != 1 || stats.Created != 2 || stats.Updated != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if store.persistCalls != 1 || len(store.results) != 1 || len(model.prompts) != 1 {
		t.Fatalf("calls: persist=%d results=%d prompts=%d", store.persistCalls, len(store.results), len(model.prompts))
	}
	// Facts are read twice per subject (today detail + yesterday rollup), for the
	// group and its project — not once per unit.
	if len(facts.filters) != 4 {
		t.Fatalf("fact filters count = %d, want 4: %#v", len(facts.filters), facts.filters)
	}
	if facts.filters[0].SubjectType != "group" || facts.filters[0].SubjectID != 1 ||
		facts.filters[0].ExcludeSourceKind == nil || *facts.filters[0].ExcludeSourceKind != progress.FactSourceRollup {
		t.Fatalf("today group filter = %#v", facts.filters[0])
	}
	if facts.filters[1].SubjectType != "group" || facts.filters[1].SourceKind == nil ||
		*facts.filters[1].SourceKind != progress.FactSourceRollup || facts.filters[1].Limit != 1 {
		t.Fatalf("yesterday group rollup filter = %#v", facts.filters[1])
	}
	if facts.filters[2].SubjectType != "project" || facts.filters[2].SubjectID != projectID ||
		facts.filters[2].ExcludeSourceKind == nil {
		t.Fatalf("today project filter = %#v", facts.filters[2])
	}
	if facts.filters[3].SubjectType != "project" || facts.filters[3].SourceKind == nil ||
		*facts.filters[3].SourceKind != progress.FactSourceRollup {
		t.Fatalf("yesterday project rollup filter = %#v", facts.filters[3])
	}
	if toolBox.built != 1 || len(model.boxes) != 1 || model.boxes[0] == nil {
		t.Fatalf("tool box wiring: built=%d boxes=%d", toolBox.built, len(model.boxes))
	}
}

func TestWorkerExtractChatReturnsCommittedTodoRefs(t *testing.T) {
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group: GroupContext{ID: 1, ChatID: "oc_realtime"},
		Units: []ConversationUnit{{Key: "chat", Messages: []MessageContext{{
			MessageID: "om_1", Content: "请跟进这个问题", IsNew: true, Extractable: true,
		}}}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_realtime", IsNew: true},
	}}}
	worker, err := NewWorker(
		store,
		&fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{}}},
		&fakeFactReader{},
		&fakeCandidateDeduplicator{},
		&fakeToolBoxBuilder{},
		fakeSharedMemoryReader{},
		validWorkerOptions(),
	)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}

	stats, todos, err := worker.ExtractChat(context.Background(), "oc_realtime")
	if err != nil {
		t.Fatalf("ExtractChat() error = %v", err)
	}
	if stats.ChatsLoaded != 1 || stats.ChatsProcessed != 1 || stats.Created != 2 || stats.Updated != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if len(todos) != 1 || todos[0].ID != 7 || todos[0].Version != 0 || todos[0].Status != "extracted" {
		t.Fatalf("todos = %#v", todos)
	}
}

func TestWorkerExtractChatSkipsChatWithoutPendingMessages(t *testing.T) {
	worker, err := NewWorker(
		&fakePipelineStore{},
		&fakeModelExtractor{},
		&fakeFactReader{},
		&fakeCandidateDeduplicator{},
		&fakeToolBoxBuilder{},
		fakeSharedMemoryReader{},
		validWorkerOptions(),
	)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	stats, todos, err := worker.ExtractChat(context.Background(), "oc_idle")
	if err != nil {
		t.Fatalf("ExtractChat() error = %v", err)
	}
	if stats != (WorkerStats{}) || len(todos) != 0 {
		t.Fatalf("stats=%#v todos=%#v", stats, todos)
	}
}

func TestWorkerDoesNotAdvanceWatermarkAfterModelFailure(t *testing.T) {
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group: GroupContext{ID: 1, ChatID: "oc_1"},
		Units: []ConversationUnit{{Key: "chat", Messages: []MessageContext{{
			MessageID: "om_1", Content: "请跟进", IsNew: true, Extractable: true,
		}}}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true},
	}}}
	model := &fakeModelExtractor{err: errors.New("model unavailable")}
	worker, err := NewWorker(store, model, &fakeFactReader{}, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, validWorkerOptions())
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := extractAllChats(context.Background(), worker); err == nil {
		t.Fatal("extractAllChats() accepted model failure")
	}
	if store.persistCalls != 0 {
		t.Fatalf("PersistChat() calls = %d, want 0", store.persistCalls)
	}
}

func TestWorkerDoesNotPersistAfterSemanticDedupFailure(t *testing.T) {
	candidate := strictCandidate()
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group: GroupContext{ID: 1, ChatID: "oc_1"},
		Units: []ConversationUnit{{
			Key: "chat",
			Messages: []MessageContext{{
				MessageID: "om_1", ChatID: "oc_1", Content: candidate.SourceQuote,
				CreateTime: 1_700_000_000_000, IsNew: true, Extractable: true,
			}},
		}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true, CreateTime: 1_700_000_000_000},
	}}}
	dedup := &fakeCandidateDeduplicator{err: errors.New("qdrant unavailable")}
	worker, err := NewWorker(
		store,
		&fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{candidate}}},
		&fakeFactReader{}, dedup, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, validWorkerOptions(),
	)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := extractAllChats(context.Background(), worker); err == nil || !strings.Contains(err.Error(), "qdrant unavailable") {
		t.Fatalf("extractAllChats() error = %v", err)
	}
	if store.persistCalls != 0 || len(dedup.inputs) != 1 {
		t.Fatalf("persistCalls=%d dedup.inputs=%d", store.persistCalls, len(dedup.inputs))
	}
}

// TestWorkerDedupsWithinResolvedProjectScope pins the project scope used for
// semantic dedup to the one persistence derives. The group is unbound, so a
// candidate whose project_hint matches a known project must be searched inside
// that project's partition — searching the group binding (nil) instead would
// match a project-less Todo that persistence then rejects as a domain change.
func TestWorkerDedupsWithinResolvedProjectScope(t *testing.T) {
	candidate := strictCandidate()
	hint := "jarvis"
	candidate.ProjectHint = &hint
	store := &fakePipelineStore{batches: []ChatBatch{{
		Group:         GroupContext{ID: 1, ChatID: "oc_1"}, // unbound
		OtherProjects: []OtherProjectContext{{ID: 42, Code: "jarvis", Name: "Jarvis"}},
		Units: []ConversationUnit{{
			Key: "chat",
			Messages: []MessageContext{{
				MessageID: "om_1", ChatID: "oc_1", Content: candidate.SourceQuote,
				CreateTime: 1_700_000_000_000, IsNew: true, Extractable: true,
			}},
		}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true, CreateTime: 1_700_000_000_000},
	}}}
	dedup := &fakeCandidateDeduplicator{}
	worker, err := NewWorker(
		store,
		&fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{candidate}}},
		&fakeFactReader{}, dedup, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, validWorkerOptions(),
	)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := extractAllChats(context.Background(), worker); err != nil {
		t.Fatalf("extractAllChats() error = %v", err)
	}
	if len(dedup.projects) != 1 {
		t.Fatalf("dedup.projects=%d, want 1", len(dedup.projects))
	}
	if dedup.projects[0] == nil || *dedup.projects[0] != 42 {
		t.Fatalf("dedup project scope = %v, want 42", dedup.projects[0])
	}
}

// retryBatch is a single-unit batch whose [new] message content deliberately
// interleaves fragments ("看下 ... 当前服务和架构梳理 ...") so a spliced quote fails
// the verbatim check while a contiguous substring passes.
func retryBatch() ChatBatch {
	const newContent = "todo：看下自建agent loop的模型接入层和流式输出，看下feishu写入没有权限的问题，以及当前服务和架构梳理，以及多机房支持"
	return ChatBatch{
		Group: GroupContext{ID: 1, ChatID: "oc_1"},
		Units: []ConversationUnit{{
			Key: "chat",
			Messages: []MessageContext{{
				MessageID: "om_1", ChatID: "oc_1", Content: newContent,
				CreateTime: 1_700_000_000_000, IsNew: true, Extractable: true,
			}},
			Participants: []ParticipantContext{{OpenID: "ou_owner", Name: "Me"}},
		}},
		LastNew: MessageContext{MessageID: "om_1", ChatID: "oc_1", IsNew: true, CreateTime: 1_700_000_000_000},
	}
}

func retryCandidate(quote string) Candidate {
	return Candidate{
		ActionType: "investigate", Status: "extracted", Title: "梳理架构", Target: "当前服务和架构梳理",
		Payload: "产出一份当前服务与架构的梳理结论。", SourceMessageIDs: []string{"om_1"}, SourceQuote: quote,
	}
}

func TestWorkerRetriesOnQuoteMismatchThenSucceeds(t *testing.T) {
	store := &fakePipelineStore{batches: []ChatBatch{retryBatch()}}
	rewritten := retryCandidate("看下当前服务和架构梳理") // spliced, not contiguous in 原文
	verbatim := retryCandidate("当前服务和架构梳理")    // contiguous substring of 原文
	model := &fakeModelExtractor{results: []*ExtractionResult{
		{Candidates: []Candidate{rewritten}},
		{Candidates: []Candidate{verbatim}},
	}}
	opts := validWorkerOptions()
	opts.EvidenceRetryMax = 2
	worker, err := NewWorker(store, model, &fakeFactReader{}, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, opts)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	stats, err := extractAllChats(context.Background(), worker)
	if err != nil {
		t.Fatalf("extractAllChats() error = %v", err)
	}
	if len(model.prompts) != 2 {
		t.Fatalf("extract calls = %d, want 2", len(model.prompts))
	}
	if store.persistCalls != 1 || stats.Units != 1 || stats.Candidates != 1 {
		t.Fatalf("persistCalls=%d stats=%#v", store.persistCalls, stats)
	}
	// The first prompt must be the plain prompt; the second must carry the feedback
	// block with both the mismatch explanation and the cited 原文.
	if strings.Contains(model.prompts[0].User, "上一轮抽取校验未通过") {
		t.Fatalf("first prompt unexpectedly carried feedback: %q", model.prompts[0].User)
	}
	second := model.prompts[1].User
	for _, want := range []string{"上一轮抽取校验未通过", "逐字连续复制", "看下当前服务和架构梳理", "当前服务和架构梳理，以及多机房支持"} {
		if !strings.Contains(second, want) {
			t.Fatalf("retry prompt missing %q; got %q", want, second)
		}
	}
}

// TestWorkerRetriesUnparseableFinalMessage pins the cheapest recovery M3 has: a
// run that read the whole chat and ran tools for minutes, then ended on prose
// instead of JSON, must be asked to restate itself rather than thrown away.
func TestWorkerRetriesUnparseableFinalMessage(t *testing.T) {
	store := &fakePipelineStore{batches: []ChatBatch{retryBatch()}}
	decodeErr := fmt.Errorf("decode codex extraction result: %w", ErrInvalidExtraction)
	model := &fakeModelExtractor{
		results: []*ExtractionResult{nil, {Candidates: []Candidate{retryCandidate("当前服务和架构梳理")}}},
		errs:    []error{decodeErr, nil},
	}
	opts := validWorkerOptions()
	opts.EvidenceRetryMax = 2
	worker, err := NewWorker(store, model, &fakeFactReader{}, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, opts)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := extractAllChats(context.Background(), worker); err != nil {
		t.Fatalf("extractAllChats() error = %v", err)
	}
	if len(model.prompts) != 2 {
		t.Fatalf("extract calls = %d, want 2", len(model.prompts))
	}
	if retry := model.prompts[1].User; !strings.Contains(retry, "最终消息没能解析") {
		t.Fatalf("retry prompt missing format feedback; got %q", retry)
	}
	if store.persistCalls != 1 {
		t.Fatalf("persistCalls = %d, want 1", store.persistCalls)
	}
}

func TestWorkerFailsAfterExhaustingEvidenceRetries(t *testing.T) {
	store := &fakePipelineStore{batches: []ChatBatch{retryBatch()}}
	rewritten := retryCandidate("看下当前服务和架构梳理")
	model := &fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{rewritten}}}
	opts := validWorkerOptions()
	opts.EvidenceRetryMax = 2
	worker, err := NewWorker(store, model, &fakeFactReader{}, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, opts)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	_, err = extractAllChats(context.Background(), worker)
	if err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("extractAllChats() error = %v, want exhausted evidence retries", err)
	}
	if len(model.prompts) != 3 { // initial + 2 retries
		t.Fatalf("extract calls = %d, want 3", len(model.prompts))
	}
	if store.persistCalls != 0 {
		t.Fatalf("persistCalls = %d, want 0", store.persistCalls)
	}
}

// 模型带着工具读整个群，引用本 unit 之外但群里真实存在的消息（机器人回复、
// 兄弟话题、批次加载后才到的消息）是正常的。这类引用必须补进 unit，否则落库时
// 取不到消息，first_evidence_at 会退化成 1970，快照也会丢掉这条证据。
func TestWorkerHydratesCitedMessageFromOutsideUnit(t *testing.T) {
	const botReply = "已安排袁昕钰跟进该问题"
	batch := retryBatch()
	store := &fakePipelineStore{
		batches: []ChatBatch{batch},
		chatMessages: map[string]MessageContext{"om_bot": {
			DatabaseID: 42, MessageID: "om_bot", ChatID: "oc_1", SenderType: "bot",
			SenderName: "Pulse", Content: botReply, CreateTime: 1_700_000_300_000,
		}},
	}
	candidate := retryCandidate("当前服务和架构梳理")
	candidate.SourceMessageIDs = []string{"om_1", "om_bot"}
	model := &fakeModelExtractor{result: &ExtractionResult{Candidates: []Candidate{candidate}}}
	worker, err := NewWorker(store, model, &fakeFactReader{}, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, validWorkerOptions())
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := extractAllChats(context.Background(), worker); err != nil {
		t.Fatalf("extractAllChats() error = %v", err)
	}
	if len(model.prompts) != 1 {
		t.Fatalf("extract calls = %d, want 1 (no retry needed)", len(model.prompts))
	}
	if store.persistCalls != 1 {
		t.Fatalf("persistCalls = %d, want 1", store.persistCalls)
	}
	// PersistChat 按 key 从 batch.Units 重新取 unit，补进去的消息必须在那里，
	// 并且落在正确的时间顺序上（快照按尾部截断）。
	persisted := store.persistedBatch.Units[0].Messages
	if len(persisted) != 2 || persisted[0].MessageID != "om_1" || persisted[1].MessageID != "om_bot" {
		t.Fatalf("hydrated unit messages = %#v", persisted)
	}
	if persisted[1].IsNew {
		t.Fatalf("hydrated message must be context, not [new]: %#v", persisted[1])
	}
}

// 引用了群里根本不存在的 message_id（模型编的），走和 quote 对不上一样的反馈重试，
// 不再整批 fail-fast。
func TestWorkerRetriesOnInventedMessageIDThenSucceeds(t *testing.T) {
	store := &fakePipelineStore{batches: []ChatBatch{retryBatch()}}
	invented := retryCandidate("当前服务和架构梳理")
	invented.SourceMessageIDs = []string{"om_does_not_exist"}
	model := &fakeModelExtractor{results: []*ExtractionResult{
		{Candidates: []Candidate{invented}},
		{Candidates: []Candidate{retryCandidate("当前服务和架构梳理")}},
	}}
	opts := validWorkerOptions()
	opts.EvidenceRetryMax = 2
	worker, err := NewWorker(store, model, &fakeFactReader{}, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, fakeSharedMemoryReader{}, opts)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := extractAllChats(context.Background(), worker); err != nil {
		t.Fatalf("extractAllChats() error = %v", err)
	}
	if len(model.prompts) != 2 {
		t.Fatalf("extract calls = %d, want 2", len(model.prompts))
	}
	if store.persistCalls != 1 {
		t.Fatalf("persistCalls = %d, want 1", store.persistCalls)
	}
	second := model.prompts[1].User
	for _, want := range []string{"上一轮抽取校验未通过", "om_does_not_exist", "真实存在的消息 id"} {
		if !strings.Contains(second, want) {
			t.Fatalf("retry prompt missing %q; got %q", want, second)
		}
	}
}

func TestValidateCandidateEvidenceQuoteMismatchIncludesSourceText(t *testing.T) {
	unit := retryBatch().Units[0]
	candidate := retryCandidate("看下当前服务和架构梳理")
	err := validateCandidateEvidence(unit, &candidate)
	if err == nil || !errors.Is(err, ErrEvidenceQuoteMismatch) {
		t.Fatalf("validateCandidateEvidence() error = %v, want ErrEvidenceQuoteMismatch", err)
	}
	for _, want := range []string{"om_1", "看下当前服务和架构梳理", "当前服务和架构梳理，以及多机房支持"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("evidence error missing %q; got %q", want, err.Error())
		}
	}
}

// 交办人等语义可以直接写入 payload；证据校验只检查机器消费的消息 ID 和原文。
func TestValidateCandidateEvidenceIgnoresOpaquePayload(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "clue:feishu_meeting:m1", Source: "clue", SenderOpenID: "__clue__",
		Content: "会议《周会》已结束\n张三负责补齐测试", IsNew: true, Extractable: true,
	}}, Participants: []ParticipantContext{{OpenID: "__clue__", Name: "feishu_meeting"}}}
	candidate := Candidate{
		ActionType: "investigate", Status: "extracted", Title: "补齐测试", Target: "测试",
		Payload:          "张三（ou_zhangsan）负责补齐测试并确保通过。",
		SourceMessageIDs: []string{"clue:feishu_meeting:m1"}, SourceQuote: "张三负责补齐测试",
	}
	if err := validateCandidateEvidence(unit, &candidate); err != nil {
		t.Fatalf("validateCandidateEvidence() error = %v, want nil", err)
	}
}

func validWorkerOptions() WorkerOptions {
	return WorkerOptions{
		Load: LoadOptions{
			BatchMessages: 100, ContextMessages: 20, ContextWindow: 2 * time.Hour,
			OpenTodoLimit: 50, RecentTaskLimit: 10,
		},
		PrincipalOpenID: "ou_owner", ModelName: "model", FactLimit: 10, KeyPersonLimit: 5,
		MaxPromptChars: 60_000, Location: time.UTC,
		WorkRules:     fakeWorkRuleReader{},
		Skills:        fakeSkillReader{},
		SystemPrompts: fakeSystemPromptReader{},
	}
}
