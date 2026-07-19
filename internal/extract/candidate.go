// Package extract owns M3's deterministic extraction contract. Model-provider
// transport is intentionally kept outside this file so schema and business
// validation can be tested without a live model endpoint.
package extract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const PromptVersion = "todo-extraction-v1"

var (
	ErrInvalidExtraction     = errors.New("invalid extraction result")
	ErrInvalidCandidate      = errors.New("invalid todo candidate")
	ErrFingerprintIncomplete = errors.New("todo fingerprint identity is incomplete")
)

var requiredSlots = map[string][]string{
	"code_change":      {"repo_ref", "change_summary"},
	"summary_post":     {"source_ref", "target_chat_id", "summary_scope"},
	"investigate":      {"question", "lookup_sources"},
	"schedule_meeting": {"meeting_title", "attendees", "proposed_time"},
	"reply_message":    {"target_chat_id", "message_body"},
	"doc_write":        {"doc_title", "summary_scope"},
	"manual_followup":  {"followup_action"},
}

var identitySlots = map[string][]string{
	"code_change":      {"repo_ref", "change_summary"},
	"summary_post":     {"source_ref", "target_chat_id"},
	"investigate":      {"question"},
	"schedule_meeting": {"meeting_title", "attendees"},
	"reply_message":    {"target_chat_id", "message_body"},
	"doc_write":        {"doc_title"},
	"manual_followup":  {"followup_action"},
}

var allowedSlots = map[string]struct{}{
	"repo_ref": {}, "change_summary": {}, "based_on": {}, "scope": {}, "acceptance": {},
	"source_ref": {}, "target_chat_id": {}, "summary_scope": {}, "assignees": {},
	"question": {}, "lookup_sources": {}, "deliverable": {}, "meeting_title": {},
	"attendees": {}, "proposed_time": {}, "duration_minutes": {}, "agenda": {},
	"meeting_room": {}, "message_body": {}, "doc_title": {}, "followup_action": {},
}

var structuralValidator = validator.New(validator.WithRequiredStructEnabled())

// Candidate mirrors one item from the strict todo_extraction JSON schema.
type Candidate struct {
	ActionType         string         `json:"action_type" validate:"required,oneof=code_change summary_post investigate schedule_meeting reply_message doc_write manual_followup"`
	Title              string         `json:"title" validate:"required"`
	Description        string         `json:"description" validate:"required"`
	CommitmentStrength string         `json:"commitment_strength" validate:"required,oneof=firm tentative mentioned"`
	AssignerOpenID     *string        `json:"assigner_open_id"`
	ProjectHint        *string        `json:"project_hint"`
	DueDate            *string        `json:"due_date"`
	SourceMessageIDs   []string       `json:"source_message_ids" validate:"required,min=1,dive,required"`
	SourceQuote        string         `json:"source_quote" validate:"required"`
	Slots              map[string]any `json:"slots" validate:"required"`
	InfoSufficient     bool           `json:"info_sufficient"`
	MissingInfo        []string       `json:"missing_info"`
}

type ExtractionResult struct {
	Candidates []Candidate `json:"candidates"`
}

// DecodeExtractionResult rejects unknown fields and trailing JSON before
// applying the domain validator. There is deliberately no permissive parser.
func DecodeExtractionResult(payload []byte) (*ExtractionResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result ExtractionResult
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decode JSON: %v", ErrInvalidExtraction, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidExtraction, err)
	}
	if result.Candidates == nil {
		return nil, fmt.Errorf("%w: candidates field is required", ErrInvalidExtraction)
	}
	for i := range result.Candidates {
		if err := validateStrictSlotShape(result.Candidates[i].Slots); err != nil {
			return nil, fmt.Errorf("%w: candidate[%d]: %v", ErrInvalidExtraction, i, err)
		}
		if err := ValidateCandidate(&result.Candidates[i]); err != nil {
			return nil, fmt.Errorf("%w: candidate[%d]: %v", ErrInvalidExtraction, i, err)
		}
	}
	return &result, nil
}

func validateStrictSlotShape(slots map[string]any) error {
	missing := make([]string, 0)
	for name := range allowedSlots {
		if _, ok := slots[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: strict slots missing: %s", ErrInvalidCandidate, strings.Join(missing, ","))
	}
	return nil
}

// ValidateCandidate enforces the closed action/slot vocabulary. Missing
// action-specific slots are retained as an explicit incomplete candidate.
func ValidateCandidate(candidate *Candidate) error {
	if candidate == nil {
		return fmt.Errorf("%w: candidate is nil", ErrInvalidCandidate)
	}
	if err := structuralValidator.Struct(candidate); err != nil {
		return fmt.Errorf("%w: structural validation: %v", ErrInvalidCandidate, err)
	}
	for _, field := range []struct {
		name  string
		value string
	}{{"title", candidate.Title}, {"description", candidate.Description}, {"source_quote", candidate.SourceQuote}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s must not be blank", ErrInvalidCandidate, field.name)
		}
	}
	if err := validateMessageIDs(candidate.SourceMessageIDs); err != nil {
		return err
	}
	if candidate.DueDate != nil {
		if _, err := time.Parse(time.DateOnly, *candidate.DueDate); err != nil {
			return fmt.Errorf("%w: due_date must use YYYY-MM-DD: %v", ErrInvalidCandidate, err)
		}
	}
	for name, value := range candidate.Slots {
		if _, ok := allowedSlots[name]; !ok {
			return fmt.Errorf("%w: unknown slot %q", ErrInvalidCandidate, name)
		}
		if err := validateSlotValue(name, value); err != nil {
			return err
		}
	}

	required, ok := requiredSlots[candidate.ActionType]
	if !ok {
		return fmt.Errorf("%w: unknown action_type %q", ErrInvalidCandidate, candidate.ActionType)
	}
	missing := append([]string(nil), candidate.MissingInfo...)
	for _, name := range required {
		if isEmptySlot(candidate.Slots[name]) {
			missing = append(missing, name)
		}
	}
	candidate.MissingInfo = normalizedStrings(missing)
	if len(candidate.MissingInfo) > 0 {
		candidate.InfoSufficient = false
	}
	if !candidate.InfoSufficient && len(candidate.MissingInfo) == 0 {
		return fmt.Errorf("%w: info_sufficient=false requires missing_info", ErrInvalidCandidate)
	}
	return nil
}

// Fingerprint returns the exact-dedup SHA256 defined by the M3 contract. An
// incomplete identity is rejected instead of inventing a collision-prone key;
// persistence policy for such candidates must be decided explicitly.
func Fingerprint(candidate *Candidate, projectID *uint64) (string, error) {
	if err := ValidateCandidate(candidate); err != nil {
		return "", err
	}
	identity, err := normalizedIdentitySlots(candidate)
	if err != nil {
		return "", err
	}
	payload := struct {
		ActionType    string         `json:"action_type"`
		ProjectID     *uint64        `json:"project_id"`
		IdentitySlots map[string]any `json:"identity_slots"`
	}{candidate.ActionType, projectID, identity}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode todo fingerprint: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

// SemanticText is the stable text embedded into todo_semantic. It deliberately
// includes both natural-language fields and normalized identity slots.
func SemanticText(candidate *Candidate) (string, error) {
	if err := ValidateCandidate(candidate); err != nil {
		return "", err
	}
	identity, err := normalizedIdentitySlots(candidate)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode Todo semantic identity: %w", err)
	}
	return strings.Join([]string{
		candidate.ActionType,
		strings.TrimSpace(candidate.Title),
		strings.TrimSpace(candidate.Description),
		string(encoded),
	}, "｜"), nil
}

func normalizedIdentitySlots(candidate *Candidate) (map[string]any, error) {
	names, ok := identitySlots[candidate.ActionType]
	if !ok {
		return nil, fmt.Errorf("%w: unknown action_type %q", ErrInvalidCandidate, candidate.ActionType)
	}
	identity := make(map[string]any, len(names))
	for _, name := range names {
		value := candidate.Slots[name]
		if isEmptySlot(value) {
			return nil, fmt.Errorf("%w: action_type=%s slot=%s", ErrFingerprintIncomplete, candidate.ActionType, name)
		}
		normalized, err := normalizeIdentityValue(name, value)
		if err != nil {
			return nil, err
		}
		identity[name] = normalized
	}
	return identity, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return fmt.Errorf("multiple JSON values are not allowed")
}

func validateMessageIDs(ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: source_message_ids contains blank value", ErrInvalidCandidate)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate source_message_id %q", ErrInvalidCandidate, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateSlotValue(name string, value any) error {
	if value == nil {
		return nil
	}
	switch name {
	case "assignees", "lookup_sources", "attendees":
		items, ok := stringSlice(value)
		if !ok {
			return fmt.Errorf("%w: slot %s must be an array of strings", ErrInvalidCandidate, name)
		}
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("%w: slot %s contains blank value", ErrInvalidCandidate, name)
			}
		}
		if name == "lookup_sources" {
			for _, item := range items {
				switch item {
				case "code", "web", "docs", "people":
				default:
					return fmt.Errorf("%w: unsupported lookup source %q", ErrInvalidCandidate, item)
				}
			}
		}
	case "duration_minutes":
		number, ok := value.(float64)
		if !ok || number <= 0 || math.Trunc(number) != number {
			return fmt.Errorf("%w: duration_minutes must be a positive integer", ErrInvalidCandidate)
		}
	default:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%w: slot %s must be a string or null", ErrInvalidCandidate, name)
		}
	}
	return nil
}

func isEmptySlot(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	default:
		return false
	}
}

func stringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		result := make([]string, len(typed))
		for i, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result[i] = text
		}
		return result, true
	default:
		return nil, false
	}
}

func normalizedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeIdentityValue(name string, value any) (any, error) {
	if items, ok := stringSlice(value); ok {
		normalized := make([]string, len(items))
		for i, item := range items {
			normalized[i] = normalizeText(item)
		}
		if name == "attendees" {
			sort.Strings(normalized)
		}
		return normalized, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("%w: identity slot %s has unsupported type %T", ErrInvalidCandidate, name, value)
	}
	return normalizeText(text), nil
}

func normalizeText(value string) string {
	value = norm.NFKC.String(value)
	value = cases.Fold().String(value)
	return strings.Join(strings.Fields(value), " ")
}
