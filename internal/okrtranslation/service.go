// Package okrtranslation translates exact OKR text values into English in
// bounded structured batches. Persistence and source freshness remain owned by
// okrworkspace; this package only makes the model decision.
package okrtranslation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	maxBatchItems        = 100
	maxBatchRunes        = 12000
	maxConcurrentBatches = 3
)

type completer interface {
	CompleteStructured(ctx context.Context, operation, schemaName string, schema map[string]any, system, user string) ([]byte, error)
}

type Service struct {
	model    completer
	glossary Glossary
}

func New(model completer, glossaries ...Glossary) (*Service, error) {
	if model == nil {
		return nil, fmt.Errorf("create OKR translator: model is nil")
	}
	if len(glossaries) > 1 {
		return nil, fmt.Errorf("create OKR translator: expected at most one glossary")
	}
	glossary := Glossary{}
	if len(glossaries) == 1 {
		glossary = glossaries[0]
	}
	if glossary.hash == "" {
		digest := sha256.Sum256([]byte(translationPromptVersion))
		glossary.hash = hex.EncodeToString(digest[:])
	}
	return &Service{model: model, glossary: glossary}, nil
}

func (s *Service) CacheKey() string { return s.glossary.hash }

type inputItem struct {
	ID     int    `json:"id"`
	Source string `json:"source"`
}

type outputItem struct {
	ID      int    `json:"id"`
	English string `json:"english"`
}

type translationRequest struct {
	Glossary []GlossaryTerm `json:"glossary"`
	Items    []inputItem    `json:"items"`
}

func (s *Service) Translate(ctx context.Context, texts []string) (map[string]string, error) {
	batches := make([][]string, 0, (len(texts)+maxBatchItems-1)/maxBatchItems)
	for start := 0; start < len(texts); {
		end, runes := start, 0
		for end < len(texts) && end-start < maxBatchItems {
			next := utf8.RuneCountInString(texts[end])
			if end > start && runes+next > maxBatchRunes {
				break
			}
			runes += next
			end++
		}
		batches = append(batches, texts[start:end])
		start = end
	}

	result := make(map[string]string, len(texts))
	if len(batches) == 0 {
		return result, nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	semaphore := make(chan struct{}, maxConcurrentBatches)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for _, batch := range batches {
		batch := batch
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-workCtx.Done():
				return
			}
			defer func() { <-semaphore }()
			translated, err := s.translateBatch(workCtx, batch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			for source, english := range translated {
				result[source] = english
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return result, nil
}

func (s *Service) translateBatch(ctx context.Context, texts []string) (map[string]string, error) {
	input := make([]inputItem, 0, len(texts))
	for id, source := range texts {
		input = append(input, inputItem{ID: id, Source: source})
	}
	payload, err := json.Marshal(translationRequest{Glossary: s.glossary.relevant(texts), Items: input})
	if err != nil {
		return nil, fmt.Errorf("encode OKR translation input: %w", err)
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"translations": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":      map[string]any{"type": "integer"},
						"english": map[string]any{"type": "string"},
					},
					"required":             []string{"id", "english"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"translations"},
		"additionalProperties": false,
	}
	system := "You translate business OKRs from Chinese into concise, natural English. The request includes an authoritative glossary filtered to terms present in these inputs. Use each matching glossary English term consistently; prefer the longest matching Chinese term and use its definition/source to disambiguate context. Inflect grammar or capitalization only when necessary, and do not replace glossary terms with synonyms. Preserve numbers, metrics, product names, acronyms, Markdown, URLs and line structure. Return one faithful English translation for every input id. If a source is already English, keep it unchanged. Never add analysis or new commitments."
	raw, err := s.model.CompleteStructured(ctx, "OKR translation", "okr_translation", schema, system, string(payload))
	if err != nil {
		return nil, fmt.Errorf("translate OKR text: %w", err)
	}
	var decoded struct {
		Translations []outputItem `json:"translations"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode OKR translations: %w", err)
	}
	byID := make(map[int]string, len(decoded.Translations))
	for _, item := range decoded.Translations {
		if item.ID < 0 || item.ID >= len(texts) {
			return nil, fmt.Errorf("OKR translation returned unknown id %d", item.ID)
		}
		if _, exists := byID[item.ID]; exists {
			return nil, fmt.Errorf("OKR translation returned duplicate id %d", item.ID)
		}
		item.English = strings.TrimSpace(item.English)
		if item.English == "" {
			return nil, fmt.Errorf("OKR translation returned empty text for id %d", item.ID)
		}
		byID[item.ID] = item.English
	}
	if len(byID) != len(texts) {
		return nil, fmt.Errorf("OKR translation returned %d items, want %d", len(byID), len(texts))
	}
	result := make(map[string]string, len(texts))
	for id, source := range texts {
		result[source] = byID[id]
	}
	return result, nil
}
