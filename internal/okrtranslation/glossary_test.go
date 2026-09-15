package okrtranslation

import "testing"

func TestRepositoryGlossaryLoadsAuthoritativeTerms(t *testing.T) {
	glossary, err := LoadGlossary("../../data/okr/translation-glossary.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(glossary.Terms) != 355 || len(glossary.hash) != 64 {
		t.Fatalf("glossary terms/hash = %d/%q", len(glossary.Terms), glossary.hash)
	}
	want := map[string]string{
		"公会":   "Creator Network",
		"公会任务": "Fee Policy",
		"主播中心": "Host Center",
	}
	for _, term := range glossary.Terms {
		if expected, ok := want[term.Chinese]; ok {
			if term.English != expected {
				t.Fatalf("glossary[%q] = %q, want %q", term.Chinese, term.English, expected)
			}
			delete(want, term.Chinese)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing repository glossary terms: %+v", want)
	}
}
