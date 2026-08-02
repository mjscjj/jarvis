package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/domain"
)

// ErrInvalidObservation marks an observation that fails the extraction contract.
var ErrInvalidObservation = errors.New("invalid observation")

// ObservationCandidate is something worth remembering that asks nothing of the
// principal: a decision a group reached, a fact someone stated, work another
// person owns, a constraint that surfaced in passing.
//
// It carries the same evidence discipline as Candidate — cited messages plus a
// verbatim quote — because an observation nobody can trace back is not worth
// keeping. It deliberately has no action_type, no desired_outcome and no
// commitment_strength: those fields only mean something for work, and offering
// them is what previously pushed "worth knowing" into the Todo pipeline.
type ObservationCandidate struct {
	// Subject names what this is about and doubles as the retrieval anchor.
	Subject string `json:"subject" validate:"required"`
	// Content is the observation itself, in natural language. Go never parses it.
	Content          string   `json:"content" validate:"required"`
	ProjectHint      *string  `json:"project_hint"`
	SourceMessageIDs []string `json:"source_message_ids" validate:"required,min=1,dive,required"`
	SourceQuote      string   `json:"source_quote" validate:"required"`
}

// ValidateObservation enforces the non-blank fields and the evidence rules.
func ValidateObservation(observation *ObservationCandidate) error {
	if observation == nil {
		return fmt.Errorf("%w: observation is nil", ErrInvalidObservation)
	}
	if err := structuralValidator.Struct(observation); err != nil {
		return fmt.Errorf("%w: structural validation: %v", ErrInvalidObservation, err)
	}
	for _, field := range []struct {
		name  string
		value string
	}{{"subject", observation.Subject}, {"content", observation.Content}, {"source_quote", observation.SourceQuote}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s must not be blank", ErrInvalidObservation, field.name)
		}
	}
	if err := validateMessageIDs(observation.SourceMessageIDs); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidObservation, err)
	}
	return nil
}

// ObservationDedupKey is the idempotency key for re-extraction. One message can
// legitimately yield several distinct observations, so identity has to include
// the content — unlike a Todo, where (action_type, project, target) is enough.
func ObservationDedupKey(observation *ObservationCandidate, projectID *uint64) (string, error) {
	if err := ValidateObservation(observation); err != nil {
		return "", err
	}
	payload := struct {
		Producer  string  `json:"producer"`
		ProjectID *uint64 `json:"project_id"`
		Subject   string  `json:"subject"`
		Content   string  `json:"content"`
		Quote     string  `json:"quote"`
	}{
		Producer:  domain.ObservationProducerM3,
		ProjectID: projectID,
		Subject:   normalizeText(observation.Subject),
		Content:   normalizeText(observation.Content),
		Quote:     normalizeText(observation.SourceQuote),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode observation dedup key: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
