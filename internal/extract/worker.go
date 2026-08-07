package extract

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/agentusage"
	"jarvis/internal/contextsnap"
	"jarvis/internal/progress"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
	"jarvis/internal/workrule"
)

// factReader is progress.Service. M3 reads the facts the offline engine has
// already distilled instead of retrieving raw conversation memories: a fact is
// a settled conclusion bound to a subject, which is what deciding "is this clue
// new" actually needs.
type factReader interface {
	ListFacts(context.Context, progress.FactFilter) ([]progress.FactView, error)
}

type WorkerOptions struct {
	Load            LoadOptions
	PrincipalOpenID string
	ModelName       string
	// FactLimit caps how many of a subject's *today* detail facts (excluding
	// rollups) are injected per extraction prompt. Each subject also gets at
	// most one previous-day rollup on top of this.
	FactLimit int
	// KeyPersonLimit caps how many person subjects contribute facts.
	KeyPersonLimit int
	MaxPromptChars int
	Location       *time.Location
	// AgentToolCatalog controls whether shell-tool descriptions are injected.
	// It is true for the Codex engine and false for schema-driven model_api.
	AgentToolCatalog bool
	// EvidenceRetryMax caps how many *extra* extraction attempts are made when a
	// unit's candidates fail the verbatim-quote evidence check. On such a failure
	// the model is fed a Chinese explanation of what it got wrong plus the cited
	// [new] messages' 原文 and asked to re-extract without paraphrasing/splicing
	// the source_quote. 0 disables retry (extract exactly once). Must be >= 0.
	EvidenceRetryMax int
	WorkRules        workrule.Reader
	Skills           skill.Reader
	SystemPrompts    textstore.Reader
}

type WorkerStats struct {
	ChatsLoaded    int
	ChatsProcessed int
	Units          int
	Candidates     int
	Created        int
	Updated        int
	// Skipped counts info-insufficient candidates dropped for lacking a
	// fingerprintable identity slot (see PersistStats.Skipped).
	Skipped int
}

// Worker performs network enrichment outside transactions, then commits all
// candidates and the watermark for one chat atomically. Extraction runs as a
// function-calling loop: the model may call retrieval tools (chat history,
// memory) via the per-unit tool box before emitting the final result.
type Worker struct {
	store     pipelineStore
	model     ToolExtractor
	facts     factReader
	dedup     candidateDeduplicator
	toolBox   toolBoxBuilder
	sharedMem sharedmem.SharedMemoryReader
	opts      WorkerOptions
	now       func() time.Time
}

func NewWorker(store pipelineStore, model ToolExtractor, facts factReader, dedup candidateDeduplicator, toolBox toolBoxBuilder, sharedMem sharedmem.SharedMemoryReader, opts WorkerOptions) (*Worker, error) {
	if store == nil {
		return nil, fmt.Errorf("extract worker store is nil")
	}
	if model == nil {
		return nil, fmt.Errorf("extract worker model is nil")
	}
	if facts == nil {
		return nil, fmt.Errorf("extract worker fact reader is nil")
	}
	if dedup == nil {
		return nil, fmt.Errorf("extract worker semantic deduplicator is nil")
	}
	if toolBox == nil {
		return nil, fmt.Errorf("extract worker tool box builder is nil")
	}
	if sharedMem == nil {
		return nil, fmt.Errorf("extract worker shared memory reader is nil")
	}
	if opts.WorkRules == nil {
		return nil, fmt.Errorf("extract worker work rule reader is nil")
	}
	if opts.Skills == nil {
		return nil, fmt.Errorf("extract worker skill reader is nil")
	}
	if opts.SystemPrompts == nil {
		return nil, fmt.Errorf("extract worker system prompt reader is nil")
	}
	if err := validateLoadOptions(opts.Load); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.PrincipalOpenID) == "" {
		return nil, fmt.Errorf("extract worker principal open_id is empty")
	}
	if strings.TrimSpace(opts.ModelName) == "" {
		return nil, fmt.Errorf("extract worker model name is empty")
	}
	if opts.FactLimit <= 0 {
		return nil, fmt.Errorf("extract worker fact limit must be positive")
	}
	if opts.KeyPersonLimit <= 0 {
		return nil, fmt.Errorf("extract worker key person limit must be positive")
	}
	if opts.MaxPromptChars <= 0 {
		return nil, fmt.Errorf("extract worker max prompt chars must be positive")
	}
	if opts.EvidenceRetryMax < 0 {
		return nil, fmt.Errorf("extract worker evidence retry max must be non-negative")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("extract worker location is nil")
	}
	return &Worker{store: store, model: model, facts: facts, dedup: dedup, toolBox: toolBox, sharedMem: sharedMem, opts: opts, now: time.Now}, nil
}

// PendingChatIDs lists the chats with work left beyond their extraction
// watermark. Extraction itself always runs one chat at a time through
// ExtractChat, so reconciliation only has to name the chats a real-time wake-up
// missed and let the caller schedule them.
func (w *Worker) PendingChatIDs(ctx context.Context) ([]string, error) {
	return w.store.PendingChatIDs(ctx)
}

// ExtractChat processes the chat that M2 just advanced, then returns the exact
// Todo rows M3 committed. Duplicate wake-ups are cheap: a chat with no messages
// beyond its extraction watermark returns zero stats and no Todo references.
func (w *Worker) ExtractChat(ctx context.Context, chatID string) (stats WorkerStats, todos []TodoRef, retErr error) {
	batch, err := w.store.LoadPendingChat(ctx, chatID, w.opts.Load)
	if err != nil {
		return WorkerStats{}, nil, err
	}
	if batch == nil {
		return WorkerStats{}, nil, nil
	}
	stats = WorkerStats{ChatsLoaded: 1}
	startedAt := w.now().UTC()
	runID, err := w.store.StartExtractionRun(ctx, batch.Group.ChatID, startedAt)
	if err != nil {
		return stats, nil, err
	}
	runCtx, usageCollector := agentusage.WithCollector(ctx)
	messageCount := countNewMessages(*batch)
	defer func() {
		finishedAt := w.now().UTC()
		status := "succeeded"
		var errorDetail *string
		if retErr != nil {
			status = "failed"
			detail := retErr.Error()
			errorDetail = &detail
		}
		finishErr := w.store.FinishExtractionRun(context.WithoutCancel(ctx), runID, ExtractionRunFinish{
			Status: status, MessageCount: int64(messageCount), TodoCount: int64(stats.Created),
			Usage: usageCollector.Total(), ErrorDetail: errorDetail, FinishedAt: finishedAt,
		})
		if finishErr != nil {
			retErr = errors.Join(retErr, finishErr)
		}
	}()

	batchStats, persisted, err := w.extractBatch(runCtx, *batch, startedAt)
	if err != nil {
		return stats, nil, err
	}
	mergeWorkerStats(&stats, batchStats)
	return stats, append([]TodoRef(nil), persisted.Todos...), nil
}

func countNewMessages(batch ChatBatch) int {
	if batch.NewMessageCount > 0 {
		return batch.NewMessageCount
	}
	seen := make(map[string]struct{})
	for _, unit := range batch.Units {
		for _, message := range unit.Messages {
			if !message.IsNew {
				continue
			}
			seen[message.MessageID] = struct{}{}
		}
	}
	return len(seen)
}

func (w *Worker) extractBatch(ctx context.Context, batch ChatBatch, runNow time.Time) (WorkerStats, PersistStats, error) {
	stats := WorkerStats{}
	// 实时读一次共享记忆文本，注入本 chat 各 unit 的抽取 prompt；读表出错 fail-fast。
	sharedMemory, err := w.sharedMem.Text(ctx)
	if err != nil {
		return stats, PersistStats{}, fmt.Errorf("read shared memory chat_id=%s: %w", batch.Group.ChatID, err)
	}
	workRules, err := w.opts.WorkRules.Block(ctx, workrule.StageExtract)
	if err != nil {
		return stats, PersistStats{}, fmt.Errorf("read extract work rules chat_id=%s: %w", batch.Group.ChatID, err)
	}
	skills, err := w.opts.Skills.Catalog(ctx, skill.StageExtract)
	if err != nil {
		return stats, PersistStats{}, fmt.Errorf("read extract skills chat_id=%s: %w", batch.Group.ChatID, err)
	}
	systemPrompt, err := w.opts.SystemPrompts.Content(ctx, textstore.SystemPromptM3Key)
	if err != nil {
		return stats, PersistStats{}, fmt.Errorf("read M3 system prompt chat_id=%s: %w", batch.Group.ChatID, err)
	}
	toolCatalog := ""
	if w.opts.AgentToolCatalog {
		toolCatalog, err = toolcatalog.Block(toolcatalog.StageExtract)
		if err != nil {
			return stats, PersistStats{}, fmt.Errorf("read extract tool catalog chat_id=%s: %w", batch.Group.ChatID, err)
		}
	}
	// Facts are bound to the group / project / key persons of this chat, not to
	// one unit, so they are read once per chat rather than once per unit.
	facts, err := w.loadFacts(ctx, batch, runNow)
	if err != nil {
		return stats, PersistStats{}, err
	}
	results := make([]UnitExtraction, 0, len(batch.Units))
	for index := range batch.Units {
		unit := batch.Units[index]
		prompt, err := BuildPrompt(batch, unit, facts, runNow, PromptOptions{
			PrincipalOpenID: w.opts.PrincipalOpenID, Location: w.opts.Location, MaxChars: w.opts.MaxPromptChars,
			SystemPrompt: systemPrompt, ToolCatalog: toolCatalog,
			SharedMemory: sharedMemory, WorkRules: workRules, Skills: skills,
		})
		if err != nil {
			return stats, PersistStats{}, fmt.Errorf("build extraction prompt chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
		}
		box, err := w.toolBox.Build(batch, unit)
		if err != nil {
			return stats, PersistStats{}, fmt.Errorf("build extraction tool box chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
		}
		// PersistChat re-reads the unit out of batch.Units by key, so hydrated
		// evidence has to land there and not in a local copy.
		resolved, candidateCount, err := w.extractUnitWithRetry(ctx, batch, &batch.Units[index], prompt, box)
		if err != nil {
			return stats, PersistStats{}, err
		}
		results = append(results, UnitExtraction{
			UnitKey: unit.Key, Candidates: resolved, Facts: facts,
		})
		stats.Units++
		stats.Candidates += candidateCount
	}
	persisted, err := w.store.PersistChat(ctx, batch, results, w.opts.ModelName)
	if err != nil {
		return stats, PersistStats{}, fmt.Errorf("persist extracted chat chat_id=%s: %w", batch.Group.ChatID, err)
	}
	stats.ChatsProcessed = 1
	stats.Created = persisted.Created
	stats.Updated = persisted.Updated
	stats.Skipped = persisted.Skipped
	return stats, persisted, nil
}

// loadFacts loads two layers per subject: today's detail facts (excluding
// rollups) and yesterday's single rollup. Subjects are the group, its project,
// and the key persons of this chat (assigners ∪ leaders ∪ speakers).
func (w *Worker) loadFacts(ctx context.Context, batch ChatBatch, now time.Time) ([]contextsnap.Fact, error) {
	subjects := []factSubject{{subjectType: "group", subjectID: batch.Group.ID}}
	if batch.Group.ProjectID != nil {
		subjects = append(subjects, factSubject{subjectType: "project", subjectID: *batch.Group.ProjectID})
	}
	for _, personID := range selectKeyPersonIDs(batch, w.opts.KeyPersonLimit) {
		subjects = append(subjects, factSubject{subjectType: "person", subjectID: personID})
	}

	localNow := now.In(w.opts.Location)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, w.opts.Location)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	excludeRollup := progress.FactSourceRollup
	rollupKind := progress.FactSourceRollup

	facts := make([]contextsnap.Fact, 0, len(subjects)*(w.opts.FactLimit+1))
	for _, subject := range subjects {
		if subject.subjectID == 0 {
			continue
		}
		details, err := w.facts.ListFacts(ctx, progress.FactFilter{
			SubjectType:       subject.subjectType,
			SubjectID:         subject.subjectID,
			From:              &todayStart,
			Until:             &tomorrowStart,
			ExcludeSourceKind: &excludeRollup,
			Limit:             w.opts.FactLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("read today facts chat_id=%s subject=%s/%d: %w",
				batch.Group.ChatID, subject.subjectType, subject.subjectID, err)
		}
		rollups, err := w.facts.ListFacts(ctx, progress.FactFilter{
			SubjectType: subject.subjectType,
			SubjectID:   subject.subjectID,
			From:        &yesterdayStart,
			Until:       &todayStart,
			SourceKind:  &rollupKind,
			Limit:       1,
		})
		if err != nil {
			return nil, fmt.Errorf("read yesterday rollup chat_id=%s subject=%s/%d: %w",
				batch.Group.ChatID, subject.subjectType, subject.subjectID, err)
		}
		for _, fact := range append(details, rollups...) {
			facts = append(facts, contextsnap.Fact{
				ID: fact.ID, SubjectType: fact.SubjectType, SubjectID: fact.SubjectID,
				Description: fact.Description, OccurredAt: fact.OccurredAt.UTC().Format(time.RFC3339),
			})
		}
	}
	return facts, nil
}

type factSubject struct {
	subjectType string
	subjectID   uint64
}

// selectKeyPersonIDs unions assigners (from open todos), IsLeader participants
// and message speakers, then truncates to limit. People without a person-table
// id are skipped silently.
func selectKeyPersonIDs(batch ChatBatch, limit int) []uint64 {
	if limit <= 0 {
		return nil
	}
	seen := make(map[uint64]struct{})
	ordered := make([]uint64, 0, limit)
	add := func(id *uint64) {
		if id == nil || *id == 0 {
			return
		}
		if _, ok := seen[*id]; ok {
			return
		}
		if len(ordered) >= limit {
			return
		}
		seen[*id] = struct{}{}
		ordered = append(ordered, *id)
	}
	for _, todo := range batch.OpenTodos {
		add(todo.AssignerPersonID)
	}
	for _, unit := range batch.Units {
		byOpenID := make(map[string]*uint64, len(unit.Participants))
		for i := range unit.Participants {
			participant := &unit.Participants[i]
			byOpenID[participant.OpenID] = participant.PersonID
			if participant.IsLeader {
				add(participant.PersonID)
			}
		}
		for _, message := range unit.Messages {
			add(byOpenID[message.SenderOpenID])
		}
	}
	return ordered
}

func mergeWorkerStats(target *WorkerStats, source WorkerStats) {
	target.ChatsProcessed += source.ChatsProcessed
	target.Units += source.Units
	target.Candidates += source.Candidates
	target.Created += source.Created
	target.Updated += source.Updated
	target.Skipped += source.Skipped
}

// extractUnitWithRetry runs "hydrate cited evidence + ExtractWithTools + full
// candidate validation" as one retryable unit. Failures the model can fix by
// itself do not abort the round: a Chinese explanation of the mistake is
// appended to the user prompt and the whole unit is re-extracted, up to
// opts.EvidenceRetryMax extra attempts. Two kinds qualify — a malformed final
// message (prose instead of JSON, a missing candidates field, a candidate that
// fails validation) and bad evidence (a paraphrased source_quote, an invented
// source_message_id, a candidate grounded in no [new] message). Everything else
// (transport, timeout, dedup error) aborts fail-fast immediately. Retries also
// stop once attempts are exhausted, propagating the last error.
func (w *Worker) extractUnitWithRetry(ctx context.Context, batch ChatBatch, unit *ConversationUnit, prompt Prompt, box ToolBox) ([]ResolvedCandidate, int, error) {
	current := prompt
	for attempt := 0; ; attempt++ {
		extracted, err := w.model.ExtractWithTools(ctx, current, box)
		if err != nil {
			// A broken final message is worth another shot: the model already
			// spent minutes reading the chat and running tools, and the mistake
			// is in the shape of the answer, not in the answer.
			if errors.Is(err, ErrInvalidExtraction) && attempt < w.opts.EvidenceRetryMax {
				current = Prompt{System: prompt.System, User: prompt.User + "\n\n" + buildFormatFeedback(err)}
				continue
			}
			return nil, 0, fmt.Errorf("extract todos chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
		}
		if extracted == nil {
			return nil, 0, fmt.Errorf("extract todos chat_id=%s unit=%s: nil result", batch.Group.ChatID, unit.Key)
		}
		if err := w.hydrateCitedMessages(ctx, batch, unit, extracted); err != nil {
			return nil, 0, err
		}
		resolved, evidenceErrs, err := w.validateExtraction(ctx, batch, *unit, extracted)
		if err != nil {
			return nil, 0, err
		}
		if len(evidenceErrs) == 0 {
			return resolved, len(extracted.Candidates), nil
		}
		// Self-correctable evidence failure. Retry with feedback if budget
		// remains; otherwise fail-fast with the aggregated errors (原文 included).
		if attempt >= w.opts.EvidenceRetryMax {
			return nil, 0, fmt.Errorf("validate extracted evidence chat_id=%s unit=%s: exhausted %d evidence retries: %s",
				batch.Group.ChatID, unit.Key, w.opts.EvidenceRetryMax, strings.Join(evidenceErrs, "; "))
		}
		current = Prompt{System: prompt.System, User: prompt.User + "\n\n" + buildEvidenceFeedback(evidenceErrs)}
	}
}

// hydrateCitedMessages pulls cited evidence that is missing from the unit but
// really exists in the chat into the unit's message set. The model reads the
// whole chat with its own tools, so it legitimately cites bot replies, messages
// from a sibling topic, and messages that landed after the batch was loaded —
// none of which the unit slice contains. Evidence timestamps, leader
// attribution and the context snapshot all read cited messages back out of the
// unit, so those messages have to be present there, not merely tolerated.
func (w *Worker) hydrateCitedMessages(ctx context.Context, batch ChatBatch, unit *ConversationUnit, extracted *ExtractionResult) error {
	present := make(map[string]struct{}, len(unit.Messages))
	for _, message := range unit.Messages {
		present[message.MessageID] = struct{}{}
	}
	missing := make([]string, 0)
	collect := func(messageIDs []string) {
		for _, messageID := range messageIDs {
			if _, ok := present[messageID]; ok {
				continue
			}
			present[messageID] = struct{}{}
			missing = append(missing, messageID)
		}
	}
	for i := range extracted.Candidates {
		collect(extracted.Candidates[i].SourceMessageIDs)
	}
	if len(missing) == 0 {
		return nil
	}
	found, err := w.store.LoadChatMessages(ctx, batch.Group.ChatID, missing)
	if err != nil {
		return fmt.Errorf("hydrate cited evidence chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
	}
	if len(found) == 0 {
		return nil
	}
	unit.Messages = mergeChronological(unit.Messages, found)
	return nil
}

// mergeChronological merges two chronologically ordered message slices into one,
// preserving the (create_time, database id) order that snapshotConversation
// relies on when it keeps the tail of a unit.
func mergeChronological(base, extra []MessageContext) []MessageContext {
	merged := make([]MessageContext, 0, len(base)+len(extra))
	first, second := 0, 0
	for first < len(base) && second < len(extra) {
		if messageBefore(extra[second], base[first]) {
			merged = append(merged, extra[second])
			second++
			continue
		}
		merged = append(merged, base[first])
		first++
	}
	merged = append(merged, base[first:]...)
	return append(merged, extra[second:]...)
}

func messageBefore(first, second MessageContext) bool {
	if first.CreateTime != second.CreateTime {
		return first.CreateTime < second.CreateTime
	}
	return first.DatabaseID < second.DatabaseID
}

// validateExtraction validates every candidate in one extraction result. It
// returns the resolved candidates on full success. When one or more candidates
// fail only the verbatim-quote check, it returns the collected evidence error
// messages (so the whole unit can be re-extracted with a single feedback block)
// and nil resolved/err. Any non-self-correctable failure is returned as err.
func (w *Worker) validateExtraction(ctx context.Context, batch ChatBatch, unit ConversationUnit, extracted *ExtractionResult) ([]ResolvedCandidate, []string, error) {
	var evidenceErrs []string
	for i := range extracted.Candidates {
		if err := ValidateCandidate(&extracted.Candidates[i]); err != nil {
			return nil, nil, fmt.Errorf("validate extracted candidate chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
		}
		if err := validateCandidateEvidence(unit, &extracted.Candidates[i]); err != nil {
			if selfCorrectableEvidence(err) {
				evidenceErrs = append(evidenceErrs, fmt.Sprintf("第%d条线索：%s", i+1, err.Error()))
				continue
			}
			return nil, nil, fmt.Errorf("validate extracted evidence chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
		}
	}
	if len(evidenceErrs) > 0 {
		return nil, evidenceErrs, nil
	}
	resolved := make([]ResolvedCandidate, len(extracted.Candidates))
	for i := range extracted.Candidates {
		// The semantic index is partitioned by the Todo's own project_id, which
		// persistence derives with resolveProject (group binding first, then
		// project_hint). Searching with the raw group binding would look in the
		// wrong partition whenever the group is unbound but the hint resolves,
		// and persistence would then reject the match as a domain change.
		projectID, _ := resolveProject(batch, extracted.Candidates[i])
		resolution, err := w.dedup.Resolve(ctx, extracted.Candidates[i], projectID)
		if err != nil {
			return nil, nil, fmt.Errorf("deduplicate extracted candidate chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
		}
		resolved[i] = ResolvedCandidate{Candidate: extracted.Candidates[i], Semantic: resolution}
	}
	return resolved, nil, nil
}

// selfCorrectableEvidence reports whether an evidence failure is one the model
// can fix when told what it got wrong, rather than a bug that must abort the
// chat: a rewritten quote, an invented message id, or a clue grounded in no
// [new] message.
func selfCorrectableEvidence(err error) bool {
	return errors.Is(err, ErrEvidenceQuoteMismatch) ||
		errors.Is(err, ErrEvidenceUnknownMessage) ||
		errors.Is(err, ErrEvidenceNoNewSource)
}

// buildEvidenceFeedback renders the Chinese retry feedback appended after the
// user prompt. evidenceErrs already carry the per-candidate detail (offending
// quote, invented id, cited 原文); this adds the rules the model has to satisfy
// on the next attempt.
func buildFormatFeedback(err error) string {
	return "【上一轮的最终消息没能解析，请重新输出】\n解析报错：" + err.Error() +
		"\n\n本轮的调查结论不用推翻，也不要重新跑工具。" +
		"只需要把同样的结论按输出格式重新写一遍最终消息：" +
		"从 { 开始、到 } 结束的单个 JSON 对象，前后不能有任何其它字符，也不要包代码围栏。" +
		"确实没有值得留下的线索时输出 {\"candidates\": []}。"
}

func buildEvidenceFeedback(evidenceErrs []string) string {
	var b strings.Builder
	b.WriteString("【上一轮抽取校验未通过，请修正后重新抽取】\n")
	b.WriteString("你上一轮抽取的线索里，下面这些证据引用没通过校验：\n")
	for _, msg := range evidenceErrs {
		b.WriteString("- ")
		b.WriteString(msg)
		b.WriteString("\n")
	}
	b.WriteString("\n请重新抽取本段会话，硬性要求：\n")
	b.WriteString("1. source_message_ids 里的每个 id 都必须是这个群里真实存在的消息 id，逐字复制自 [new] 消息、上下文消息或工具返回的原始结果，不得凭印象拼造或改写。引用同群里本段会话之外的消息（机器人回复、其它话题、刚到的新消息）是允许的。\n")
	b.WriteString("2. 每条线索至少要引用一条本轮的 [new] 消息作为证据；只靠历史消息支撑的线索本轮不要输出。\n")
	b.WriteString("3. source_quote 必须从某一条 [new] 消息里逐字连续复制（exact contiguous substring），不得改写、补字、删字，也不得跨片段或跨消息拼接；如果一句话在原文里被打断，就只截取其中真正连续的一段作为 quote，并让 source_message_ids 指向它所在的那条消息。\n")
	b.WriteString("\n请重新输出完整的 candidates（JSON），不要输出解释。")
	return b.String()
}

func validateCandidateEvidence(unit ConversationUnit, candidate *Candidate) error {
	if err := validateEvidence(unit, candidate.SourceMessageIDs, candidate.SourceQuote); err != nil {
		return err
	}
	return nil
}

// validateEvidence is the citation discipline for todo candidates: every cited
// id must really exist in this unit, at least one of them must be an extractable
// [new] message, and the quote must appear verbatim in one of those new messages.
func validateEvidence(unit ConversationUnit, sourceMessageIDs []string, sourceQuote string) error {
	byID := make(map[string]MessageContext, len(unit.Messages))
	for _, message := range unit.Messages {
		byID[message.MessageID] = message
	}
	hasNew := false
	quoteFound := false
	for _, messageID := range sourceMessageIDs {
		message, ok := byID[messageID]
		if !ok {
			// Cited evidence that exists anywhere in the chat was already
			// hydrated into the unit, so a miss here means the id is invented.
			return fmt.Errorf("%w: source_message_id %q", ErrEvidenceUnknownMessage, messageID)
		}
		if message.IsNew && message.Extractable {
			hasNew = true
		}
		if message.IsNew && containsNormalized(message.Content, sourceQuote) {
			quoteFound = true
		}
	}
	if !hasNew {
		return fmt.Errorf("%w: cited messages are %s", ErrEvidenceNoNewSource, citedMessagesText(unit, sourceMessageIDs))
	}
	if !quoteFound {
		return fmt.Errorf("%w: source_quote %q is not present in cited [new] messages; cited [new] messages: %s",
			ErrEvidenceQuoteMismatch, sourceQuote, citedNewMessagesText(unit, sourceMessageIDs))
	}
	return nil
}

func containsNormalized(content, quote string) bool {
	normalize := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	return strings.Contains(normalize(content), normalize(quote))
}

// evidenceMessageContentMax caps how many runes of one cited [new] message are
// echoed into evidence errors and retry feedback, so a long message can't blow up
// logs or the prompt while still showing enough原文 to spot the rewrite.
const evidenceMessageContentMax = 500

// citedNewMessagesText renders the [new] messages a candidate cited (message_id +
// truncated 原文) so both humans (logs) and the model (retry feedback) can compare
// the quoted text against the actual source and see where it was rewritten/spliced.
func citedNewMessagesText(unit ConversationUnit, sourceMessageIDs []string) string {
	byID := make(map[string]MessageContext, len(unit.Messages))
	for _, message := range unit.Messages {
		byID[message.MessageID] = message
	}
	lines := make([]string, 0, len(sourceMessageIDs))
	for _, messageID := range sourceMessageIDs {
		message, ok := byID[messageID]
		if !ok || !message.IsNew {
			continue
		}
		lines = append(lines, fmt.Sprintf("message_id=%s 原文=%q", messageID, truncateRunes(message.Content, evidenceMessageContentMax)))
	}
	if len(lines) == 0 {
		return "(无被引用的 [new] 消息)"
	}
	return strings.Join(lines, "\n")
}

// citedMessagesText renders every cited message the unit knows about, tagging
// whether it is [new] or older context, so a "no [new] evidence" failure shows
// what the candidate was actually grounded in.
func citedMessagesText(unit ConversationUnit, sourceMessageIDs []string) string {
	byID := make(map[string]MessageContext, len(unit.Messages))
	for _, message := range unit.Messages {
		byID[message.MessageID] = message
	}
	lines := make([]string, 0, len(sourceMessageIDs))
	for _, messageID := range sourceMessageIDs {
		message, ok := byID[messageID]
		if !ok {
			continue
		}
		tag := "context"
		if message.IsNew {
			tag = "new"
		}
		if !message.Extractable {
			tag += ",not-extractable"
		}
		lines = append(lines, fmt.Sprintf("message_id=%s(%s) 原文=%q", messageID, tag, truncateRunes(message.Content, evidenceMessageContentMax)))
	}
	if len(lines) == 0 {
		return "(无)"
	}
	return strings.Join(lines, "; ")
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…(已截断)"
}
