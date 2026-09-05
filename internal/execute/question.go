package execute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Question is the card M5 puts in front of the principal when it stops for a
// human: a decision, a missing fact, or permission for a side effect the
// approval policy gates. It is the only such mechanism — there is no separate
// approval object — because the answer resumes the same Codex session, where
// the agent judges what to do with it in full context.
//
// Everything semantic (what to ask, which controls to offer, how to word them)
// belongs to the model. Validation below covers only what has to hold for the
// card to be answerable at all.
type Question struct {
	Title  string          `json:"title"`
	Body   string          `json:"body"`
	Fields []QuestionField `json:"fields"`
}

// QuestionField is one control. Type is closed because each value maps to a
// concrete Feishu element; everything else is free text the model writes.
type QuestionField struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Options []string `json:"options"`
	URL     string   `json:"url"`
	Style   string   `json:"style"`
}

// Field types. A button submits the form; link opens a URL and answers nothing.
const (
	FieldButton      = "button"
	FieldSelect      = "select"
	FieldMultiSelect = "multi_select"
	FieldInput       = "input"
	FieldLink        = "link"
)

// ParseQuestion decodes a question and fails fast on anything that would make
// the card unanswerable: no title, no submit button, a control with no name to
// answer under, two controls sharing one name, a choice list with no choices.
func ParseQuestion(raw json.RawMessage) (*Question, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("question is missing")
	}
	var question Question
	if err := json.Unmarshal(raw, &question); err != nil {
		return nil, fmt.Errorf("decode question: %w", err)
	}
	question.Title = strings.TrimSpace(question.Title)
	if question.Title == "" {
		return nil, fmt.Errorf("question title is blank")
	}
	buttons := 0
	names := make(map[string]struct{}, len(question.Fields))
	for position, field := range question.Fields {
		field.Type = strings.TrimSpace(field.Type)
		field.Name = strings.TrimSpace(field.Name)
		field.Label = strings.TrimSpace(field.Label)
		if field.Label == "" {
			return nil, fmt.Errorf("question fields[%d] label is blank", position)
		}
		switch field.Type {
		case FieldButton:
			buttons++
		case FieldSelect, FieldMultiSelect:
			if len(field.Options) == 0 {
				return nil, fmt.Errorf("question fields[%d] %s has no options", position, field.Type)
			}
		case FieldInput:
		case FieldLink:
			if strings.TrimSpace(field.URL) == "" {
				return nil, fmt.Errorf("question fields[%d] link has no url", position)
			}
		default:
			return nil, fmt.Errorf("question fields[%d] has unknown type %q", position, field.Type)
		}
		if field.Type != FieldLink {
			if field.Name == "" {
				return nil, fmt.Errorf("question fields[%d] %s has no name", position, field.Type)
			}
			if _, duplicate := names[field.Name]; duplicate {
				return nil, fmt.Errorf("question fields[%d] reuses name %q", position, field.Name)
			}
			names[field.Name] = struct{}{}
		}
		question.Fields[position] = field
	}
	if buttons == 0 {
		return nil, fmt.Errorf("question has no button, so it cannot be answered")
	}
	return &question, nil
}
