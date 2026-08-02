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
	"regexp"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrInvalidExtraction     = errors.New("invalid extraction result")
	ErrInvalidCandidate      = errors.New("invalid todo candidate")
	ErrFingerprintIncomplete = errors.New("todo fingerprint identity is incomplete")
	// ErrEvidenceQuoteMismatch marks the self-correctable evidence failure where a
	// candidate's source_quote is not a verbatim contiguous substring of any cited
	// [new] message, so the worker can single it out for validation-feedback retry
	// (ask the model to re-extract without paraphrasing/splicing the quote).
	ErrEvidenceQuoteMismatch = errors.New("source_quote not a verbatim substring of cited [new] messages")
	// ErrEvidenceUnknownMessage marks the self-correctable evidence failure where
	// a candidate cites a source_message_id that does not exist in the chat at
	// all, i.e. the model invented the id. Evidence the model legitimately found
	// with its own tools is hydrated into the unit before validation, so a miss
	// here really means the id is not real.
	ErrEvidenceUnknownMessage = errors.New("source_message_id does not exist in this chat")
	// ErrEvidenceNoNewSource marks the self-correctable evidence failure where a
	// candidate cites only older context and no extractable [new] message, so the
	// clue is not actually grounded in what this round is extracting.
	ErrEvidenceNoNewSource = errors.New("candidate has no extractable [new] evidence")
)

// commonActionTypes lists the well-known clue kinds we surface to the model as
// guidance. action_type is an OPEN set: the model may emit any snake_case
// identifier (e.g. a novel intent, or "other") and downstream must accept it.
// M5 runs every known or novel type through the same execution/approval path,
// so the set stays advisory, not a closed enum.
var commonActionTypes = map[string]struct{}{
	"code_change": {}, "summary_post": {}, "investigate": {}, "schedule_meeting": {},
	"reply_message": {}, "doc_write": {}, "notify_principal": {}, "manual_followup": {}, "other": {},
}

// actionTypeIdentifier constrains an action_type to a lowercase snake_case
// token. This is a structural guard (stable key for dedup/routing), not a
// closed vocabulary—any well-formed identifier is accepted.
var actionTypeIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// IsKnownActionType reports whether value is one of the well-known common types.
// It no longer gates extraction (action_type is open); it is used where a
// caller wants to distinguish common types from novel ones.
func IsKnownActionType(value string) bool {
	_, ok := commonActionTypes[value]
	return ok
}

// IsValidActionType reports whether value is an acceptable action_type: any
// non-blank lowercase snake_case identifier.
func IsValidActionType(value string) bool {
	return actionTypeIdentifier.MatchString(strings.TrimSpace(value))
}

// Candidate is the small machine-consumed envelope between M3 and downstream.
// All model semantics live in Payload and are carried verbatim; Go only consumes
// the fields needed for routing, deduplication, project resolution and evidence.
type Candidate struct {
	ActionType string `json:"action_type"`
	// Status is the only control value M3 writes directly, and it is projected
	// verbatim onto Todo.status. It is deliberately limited to the two states M3
	// is entitled to pick between: extracted (needs an action, so it becomes a
	// Task) and observing (worth remembering, nobody acts on it). M3 must never
	// be able to reach downstream statuses directly.
	Status           string   `json:"status"`
	Title            string   `json:"title"`
	Target           string   `json:"target"`
	ProjectHint      *string  `json:"project_hint"`
	SourceMessageIDs []string `json:"source_message_ids"`
	SourceQuote      string   `json:"source_quote"`
	Payload          string   `json:"payload"`
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
		if err := ValidateCandidate(&result.Candidates[i]); err != nil {
			return nil, fmt.Errorf("%w: candidate[%d]: %v", ErrInvalidExtraction, i, err)
		}
	}
	return &result, nil
}

// ValidateCandidate validates only the machine-consumed envelope. Payload is
// intentionally opaque: it may be natural language or JSON text and is never
// parsed, normalized or projected into another DTO.
func ValidateCandidate(candidate *Candidate) error {
	if candidate == nil {
		return fmt.Errorf("%w: candidate is nil", ErrInvalidCandidate)
	}
	for _, field := range []struct {
		name  string
		value string
	}{{"status", candidate.Status}, {"title", candidate.Title}, {"target", candidate.Target}, {"source_quote", candidate.SourceQuote}, {"payload", candidate.Payload}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s must not be blank", ErrInvalidCandidate, field.name)
		}
	}
	if candidate.Status != "extracted" && candidate.Status != "observing" {
		return fmt.Errorf("%w: status %q must be extracted or observing", ErrInvalidCandidate, candidate.Status)
	}
	if !IsValidActionType(candidate.ActionType) {
		return fmt.Errorf("%w: action_type %q must be a lowercase snake_case identifier", ErrInvalidCandidate, candidate.ActionType)
	}
	if len(candidate.SourceMessageIDs) == 0 {
		return fmt.Errorf("%w: source_message_ids must not be empty", ErrInvalidCandidate)
	}
	if err := validateMessageIDs(candidate.SourceMessageIDs); err != nil {
		return err
	}
	return nil
}

// Fingerprint returns the exact-dedup SHA256 defined by the M3 contract. Identity
// is (action_type, project_id, normalized target). A blank target has no stable
// identity and is rejected; persistence policy for such a candidate is decided
// by the caller.
func Fingerprint(candidate *Candidate, projectID *uint64) (string, error) {
	if err := ValidateCandidate(candidate); err != nil {
		return "", err
	}
	identity := strings.TrimSpace(candidate.Target)
	if identity == "" {
		return "", fmt.Errorf("%w: action_type=%s target is blank", ErrFingerprintIncomplete, candidate.ActionType)
	}
	payload := struct {
		ActionType string  `json:"action_type"`
		ProjectID  *uint64 `json:"project_id"`
		Target     string  `json:"target"`
	}{candidate.ActionType, projectID, normalizeText(identity)}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode todo fingerprint: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

// SemanticText is the stable text embedded into todo_semantic. It joins the
// natural-language fields with the normalized target so semantically-equal clues
// (same action + subject + intent) cluster together.
func SemanticText(candidate *Candidate) (string, error) {
	if err := ValidateCandidate(candidate); err != nil {
		return "", err
	}
	return strings.Join([]string{
		candidate.ActionType,
		strings.TrimSpace(candidate.Title),
		strings.TrimSpace(candidate.Payload),
		normalizeText(candidate.Target),
	}, "｜"), nil
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

func normalizeText(value string) string {
	value = norm.NFKC.String(value)
	value = cases.Fold().String(value)
	return strings.Join(strings.Fields(value), " ")
}
