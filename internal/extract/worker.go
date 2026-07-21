package extract

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/memory"
)

type memorySearcher interface {
	Search(context.Context, memory.SearchInput) (*memory.SearchResponse, error)
}

type WorkerOptions struct {
	Load            LoadOptions
	PrincipalOpenID string
	ModelName       string
	MemoryTopK      int
	MemoryThreshold float64
	MaxPromptChars  int
	MaxToolRounds   int
	Location        *time.Location
	// PromptToolGuidance is appended to the extraction prompt for the codex
	// engine (empty for kimi). See CodexToolGuidance.
	PromptToolGuidance string
	// EvidenceRetryMax caps how many *extra* extraction attempts are made when a
	// unit's candidates fail the verbatim-quote evidence check. On such a failure
	// the model is fed a Chinese explanation of what it got wrong plus the cited
	// [new] messages' 原文 and asked to re-extract without paraphrasing/splicing
	// the source_quote. 0 disables retry (extract exactly once). Must be >= 0.
	EvidenceRetryMax int
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
	store   pipelineStore
	model   ToolExtractor
	memory  memorySearcher
	dedup   candidateDeduplicator
	toolBox toolBoxBuilder
	opts    WorkerOptions
	now     func() time.Time
}

func NewWorker(store pipelineStore, model ToolExtractor, memories memorySearcher, dedup candidateDeduplicator, toolBox toolBoxBuilder, opts WorkerOptions) (*Worker, error) {
	if store == nil {
		return nil, fmt.Errorf("extract worker store is nil")
	}
	if model == nil {
		return nil, fmt.Errorf("extract worker model is nil")
	}
	if memories == nil {
		return nil, fmt.Errorf("extract worker memory client is nil")
	}
	if dedup == nil {
		return nil, fmt.Errorf("extract worker semantic deduplicator is nil")
	}
	if toolBox == nil {
		return nil, fmt.Errorf("extract worker tool box builder is nil")
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
	if opts.MemoryTopK <= 0 {
		return nil, fmt.Errorf("extract worker memory top_k must be positive")
	}
	if opts.MemoryThreshold < 0 || opts.MemoryThreshold > 1 {
		return nil, fmt.Errorf("extract worker memory threshold must be between 0 and 1")
	}
	if opts.MaxPromptChars <= 0 {
		return nil, fmt.Errorf("extract worker max prompt chars must be positive")
	}
	if opts.MaxToolRounds <= 0 {
		return nil, fmt.Errorf("extract worker max tool rounds must be positive")
	}
	if opts.EvidenceRetryMax < 0 {
		return nil, fmt.Errorf("extract worker evidence retry max must be non-negative")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("extract worker location is nil")
	}
	return &Worker{store: store, model: model, memory: memories, dedup: dedup, toolBox: toolBox, opts: opts, now: time.Now}, nil
}

func (w *Worker) ExtractOnce(ctx context.Context) (WorkerStats, error) {
	batches, err := w.store.LoadPendingChats(ctx, w.opts.Load)
	if err != nil {
		return WorkerStats{}, err
	}
	stats := WorkerStats{ChatsLoaded: len(batches)}
	runNow := w.now()
	for _, batch := range batches {
		results := make([]UnitExtraction, 0, len(batch.Units))
		for _, unit := range batch.Units {
			query, err := SalientQuery(unit)
			if err != nil {
				return stats, fmt.Errorf("prepare extraction query chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
			}
			filters := map[string]any{"chat_id": batch.Group.ChatID}
			if batch.Group.ProjectID != nil {
				filters = map[string]any{"project_id": *batch.Group.ProjectID}
			}
			memories, err := w.memory.Search(ctx, memory.SearchInput{
				Query: query, Filters: filters, TopK: w.opts.MemoryTopK,
				Threshold: w.opts.MemoryThreshold, Rerank: false,
			})
			if err != nil {
				return stats, fmt.Errorf("search extraction memories chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
			}
			if memories == nil {
				return stats, fmt.Errorf("search extraction memories chat_id=%s unit=%s: nil response", batch.Group.ChatID, unit.Key)
			}
			prompt, err := BuildPrompt(batch, unit, memories.Results, runNow, PromptOptions{
				PrincipalOpenID: w.opts.PrincipalOpenID, Location: w.opts.Location, MaxChars: w.opts.MaxPromptChars,
				ToolGuidance: w.opts.PromptToolGuidance,
			})
			if err != nil {
				return stats, fmt.Errorf("build extraction prompt chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
			}
			box, err := w.toolBox.Build(batch, unit)
			if err != nil {
				return stats, fmt.Errorf("build extraction tool box chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
			}
			resolved, candidateCount, err := w.extractUnitWithRetry(ctx, batch, unit, prompt, box)
			if err != nil {
				return stats, err
			}
			results = append(results, UnitExtraction{UnitKey: unit.Key, Candidates: resolved, Memories: FilterMemoriesForSnapshot(memories.Results)})
			stats.Units++
			stats.Candidates += candidateCount
		}
		persisted, err := w.store.PersistChat(ctx, batch, results, w.opts.ModelName)
		if err != nil {
			return stats, fmt.Errorf("persist extracted chat chat_id=%s: %w", batch.Group.ChatID, err)
		}
		stats.ChatsProcessed++
		stats.Created += persisted.Created
		stats.Updated += persisted.Updated
		stats.Skipped += persisted.Skipped
	}
	return stats, nil
}

// extractUnitWithRetry runs "ExtractWithTools + full candidate validation" as one
// retryable unit. When validation fails only because a source_quote is not a
// verbatim substring of the cited [new] messages (ErrEvidenceQuoteMismatch), it
// does not abort the round: it appends a Chinese explanation of the mistake plus
// the cited [new] 原文 to the user prompt and asks the model to re-extract the
// whole unit, up to opts.EvidenceRetryMax extra attempts. Any other validation
// failure (structural/schema, out-of-unit source id, missing [new] evidence,
// out-of-unit assigner, dedup error) is not self-correctable and aborts fail-fast
// immediately. Retries also stop once attempts are exhausted, propagating the last
// error (which carries the cited 原文 for diagnosis).
func (w *Worker) extractUnitWithRetry(ctx context.Context, batch ChatBatch, unit ConversationUnit, prompt Prompt, box ToolBox) ([]ResolvedCandidate, int, error) {
	current := prompt
	for attempt := 0; ; attempt++ {
		extracted, err := w.model.ExtractWithTools(ctx, current, box, w.opts.MaxToolRounds)
		if err != nil {
			return nil, 0, fmt.Errorf("extract todos chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
		}
		if extracted == nil {
			return nil, 0, fmt.Errorf("extract todos chat_id=%s unit=%s: nil result", batch.Group.ChatID, unit.Key)
		}
		resolved, evidenceErrs, err := w.validateExtraction(ctx, batch, unit, extracted)
		if err != nil {
			return nil, 0, err
		}
		if len(evidenceErrs) == 0 {
			return resolved, len(extracted.Candidates), nil
		}
		// Evidence/quote mismatch: self-correctable. Retry with feedback if budget
		// remains; otherwise fail-fast with the aggregated errors (原文 included).
		if attempt >= w.opts.EvidenceRetryMax {
			return nil, 0, fmt.Errorf("validate extracted evidence chat_id=%s unit=%s: exhausted %d evidence retries: %s",
				batch.Group.ChatID, unit.Key, w.opts.EvidenceRetryMax, strings.Join(evidenceErrs, "; "))
		}
		current = Prompt{System: prompt.System, User: prompt.User + "\n\n" + buildEvidenceFeedback(evidenceErrs)}
	}
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
			if errors.Is(err, ErrEvidenceQuoteMismatch) {
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
		resolution, err := w.dedup.Resolve(ctx, extracted.Candidates[i], batch.Group.ProjectID)
		if err != nil {
			return nil, nil, fmt.Errorf("deduplicate extracted candidate chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
		}
		resolved[i] = ResolvedCandidate{Candidate: extracted.Candidates[i], Semantic: resolution}
	}
	return resolved, nil, nil
}

// buildEvidenceFeedback renders the Chinese retry feedback appended after the user
// prompt: it explains that some source_quote values could not be found verbatim in
// the cited [new] messages (likely paraphrased/padded/spliced across messages) and
// instructs the model to re-extract with source_quote copied verbatim from a single
// [new] message. evidenceErrs already carry each offending quote and the cited 原文.
func buildEvidenceFeedback(evidenceErrs []string) string {
	var b strings.Builder
	b.WriteString("【上一轮抽取校验未通过，请修正后重新抽取】\n")
	b.WriteString("你上一轮抽取的线索里，下面这些 source_quote 在你引用的 [new] 消息原文里找不到——很可能是你改写、补字，或把不连续的片段、多条消息拼接成了引用：\n")
	for _, msg := range evidenceErrs {
		b.WriteString("- ")
		b.WriteString(msg)
		b.WriteString("\n")
	}
	b.WriteString("\n请重新抽取本段会话。硬性要求：每条线索的 source_quote 必须从某一条 [new] 消息里逐字连续复制（exact contiguous substring），不得改写、补字、删字，也不得跨片段或跨消息拼接；如果一句话在原文里被打断，就只截取其中真正连续的一段作为 quote，并让 source_message_ids 指向它所在的那条消息。请重新输出完整的 candidates（JSON），不要输出解释。")
	return b.String()
}

func validateCandidateEvidence(unit ConversationUnit, candidate *Candidate) error {
	byID := make(map[string]MessageContext, len(unit.Messages))
	for _, message := range unit.Messages {
		byID[message.MessageID] = message
	}
	hasNew := false
	quoteFound := false
	for _, messageID := range candidate.SourceMessageIDs {
		message, ok := byID[messageID]
		if !ok {
			return fmt.Errorf("%w: source_message_id %q is outside conversation unit", ErrInvalidCandidate, messageID)
		}
		if message.IsNew && message.Extractable {
			hasNew = true
		}
		if message.IsNew && containsNormalized(message.Content, candidate.SourceQuote) {
			quoteFound = true
		}
	}
	if !hasNew {
		return fmt.Errorf("%w: candidate has no extractable [new] evidence", ErrInvalidCandidate)
	}
	if !quoteFound {
		return fmt.Errorf("%w: source_quote %q is not present in cited [new] messages; cited [new] messages: %s",
			ErrEvidenceQuoteMismatch, candidate.SourceQuote, citedNewMessagesText(unit, candidate.SourceMessageIDs))
	}
	if candidate.AssignerOpenID != nil {
		found := false
		for _, participant := range unit.Participants {
			if participant.OpenID == *candidate.AssignerOpenID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: assigner_open_id %q is outside conversation participants", ErrInvalidCandidate, *candidate.AssignerOpenID)
		}
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

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…(已截断)"
}
