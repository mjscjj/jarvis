package okrtranslation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

type fakeCompleter struct {
	mu            sync.Mutex
	calls         int
	glossaryTerms int
	omitID        int
	invalid       bool
}

func (f *fakeCompleter) CompleteStructured(_ context.Context, operation, schemaName string, schema map[string]any, _, user string) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if operation != "OKR translation" || schemaName != "okr_translation" || schema["type"] != "object" {
		return nil, fmt.Errorf("unexpected structured completion metadata")
	}
	if f.invalid {
		return []byte(`{"translations":`), nil
	}
	var request translationRequest
	if err := json.Unmarshal([]byte(user), &request); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.glossaryTerms += len(request.Glossary)
	f.mu.Unlock()
	input := request.Items
	output := struct {
		Translations []outputItem `json:"translations"`
	}{Translations: make([]outputItem, 0, len(input))}
	for _, item := range input {
		if item.ID == f.omitID {
			continue
		}
		output.Translations = append(output.Translations, outputItem{ID: item.ID, English: "EN: " + item.Source})
	}
	return json.Marshal(output)
}

func TestTranslateSuppliesOnlyMatchingGlossaryTerms(t *testing.T) {
	model := &fakeCompleter{omitID: -1}
	service, err := New(model, Glossary{hash: "glossary-v1", Terms: []GlossaryTerm{
		{Chinese: "公会任务", English: "Fee Policy"},
		{Chinese: "公会", English: "Creator Network"},
		{Chinese: "直播间", English: "LIVE Room"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Translate(t.Context(), []string{"完善公会任务"}); err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	matched := model.glossaryTerms
	model.mu.Unlock()
	if matched != 2 {
		t.Fatalf("matching glossary terms = %d, want compound and component terms", matched)
	}
	if service.CacheKey() != "glossary-v1" {
		t.Fatalf("cache key = %q", service.CacheKey())
	}
}

func TestTranslateBatchesAndMapsEveryExactSource(t *testing.T) {
	model := &fakeCompleter{omitID: -1}
	service, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, maxBatchItems+1)
	for index := range texts {
		texts[index] = fmt.Sprintf("OKR 内容 %d", index)
	}
	translated, err := service.Translate(t.Context(), texts)
	if err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	calls := model.calls
	model.mu.Unlock()
	if calls != 2 {
		t.Fatalf("model calls = %d, want 2", calls)
	}
	for _, source := range texts {
		if got := translated[source]; got != "EN: "+source {
			t.Fatalf("translation[%q] = %q", source, got)
		}
	}
}

func TestTranslateRejectsIncompleteStructuredResult(t *testing.T) {
	service, err := New(&fakeCompleter{omitID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Translate(t.Context(), []string{"一", "二"}); err == nil {
		t.Fatal("Translate error = nil, want incomplete response rejection")
	}
}
