package factengine

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
)

type cursorStore interface {
	Cursor(context.Context, string) (uint64, bool, error)
	AdvanceCursor(context.Context, string, uint64, time.Time) error
}

// worldMaintainer runs one complete world-maintenance Agent session. The Agent
// writes current entities, relations and Facts through generic Jarvis tools;
// its final message is only an audit summary and has no machine schema.
type worldMaintainer interface {
	Maintain(context.Context, string, string) (string, error)
}

type WorkerOptions struct {
	BatchLimit       int
	MaxMaterialChars int
	Window           WindowOptions
	Prompts          textstore.Reader
}

type Stats struct {
	Calls         int
	Units         int
	MaterialChars int
	Result        string
	Sources       []SourceStats
}

type SourceStats struct {
	Source string
	Units  int
	LastID uint64
	Seeded bool
}

// Worker periodically gives one bounded batch of new evidence to one complete
// world-model Agent. Go owns only material projection, cursors and the coarse
// character budget; all semantic reads and writes belong to the Agent.
type Worker struct {
	store      cursorStore
	sources    []MaterialSource
	maintainer worldMaintainer
	opts       WorkerOptions
}

func NewWorker(store cursorStore, sources []MaterialSource, maintainer worldMaintainer, opts WorkerOptions) (*Worker, error) {
	if store == nil {
		return nil, fmt.Errorf("fact engine store is nil")
	}
	if maintainer == nil {
		return nil, fmt.Errorf("fact engine maintainer is nil")
	}
	if opts.BatchLimit <= 0 {
		return nil, fmt.Errorf("fact engine batch limit must be positive")
	}
	if opts.MaxMaterialChars <= 0 {
		return nil, fmt.Errorf("fact engine max material chars must be positive")
	}
	if err := opts.Window.validate(); err != nil {
		return nil, err
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("fact engine prompt reader is nil")
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("fact engine has no material sources")
	}
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source.Name == "" || source.MaxID == nil || source.Units == nil {
			return nil, fmt.Errorf("fact engine material source is incomplete: %+v", source)
		}
		if _, duplicate := seen[source.Name]; duplicate {
			return nil, fmt.Errorf("fact engine material source %q is duplicated", source.Name)
		}
		seen[source.Name] = struct{}{}
	}
	return &Worker{store: store, sources: append([]MaterialSource(nil), sources...), maintainer: maintainer, opts: opts}, nil
}

type selectedSource struct {
	index int
	maxID uint64
	units []SourceUnit
}

// ExtractOnce keeps the historical command name, but one invocation now runs
// exactly one world-maintenance Agent session over all selected source material.
// If the configured row batch renders above the coarse character budget, the
// row limit is halved and re-read until it fits. If one row from each active
// source still exceeds the budget, only the whole source batches that fit are
// kept. A single complete row is never truncated, even when it alone exceeds
// the budget.
func (w *Worker) ExtractOnce(ctx context.Context) (Stats, error) {
	stats := Stats{Sources: make([]SourceStats, len(w.sources))}
	for i, source := range w.sources {
		stats.Sources[i].Source = source.Name
		if err := w.seedSourceAtPresent(ctx, source, &stats.Sources[i]); err != nil {
			return stats, fmt.Errorf("source=%s: %w", source.Name, err)
		}
	}

	limit := w.opts.BatchLimit
	var selected []selectedSource
	var userPrompt string
	for {
		var err error
		selected, err = w.selectSources(ctx, limit)
		if err != nil {
			return stats, err
		}
		if len(selected) == 0 {
			return stats, nil
		}
		userPrompt, err = buildWorldBatchPrompt(selected)
		if err != nil {
			return stats, err
		}
		stats.MaterialChars = utf8.RuneCountInString(userPrompt)
		if stats.MaterialChars <= w.opts.MaxMaterialChars {
			break
		}
		if limit == 1 {
			selected, userPrompt, stats.MaterialChars, err = fitWholeSources(selected, w.opts.MaxMaterialChars)
			if err != nil {
				return stats, err
			}
			break
		}
		limit = max(1, limit/2)
	}

	rolePrompt, err := w.opts.Prompts.Content(ctx, textstore.SystemPromptFactExtractKey)
	if err != nil {
		return stats, fmt.Errorf("read fact extraction system prompt: %w", err)
	}
	systemPrompt, err := buildAgentSystemPrompt(rolePrompt)
	if err != nil {
		return stats, err
	}
	result, err := w.maintainer.Maintain(ctx, systemPrompt, userPrompt)
	if err != nil {
		return stats, fmt.Errorf("run factengine world maintenance: %w", err)
	}

	stats.Calls = 1
	stats.Result = result
	for _, item := range selected {
		occurredAt := latestOccurredAt(item.units)
		if err := w.store.AdvanceCursor(ctx, w.sources[item.index].Name, item.maxID, occurredAt); err != nil {
			return stats, fmt.Errorf("advance fact source cursor source=%s last_id=%d: %w", w.sources[item.index].Name, item.maxID, err)
		}
		stats.Sources[item.index].Units = len(item.units)
		stats.Sources[item.index].LastID = item.maxID
		stats.Units += len(item.units)
	}
	return stats, nil
}

// fitWholeSources is the final coarse boundary after the row limit reaches one.
// It never splits a SourceUnit or advances a cursor past omitted material. The
// first source is retained even when its one complete row alone exceeds the
// budget; later sources remain behind their own cursors for the next round.
func fitWholeSources(selected []selectedSource, maxChars int) ([]selectedSource, string, int, error) {
	kept := make([]selectedSource, 0, len(selected))
	var prompt string
	var chars int
	for _, source := range selected {
		candidate := append(append([]selectedSource(nil), kept...), source)
		candidatePrompt, err := buildWorldBatchPrompt(candidate)
		if err != nil {
			return nil, "", 0, err
		}
		candidateChars := utf8.RuneCountInString(candidatePrompt)
		if len(kept) > 0 && candidateChars > maxChars {
			continue
		}
		kept = candidate
		prompt = candidatePrompt
		chars = candidateChars
	}
	return kept, prompt, chars, nil
}

func (w *Worker) seedSourceAtPresent(ctx context.Context, source MaterialSource, stats *SourceStats) error {
	_, exists, err := w.store.Cursor(ctx, source.Name)
	if err != nil {
		return err
	}
	if exists || !source.StartAtPresent {
		return nil
	}
	maxID, err := source.MaxID(ctx)
	if err != nil {
		return err
	}
	if maxID == 0 {
		return nil
	}
	if err := w.store.AdvanceCursor(ctx, source.Name, maxID, time.Time{}); err != nil {
		return err
	}
	stats.LastID = maxID
	stats.Seeded = true
	return nil
}

func (w *Worker) selectSources(ctx context.Context, limit int) ([]selectedSource, error) {
	selected := make([]selectedSource, 0, len(w.sources))
	for i, source := range w.sources {
		cursor, exists, err := w.store.Cursor(ctx, source.Name)
		if err != nil {
			return nil, fmt.Errorf("source=%s: load cursor: %w", source.Name, err)
		}
		if !exists && source.StartAtPresent {
			continue
		}
		units, maxID, err := source.Units(ctx, cursor, limit, w.opts.Window)
		if err != nil {
			return nil, fmt.Errorf("source=%s: %w", source.Name, err)
		}
		if maxID == 0 {
			continue
		}
		if len(units) == 0 {
			return nil, fmt.Errorf("source=%s returned max_id=%d with no material units", source.Name, maxID)
		}
		selected = append(selected, selectedSource{index: i, maxID: maxID, units: units})
	}
	return selected, nil
}

func buildWorldBatchPrompt(selected []selectedSource) (string, error) {
	parts := []string{
		"WORLD_CHANGES",
		"以下是自各来源成功游标之后新增的完整材料。完整阅读全部材料，使用工具查询现有世界状态并直接完成必要维护。材料没有带来新认知时不要写入。",
	}
	for _, source := range selected {
		for _, unit := range source.units {
			prompt, err := unit.Prompt()
			if err != nil {
				return "", err
			}
			parts = append(parts, "BEGIN_MATERIAL_UNIT\n"+escapeInvalidUTF8(prompt)+"\nEND_MATERIAL_UNIT")
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func escapeInvalidUTF8(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	var b strings.Builder
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, "\\x%02X", value[0])
			value = value[1:]
			continue
		}
		b.WriteString(value[:size])
		value = value[size:]
	}
	return b.String()
}

func buildAgentSystemPrompt(rolePrompt string) (string, error) {
	rolePrompt = strings.TrimSpace(rolePrompt)
	if rolePrompt == "" {
		return "", fmt.Errorf("fact extraction system prompt is empty")
	}
	tools, err := toolcatalog.Block(toolcatalog.StageFactEngine)
	if err != nil {
		return "", fmt.Errorf("build fact engine tool catalog: %w", err)
	}
	return rolePrompt + "\n\n" + strings.TrimSpace(tools), nil
}

func latestOccurredAt(units []SourceUnit) time.Time {
	var latest time.Time
	for _, unit := range units {
		if unit.OccurredAt.After(latest) {
			latest = unit.OccurredAt
		}
	}
	return latest
}
