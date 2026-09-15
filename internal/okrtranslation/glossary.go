package okrtranslation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const translationPromptVersion = "regional-okr-glossary-v1"

// GlossaryTerm is one authoritative Chinese-to-English business term. Source
// and definition help the model disambiguate terms with product-specific use.
type GlossaryTerm struct {
	Chinese    string `json:"zh"`
	English    string `json:"en"`
	Definition string `json:"definition,omitempty"`
	Source     string `json:"source,omitempty"`
}

type Glossary struct {
	Terms []GlossaryTerm
	hash  string
}

// LoadGlossary validates the checked-in projection of the user-provided
// spreadsheets. Its content hash versions the translation cache automatically.
func LoadGlossary(path string) (Glossary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Glossary{}, fmt.Errorf("read OKR translation glossary %q: %w", path, err)
	}
	var file struct {
		Terms []GlossaryTerm `json:"terms"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return Glossary{}, fmt.Errorf("decode OKR translation glossary %q: %w", path, err)
	}
	if len(file.Terms) == 0 {
		return Glossary{}, fmt.Errorf("OKR translation glossary %q has no terms", path)
	}
	seen := make(map[string]bool, len(file.Terms))
	for index := range file.Terms {
		term := &file.Terms[index]
		term.Chinese = strings.TrimSpace(term.Chinese)
		term.English = strings.TrimSpace(term.English)
		term.Definition = strings.TrimSpace(term.Definition)
		term.Source = strings.TrimSpace(term.Source)
		if term.Chinese == "" || term.English == "" {
			return Glossary{}, fmt.Errorf("OKR translation glossary %q term %d is missing zh or en", path, index)
		}
		if seen[term.Chinese] {
			return Glossary{}, fmt.Errorf("OKR translation glossary %q repeats %q", path, term.Chinese)
		}
		seen[term.Chinese] = true
	}
	// Longest matches first makes compound terms such as 公会任务 authoritative
	// before their shorter component 公会 is considered.
	sort.SliceStable(file.Terms, func(i, j int) bool {
		return len([]rune(file.Terms[i].Chinese)) > len([]rune(file.Terms[j].Chinese))
	})
	digest := sha256.Sum256(append([]byte(translationPromptVersion+"\x00"), raw...))
	return Glossary{Terms: file.Terms, hash: hex.EncodeToString(digest[:])}, nil
}

func (g Glossary) relevant(texts []string) []GlossaryTerm {
	result := make([]GlossaryTerm, 0)
	for _, term := range g.Terms {
		for _, text := range texts {
			if strings.Contains(text, term.Chinese) {
				result = append(result, term)
				break
			}
		}
	}
	return result
}
