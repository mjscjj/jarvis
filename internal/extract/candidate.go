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
	"sort"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const PromptVersion = "todo-extraction-v3"

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
// M5 execution policy only special-cases code_change; every other type—known or
// novel—runs through the propose/approval gate, so the set stays advisory, not
// a closed enum.
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

var structuralValidator = validator.New(validator.WithRequiredStructEnabled())

// Candidate mirrors one item from the strict todo_extraction JSON schema.
//
// The clue's identity/details are carried by three general fields instead of a
// per-action_type slot vocabulary:
//   - Target: one-line subject/object of the clue, the stable dedup identity.
//   - Context: M3-enriched background (attribution, links, related history) that
//     the assistant gathered so downstream can act without re-digging.
//   - OpenQuestions: only the points M3 could not settle and that genuinely need
//     the principal to decide/supply; empty means the assistant handled it.
//
// Identity (Target) and completion (DesiredOutcome) are deliberately separate.
// A clue whose evidence is a blocker still names the real end state it serves,
// so clearing the blocker downstream never reads as finishing the clue.
type Candidate struct {
	ActionType string `json:"action_type" validate:"required"`
	Title      string `json:"title" validate:"required"`
	Target     string `json:"target" validate:"required"`
	// DesiredOutcome states what must be true in the real world before this clue
	// is finished. M5 receives it verbatim and checks completion against it.
	DesiredOutcome     string   `json:"desired_outcome" validate:"required"`
	Description        string   `json:"description" validate:"required"`
	Context            string   `json:"context"`
	OpenQuestions      []string `json:"open_questions"`
	CommitmentStrength string   `json:"commitment_strength" validate:"required,oneof=firm tentative mentioned"`
	AssignerOpenID     *string  `json:"assigner_open_id"`
	ProjectHint        *string  `json:"project_hint"`
	DueDate            *string  `json:"due_date"`
	SourceMessageIDs   []string `json:"source_message_ids" validate:"required,min=1,dive,required"`
	SourceQuote        string   `json:"source_quote" validate:"required"`
	// Semantics is an open pocket (natural language or JSON text) for anything
	// the model needs to carry that has no dedicated field: current blockers,
	// inference chain, candidate paths, follow-ups. Go never parses it; it rides
	// verbatim into extraction_result and on to M4 and M5. Adding a new kind of
	// reasoning here must not require widening this struct.
	Semantics string `json:"semantics"`
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

// ValidateCandidate enforces the closed action vocabulary and the non-blank
// natural-language fields. Identity now rests on target (not per-type slots).
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
	}{{"title", candidate.Title}, {"target", candidate.Target}, {"desired_outcome", candidate.DesiredOutcome}, {"description", candidate.Description}, {"source_quote", candidate.SourceQuote}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s must not be blank", ErrInvalidCandidate, field.name)
		}
	}
	if !IsValidActionType(candidate.ActionType) {
		return fmt.Errorf("%w: action_type %q must be a lowercase snake_case identifier", ErrInvalidCandidate, candidate.ActionType)
	}
	if err := validateMessageIDs(candidate.SourceMessageIDs); err != nil {
		return err
	}
	if candidate.DueDate != nil {
		if _, err := time.Parse(time.DateOnly, *candidate.DueDate); err != nil {
			return fmt.Errorf("%w: due_date must use YYYY-MM-DD: %v", ErrInvalidCandidate, err)
		}
	}
	for position, question := range candidate.OpenQuestions {
		if strings.TrimSpace(question) == "" {
			return fmt.Errorf("%w: open_questions[%d] must not be blank", ErrInvalidCandidate, position)
		}
	}
	candidate.OpenQuestions = normalizedStrings(candidate.OpenQuestions)
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
		strings.TrimSpace(candidate.Description),
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

func normalizeText(value string) string {
	value = norm.NFKC.String(value)
	value = cases.Fold().String(value)
	return strings.Join(strings.Fields(value), " ")
}
