package background

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
)

var okrEvidenceSourcePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

const maxUnassociatedOKREvidenceLimit = 100

// UnassociatedOKREvidenceFilter narrows captured evidence before an Agent
// deliberately chooses the smallest matching OKR entity. Time is the local
// capture timestamp; Anchor is a literal token searched across stable message
// identity and readable content fields.
type UnassociatedOKREvidenceFilter struct {
	Source string
	Anchor string
	From   *time.Time
	Until  *time.Time
	Limit  int
}

// UnassociatedOKREvidenceItem is read-only source material. It intentionally
// carries no suggested subject: association remains an explicit Agent choice.
type UnassociatedOKREvidenceItem struct {
	EvidenceMessageID string    `json:"evidence_message_id"`
	EvidenceRowID     uint64    `json:"evidence_row_id"`
	SourceKind        string    `json:"source_kind"`
	ChatID            string    `json:"chat_id"`
	SenderOpenID      string    `json:"sender_open_id"`
	SenderName        string    `json:"sender_name"`
	Content           string    `json:"content"`
	CapturedAt        time.Time `json:"captured_at"`
}

type UnassociatedOKREvidenceResult struct {
	Items []UnassociatedOKREvidenceItem `json:"items"`
	Count int                           `json:"count"`
}

// OKREvidenceInput associates one already captured, read-only Message with the
// smallest applicable OKR entity. The proposed Page content is a complete
// current conclusion and therefore keeps the existing CAS contract.
type OKREvidenceInput struct {
	SubjectType      string    `json:"subject_type"`
	SubjectID        uint64    `json:"subject_id"`
	EvidenceMessage  string    `json:"evidence_message_id"`
	Description      string    `json:"description"`
	OccurredAt       time.Time `json:"occurred_at"`
	Content          string    `json:"content"`
	IfUnchangedSince time.Time `json:"if_unchanged_since"`
}

// OKREvidenceResult exposes both durable writes and their source identity so a
// caller can read them back without turning the evidence into a Task.
type OKREvidenceResult struct {
	EvidenceMessageID string            `json:"evidence_message_id"`
	EvidenceRowID     uint64            `json:"evidence_row_id"`
	SourceKind        string            `json:"source_kind"`
	Fact              progress.FactView `json:"fact"`
	Page              *PageView         `json:"page"`
}

// OKREvidenceConflictError means the evidence Fact was safely associated but
// the current conclusion changed before the Page CAS. Current is returned for
// a deliberate merge/retry.
type OKREvidenceConflictError struct {
	Result *OKREvidenceResult
}

func (e *OKREvidenceConflictError) Error() string {
	return "evidence fact was recorded, but page changed since if_unchanged_since; reload and merge"
}

func (e *OKREvidenceConflictError) Unwrap() error { return ErrConflict }

// ApplyOKREvidence is the single safe write boundary for evidence-driven OKR
// progress. The captured Message is read-only, Fact is append-only/idempotent,
// and Page remains the CAS-protected current conclusion.
func (s *PageService) ApplyOKREvidence(ctx context.Context, in OKREvidenceInput) (*OKREvidenceResult, error) {
	in.SubjectType = strings.TrimSpace(strings.ToLower(in.SubjectType))
	in.EvidenceMessage = strings.TrimSpace(in.EvidenceMessage)
	in.Description = strings.TrimSpace(in.Description)
	if !isOKREvidenceSubject(in.SubjectType) {
		return nil, invalid(fmt.Errorf("subject_type must be okr, project or key_matter"))
	}
	if in.SubjectID == 0 || in.EvidenceMessage == "" || in.Description == "" || in.OccurredAt.IsZero() || in.IfUnchangedSince.IsZero() {
		return nil, invalid(fmt.Errorf("positive subject_id, evidence_message_id, description, occurred_at and if_unchanged_since are required"))
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, invalid(fmt.Errorf("content must contain the complete current conclusion"))
	}
	if err := validateSummary(in.Content); err != nil {
		return nil, invalid(err)
	}
	if _, err := s.loadPage(ctx, in.SubjectType, in.SubjectID); err != nil {
		return nil, err
	}

	var message domain.Message
	err := s.db.WithContext(ctx).Where("message_id = ?", in.EvidenceMessage).Take(&message).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, invalid(fmt.Errorf("evidence_message_id %q is not a captured message", in.EvidenceMessage))
	}
	if err != nil {
		return nil, fmt.Errorf("load evidence message %q: %w", in.EvidenceMessage, err)
	}
	sourceKind := evidenceSourceKind(&message)
	fact, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: in.SubjectType,
		SubjectID:   in.SubjectID,
		Description: in.Description,
		OccurredAt:  &in.OccurredAt,
		SourceKind:  &sourceKind,
		SourceID:    &message.ID,
	})
	if err != nil {
		return nil, err
	}
	result := &OKREvidenceResult{
		EvidenceMessageID: message.MessageID,
		EvidenceRowID:     message.ID,
		SourceKind:        sourceKind,
		Fact:              *fact,
	}

	current, err := s.GetPage(ctx, in.SubjectType, in.SubjectID)
	if err != nil {
		return nil, err
	}
	// A replay after a successful write is a complete idempotent success even
	// when it carries the original CAS timestamp.
	if current.Summary == in.Content {
		result.Page = current
		return result, nil
	}
	page, err := s.UpdatePage(ctx, in.SubjectType, in.SubjectID, UpdatePageInput{
		Content: in.Content, IfUnchangedSince: in.IfUnchangedSince,
	})
	if err != nil {
		var conflict *PageConflictError
		if errors.As(err, &conflict) {
			result.Page = conflict.Current
			return nil, &OKREvidenceConflictError{Result: result}
		}
		return nil, err
	}
	result.Page = page
	return result, nil
}

// ListUnassociatedOKREvidence returns captured Messages that have not yet been
// used as source-traceable Facts for any OKR, Project or KeyMatter. It neither
// guesses a target nor creates a Task; callers select a returned message and
// pass its evidence_message_id to ApplyOKREvidence.
func (s *PageService) ListUnassociatedOKREvidence(ctx context.Context, filter UnassociatedOKREvidenceFilter) (*UnassociatedOKREvidenceResult, error) {
	filter.Source = strings.TrimSpace(filter.Source)
	filter.Anchor = strings.TrimSpace(filter.Anchor)
	if filter.Limit < 1 || filter.Limit > maxUnassociatedOKREvidenceLimit {
		return nil, invalid(fmt.Errorf("limit must be between 1 and %d", maxUnassociatedOKREvidenceLimit))
	}
	if filter.Source != "" && filter.Source != "message" && !okrEvidenceSourcePattern.MatchString(filter.Source) {
		return nil, invalid(fmt.Errorf("source must be message or a lowercase snake_case clue source"))
	}
	if filter.From != nil && filter.Until != nil && !filter.From.Before(*filter.Until) {
		return nil, invalid(fmt.Errorf("from must be before until"))
	}

	query := s.db.WithContext(ctx).Model(&domain.Message{}).Where(`NOT EXISTS (
		SELECT 1 FROM fact AS okr_evidence_fact
		WHERE okr_evidence_fact.source_id = message.id
		  AND okr_evidence_fact.subject_type IN ?
		  AND okr_evidence_fact.source_kind = CASE
			WHEN message.chat_mode = ? AND message.chat_id LIKE ? AND length(trim(substr(message.chat_id, 6))) > 0
			THEN substr(message.chat_id, 6)
			ELSE ?
		  END
	)`, []string{PageTypeOKR, PageTypeProject, PageTypeKeyMatter}, "clue", "clue:%", "message")

	if filter.Source == "message" {
		query = query.Where(`NOT (chat_mode = ? AND chat_id LIKE ? AND length(trim(substr(chat_id, 6))) > 0)`, "clue", "clue:%")
	} else if filter.Source != "" {
		query = query.Where("chat_mode = ? AND chat_id = ?", "clue", "clue:"+filter.Source)
	}
	if filter.From != nil {
		query = query.Where("create_time >= ?", filter.From.UTC().UnixMilli())
	}
	if filter.Until != nil {
		query = query.Where("create_time < ?", filter.Until.UTC().UnixMilli())
	}
	if filter.Anchor != "" {
		like := "%" + escapeSQLiteLike(filter.Anchor) + "%"
		query = query.Where(`(content LIKE ? ESCAPE '\' OR message_id LIKE ? ESCAPE '\' OR chat_id LIKE ? ESCAPE '\' OR sender_name LIKE ? ESCAPE '\')`, like, like, like, like)
	}

	var rows []domain.Message
	if err := query.Order("create_time DESC, id DESC").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list unassociated OKR evidence: %w", err)
	}
	items := make([]UnassociatedOKREvidenceItem, len(rows))
	for i := range rows {
		items[i] = UnassociatedOKREvidenceItem{
			EvidenceMessageID: rows[i].MessageID,
			EvidenceRowID:     rows[i].ID,
			SourceKind:        evidenceSourceKind(&rows[i]),
			ChatID:            rows[i].ChatID,
			SenderOpenID:      rows[i].SenderOpenID,
			SenderName:        rows[i].SenderName,
			Content:           rows[i].Content,
			CapturedAt:        time.UnixMilli(rows[i].CreateTime).UTC(),
		}
	}
	return &UnassociatedOKREvidenceResult{Items: items, Count: len(items)}, nil
}

func escapeSQLiteLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func isOKREvidenceSubject(subjectType string) bool {
	return subjectType == PageTypeOKR || subjectType == PageTypeProject || subjectType == PageTypeKeyMatter
}

func evidenceSourceKind(message *domain.Message) string {
	if message != nil && message.ChatMode == "clue" && strings.HasPrefix(message.ChatID, "clue:") {
		if source := strings.TrimSpace(strings.TrimPrefix(message.ChatID, "clue:")); source != "" {
			return source
		}
	}
	return "message"
}
