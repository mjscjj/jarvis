package extract

import (
	"context"
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
}

type WorkerStats struct {
	ChatsLoaded    int
	ChatsProcessed int
	Units          int
	Candidates     int
	Created        int
	Updated        int
}

// Worker performs network enrichment outside transactions, then commits all
// candidates and the watermark for one chat atomically. Extraction runs as a
// function-calling loop: the model may call retrieval tools (chat history,
// memory) via the per-unit tool box before emitting the final result.
type Worker struct {
	store   pipelineStore
	model   toolExtractor
	memory  memorySearcher
	dedup   candidateDeduplicator
	toolBox toolBoxBuilder
	opts    WorkerOptions
	now     func() time.Time
}

func NewWorker(store pipelineStore, model toolExtractor, memories memorySearcher, dedup candidateDeduplicator, toolBox toolBoxBuilder, opts WorkerOptions) (*Worker, error) {
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
			})
			if err != nil {
				return stats, fmt.Errorf("build extraction prompt chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
			}
			box, err := w.toolBox.Build(batch, unit)
			if err != nil {
				return stats, fmt.Errorf("build extraction tool box chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
			}
			extracted, err := w.model.ExtractWithTools(ctx, prompt, box, w.opts.MaxToolRounds)
			if err != nil {
				return stats, fmt.Errorf("extract todos chat_id=%s unit=%s: %w", batch.Group.ChatID, unit.Key, err)
			}
			if extracted == nil {
				return stats, fmt.Errorf("extract todos chat_id=%s unit=%s: nil result", batch.Group.ChatID, unit.Key)
			}
			resolved := make([]ResolvedCandidate, len(extracted.Candidates))
			for i := range extracted.Candidates {
				if err := validateStrictSlotShape(extracted.Candidates[i].Slots); err != nil {
					return stats, fmt.Errorf("validate extracted candidate chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
				}
				if err := ValidateCandidate(&extracted.Candidates[i]); err != nil {
					return stats, fmt.Errorf("validate extracted candidate chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
				}
				if err := validateCandidateEvidence(unit, &extracted.Candidates[i]); err != nil {
					return stats, fmt.Errorf("validate extracted evidence chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
				}
				resolution, err := w.dedup.Resolve(ctx, extracted.Candidates[i], batch.Group.ProjectID)
				if err != nil {
					return stats, fmt.Errorf("deduplicate extracted candidate chat_id=%s unit=%s candidate=%d: %w", batch.Group.ChatID, unit.Key, i, err)
				}
				resolved[i] = ResolvedCandidate{Candidate: extracted.Candidates[i], Semantic: resolution}
			}
			results = append(results, UnitExtraction{UnitKey: unit.Key, Candidates: resolved})
			stats.Units++
			stats.Candidates += len(extracted.Candidates)
		}
		persisted, err := w.store.PersistChat(ctx, batch, results, w.opts.ModelName)
		if err != nil {
			return stats, fmt.Errorf("persist extracted chat chat_id=%s: %w", batch.Group.ChatID, err)
		}
		stats.ChatsProcessed++
		stats.Created += persisted.Created
		stats.Updated += persisted.Updated
	}
	return stats, nil
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
		return fmt.Errorf("%w: source_quote %q is not present in cited [new] messages", ErrInvalidCandidate, candidate.SourceQuote)
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
