package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/semantic"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type preparedCandidate struct {
	Candidate       Candidate
	Fingerprint     string
	AssignerOpenID  *string
	LeaderAssigned  bool
	DueAt           *time.Time
	FirstEvidenceAt time.Time
	LastEvidenceAt  time.Time
	MatchedTodoID   *uint64
	SemanticVector  []float32
	// ProjectID is the resolved project (group-bound has priority, else matched
	// from project_hint). It is authoritative for both the Todo column and the
	// dedup fingerprint so the same clue dedups stably across runs.
	ProjectID       *uint64
	Resolution      datatypes.JSON
	ContextSnapshot datatypes.JSON
}

func (s *PipelineStore) PersistChat(ctx context.Context, batch ChatBatch, results []UnitExtraction, modelName string) (PersistStats, error) {
	if batch.Group.ID == 0 || strings.TrimSpace(batch.Group.ChatID) == "" {
		return PersistStats{}, fmt.Errorf("persist extraction batch has invalid group")
	}
	if batch.LastNew.MessageID == "" || batch.LastNew.ChatID != batch.Group.ChatID || !batch.LastNew.IsNew {
		return PersistStats{}, fmt.Errorf("persist extraction batch has invalid last new message")
	}
	if strings.TrimSpace(modelName) == "" || len(modelName) > 64 {
		return PersistStats{}, fmt.Errorf("persist extraction model name must contain 1 to 64 bytes")
	}
	prepared, err := s.prepareResults(ctx, batch, results)
	if err != nil {
		return PersistStats{}, err
	}

	stats := PersistStats{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		records := make(map[uint64]semantic.Record, len(prepared))
		order := make([]uint64, 0, len(prepared))
		for i := range prepared {
			created, todo, err := s.persistCandidate(tx, batch, &prepared[i], modelName)
			if err != nil {
				return err
			}
			if created {
				stats.Created++
			} else {
				stats.Updated++
			}
			if _, exists := records[todo.ID]; !exists {
				order = append(order, todo.ID)
			}
			records[todo.ID] = semantic.Record{
				TodoID: todo.ID, Fingerprint: todo.DedupFingerprint, ProjectID: copyUint64(todo.ProjectID),
				Status: todo.Status, ActionType: todo.ActionType, Vector: append([]float32(nil), prepared[i].SemanticVector...),
			}
		}
		watermark := domain.TodoExtractWatermark{
			ChatID: batch.Group.ChatID, LastScannedMessageID: batch.LastNew.MessageID,
			LastScannedAt: time.UnixMilli(batch.LastNew.CreateTime).In(s.location),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_scanned_message_id", "last_scanned_at", "updated_at"}),
		}).Create(&watermark).Error; err != nil {
			return fmt.Errorf("advance extract watermark chat_id=%s message_id=%s: %w", batch.Group.ChatID, batch.LastNew.MessageID, err)
		}
		semanticRecords := make([]semantic.Record, 0, len(order))
		for _, todoID := range order {
			semanticRecords = append(semanticRecords, records[todoID])
		}
		// Qdrant is called at the end of the MySQL transaction so a sync failure
		// rolls back Todo/Event/watermark together. There is no silent outbox fallback.
		if err := s.semantic.Upsert(ctx, semanticRecords); err != nil {
			return fmt.Errorf("sync Todo semantic index: %w", err)
		}
		return nil
	})
	if err != nil {
		return PersistStats{}, err
	}
	return stats, nil
}

func (s *PipelineStore) prepareResults(ctx context.Context, batch ChatBatch, results []UnitExtraction) ([]preparedCandidate, error) {
	units := make(map[string]ConversationUnit, len(batch.Units))
	for _, unit := range batch.Units {
		if _, exists := units[unit.Key]; exists {
			return nil, fmt.Errorf("duplicate conversation unit key %q", unit.Key)
		}
		units[unit.Key] = unit
	}
	seenResults := make(map[string]struct{}, len(results))
	prepared := make([]preparedCandidate, 0)
	for _, result := range results {
		unit, ok := units[result.UnitKey]
		if !ok {
			return nil, fmt.Errorf("extraction result references unknown unit %q", result.UnitKey)
		}
		if _, exists := seenResults[result.UnitKey]; exists {
			return nil, fmt.Errorf("duplicate extraction result for unit %q", result.UnitKey)
		}
		seenResults[result.UnitKey] = struct{}{}
		for i := range result.Candidates {
			candidate, err := s.prepareCandidate(ctx, batch, unit, result.Candidates[i].Candidate, result.Memories)
			if err != nil {
				return nil, fmt.Errorf("prepare candidate unit=%s index=%d: %w", unit.Key, i, err)
			}
			if len(result.Candidates[i].Semantic.Vector) == 0 {
				return nil, fmt.Errorf("prepare candidate unit=%s index=%d: semantic vector is empty", unit.Key, i)
			}
			candidate.MatchedTodoID = copyUint64(result.Candidates[i].Semantic.MatchedTodoID)
			candidate.SemanticVector = append([]float32(nil), result.Candidates[i].Semantic.Vector...)
			prepared = append(prepared, *candidate)
		}
	}
	if len(seenResults) != len(units) {
		missing := make([]string, 0)
		for key := range units {
			if _, ok := seenResults[key]; !ok {
				missing = append(missing, key)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("extraction results missing units: %s", strings.Join(missing, ","))
	}
	return prepared, nil
}

func (s *PipelineStore) prepareCandidate(ctx context.Context, batch ChatBatch, unit ConversationUnit, candidate Candidate, memories []map[string]any) (*preparedCandidate, error) {
	if err := validateStrictSlotShape(candidate.Slots); err != nil {
		return nil, err
	}
	if err := ValidateCandidate(&candidate); err != nil {
		return nil, err
	}
	if err := validateCandidateEvidence(unit, &candidate); err != nil {
		return nil, err
	}
	projectID, resolution := resolveProject(batch, candidate)
	resolutionRaw, err := resolution.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode resolution: %w", err)
	}
	resolutionJSON := datatypes.JSON(resolutionRaw)
	// The resolved project_id (not just the group-bound one) is authoritative for
	// the fingerprint so codex-inferred attribution dedups stably across runs.
	fingerprint, err := Fingerprint(&candidate, projectID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.buildContextSnapshot(ctx, batch, unit, candidate, projectID, memories)
	if err != nil {
		return nil, err
	}
	snapshotRaw, err := snapshot.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode context snapshot: %w", err)
	}
	snapshotJSON := datatypes.JSON(snapshotRaw)

	byID := make(map[string]MessageContext, len(unit.Messages))
	for _, message := range unit.Messages {
		byID[message.MessageID] = message
	}
	first := time.Time{}
	last := time.Time{}
	leaders := make(map[string]struct{})
	for _, messageID := range candidate.SourceMessageIDs {
		message := byID[messageID]
		at := time.UnixMilli(message.CreateTime).In(s.location)
		if first.IsZero() || at.Before(first) {
			first = at
		}
		if last.IsZero() || at.After(last) {
			last = at
		}
		if message.IsLeader {
			leaders[message.SenderOpenID] = struct{}{}
		}
	}
	assigner := copyString(candidate.AssignerOpenID)
	if len(leaders) > 0 {
		if assigner == nil {
			if len(leaders) != 1 {
				return nil, fmt.Errorf("%w: multiple leader sources require assigner_open_id", ErrInvalidCandidate)
			}
			for openID := range leaders {
				assigner = &openID
			}
		} else if _, ok := leaders[*assigner]; !ok {
			return nil, fmt.Errorf("%w: assigner_open_id %q does not match cited leader source", ErrInvalidCandidate, *assigner)
		}
	}
	var dueAt *time.Time
	if candidate.DueDate != nil {
		parsed, err := time.ParseInLocation(time.DateOnly, *candidate.DueDate, s.location)
		if err != nil {
			return nil, fmt.Errorf("parse todo due date: %w", err)
		}
		dueAt = &parsed
	}
	return &preparedCandidate{
		Candidate: candidate, Fingerprint: fingerprint, AssignerOpenID: assigner,
		LeaderAssigned: len(leaders) > 0, DueAt: dueAt, FirstEvidenceAt: first, LastEvidenceAt: last,
		ProjectID: projectID, Resolution: resolutionJSON, ContextSnapshot: snapshotJSON,
	}, nil
}

func (s *PipelineStore) persistCandidate(tx *gorm.DB, batch ChatBatch, prepared *preparedCandidate, modelName string) (bool, *domain.Todo, error) {
	var existing domain.Todo
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"})
	if prepared.MatchedTodoID != nil {
		query = query.Where("id = ?", *prepared.MatchedTodoID)
	} else {
		query = query.Where("dedup_fingerprint = ?", prepared.Fingerprint)
	}
	result := query.Limit(1).Find(&existing)
	switch {
	case result.Error != nil:
		return false, nil, fmt.Errorf("find Todo for candidate fingerprint=%s: %w", prepared.Fingerprint, result.Error)
	case result.RowsAffected == 1:
		if prepared.MatchedTodoID != nil {
			if existing.ActionType != prepared.Candidate.ActionType || !sameUint64(existing.ProjectID, prepared.ProjectID) {
				return false, nil, fmt.Errorf("semantic match todo_id=%d changed domain before persistence", existing.ID)
			}
			if _, active := activeTodoStatuses[existing.Status]; !active {
				return false, nil, fmt.Errorf("semantic match todo_id=%d became inactive with status=%s", existing.ID, existing.Status)
			}
		}
		if err := s.updateTodo(tx, &existing, prepared, modelName); err != nil {
			return false, nil, err
		}
		if err := tx.Where("id = ?", existing.ID).Take(&existing).Error; err != nil {
			return false, nil, fmt.Errorf("reload updated Todo id=%d: %w", existing.ID, err)
		}
		return false, &existing, nil
	case result.RowsAffected == 0:
		if prepared.MatchedTodoID != nil {
			return false, nil, fmt.Errorf("semantic match todo_id=%d no longer exists", *prepared.MatchedTodoID)
		}
		return s.createTodo(tx, batch, prepared, modelName)
	default:
		return false, nil, fmt.Errorf("find Todo fingerprint=%s returned rows=%d, want 0 or 1", prepared.Fingerprint, result.RowsAffected)
	}
}

func (s *PipelineStore) createTodo(tx *gorm.DB, batch ChatBatch, prepared *preparedCandidate, modelName string) (bool, *domain.Todo, error) {
	slots, err := json.Marshal(prepared.Candidate.Slots)
	if err != nil {
		return false, nil, fmt.Errorf("encode todo slots: %w", err)
	}
	sourceIDs, err := json.Marshal(prepared.Candidate.SourceMessageIDs)
	if err != nil {
		return false, nil, fmt.Errorf("encode todo source message IDs: %w", err)
	}
	missingInfo, err := json.Marshal(prepared.Candidate.MissingInfo)
	if err != nil {
		return false, nil, fmt.Errorf("encode todo missing info: %w", err)
	}
	todo := domain.Todo{
		Title: prepared.Candidate.Title, Description: prepared.Candidate.Description,
		ActionType: prepared.Candidate.ActionType, Slots: datatypes.JSON(slots),
		CommitmentStrength: prepared.Candidate.CommitmentStrength,
		SourceMessageIDs:   datatypes.JSON(sourceIDs), SourceQuote: prepared.Candidate.SourceQuote,
		GroupID: &batch.Group.ID, ProjectID: prepared.ProjectID,
		AssignerOpenID: prepared.AssignerOpenID, IsLeaderAssigned: prepared.LeaderAssigned,
		DueAt: prepared.DueAt, Status: "extracted", MissingInfo: datatypes.JSON(missingInfo),
		DedupFingerprint: prepared.Fingerprint, ExtractionModel: modelName, PromptVersion: PromptVersion,
		Resolution: prepared.Resolution, ContextSnapshot: prepared.ContextSnapshot,
		Revision: 1, Version: 0, FirstSeenAt: prepared.FirstEvidenceAt, LastEvidenceAt: prepared.LastEvidenceAt,
	}
	if err := tx.Create(&todo).Error; err != nil {
		return false, nil, fmt.Errorf("create todo fingerprint=%s: %w", prepared.Fingerprint, err)
	}
	detail, err := eventDetail("created", todo.Revision, prepared.Candidate.SourceMessageIDs)
	if err != nil {
		return false, nil, err
	}
	event := domain.TodoEvent{TodoID: todo.ID, ToStatus: "extracted", Actor: "m3", Detail: detail}
	if err := tx.Create(&event).Error; err != nil {
		return false, nil, fmt.Errorf("create todo event todo_id=%d: %w", todo.ID, err)
	}
	return true, &todo, nil
}

func (s *PipelineStore) updateTodo(tx *gorm.DB, existing *domain.Todo, prepared *preparedCandidate, modelName string) error {
	existingSlots := make(map[string]any)
	if err := json.Unmarshal(existing.Slots, &existingSlots); err != nil {
		return fmt.Errorf("decode existing todo slots todo_id=%d: %w", existing.ID, err)
	}
	for name, value := range prepared.Candidate.Slots {
		if !isEmptySlot(value) {
			existingSlots[name] = value
		}
	}
	mergedCandidate := prepared.Candidate
	mergedCandidate.Slots = existingSlots
	filteredMissing := make([]string, 0, len(mergedCandidate.MissingInfo))
	for _, name := range mergedCandidate.MissingInfo {
		if _, isSlot := allowedSlots[name]; isSlot && !isEmptySlot(existingSlots[name]) {
			continue
		}
		filteredMissing = append(filteredMissing, name)
	}
	mergedCandidate.MissingInfo = filteredMissing
	if len(filteredMissing) == 0 {
		mergedCandidate.InfoSufficient = true
	}
	if err := ValidateCandidate(&mergedCandidate); err != nil {
		return fmt.Errorf("validate merged todo todo_id=%d: %w", existing.ID, err)
	}

	var existingIDs []string
	if err := json.Unmarshal(existing.SourceMessageIDs, &existingIDs); err != nil {
		return fmt.Errorf("decode existing source IDs todo_id=%d: %w", existing.ID, err)
	}
	mergedIDs := mergeStrings(existingIDs, prepared.Candidate.SourceMessageIDs)
	slots, err := json.Marshal(existingSlots)
	if err != nil {
		return fmt.Errorf("encode merged todo slots todo_id=%d: %w", existing.ID, err)
	}
	sourceIDs, err := json.Marshal(mergedIDs)
	if err != nil {
		return fmt.Errorf("encode merged source IDs todo_id=%d: %w", existing.ID, err)
	}
	missingInfo, err := json.Marshal(mergedCandidate.MissingInfo)
	if err != nil {
		return fmt.Errorf("encode merged missing info todo_id=%d: %w", existing.ID, err)
	}
	updates := map[string]any{
		"title": prepared.Candidate.Title, "description": prepared.Candidate.Description,
		"slots": datatypes.JSON(slots), "commitment_strength": prepared.Candidate.CommitmentStrength,
		"source_message_ids": datatypes.JSON(sourceIDs), "source_quote": prepared.Candidate.SourceQuote,
		"is_leader_assigned": existing.IsLeaderAssigned || prepared.LeaderAssigned,
		"missing_info":       datatypes.JSON(missingInfo), "extraction_model": modelName,
		"prompt_version": PromptVersion, "revision": existing.Revision + 1,
		"last_evidence_at": maxTime(existing.LastEvidenceAt, prepared.LastEvidenceAt),
		"version":          gorm.Expr("version + 1"),
		// Refresh the frozen snapshot/resolution on new evidence so M4/M5 always
		// replay the latest background for this clue.
		"context_snapshot": prepared.ContextSnapshot,
		"resolution":       prepared.Resolution,
	}
	if prepared.AssignerOpenID != nil {
		updates["assigner_open_id"] = *prepared.AssignerOpenID
	}
	if prepared.DueAt != nil {
		updates["due_at"] = *prepared.DueAt
	}
	result := tx.Model(&domain.Todo{}).Where("id = ? AND version = ?", existing.ID, existing.Version).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update todo id=%d: %w", existing.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("update todo id=%d optimistic lock affected=%d, want 1", existing.ID, result.RowsAffected)
	}
	detail, err := eventDetail("evidence_updated", existing.Revision+1, prepared.Candidate.SourceMessageIDs)
	if err != nil {
		return err
	}
	status := existing.Status
	event := domain.TodoEvent{
		TodoID: existing.ID, FromStatus: &status, ToStatus: existing.Status, Actor: "m3", Detail: detail,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("create todo update event todo_id=%d: %w", existing.ID, err)
	}
	return nil
}

func eventDetail(eventType string, revision int32, messageIDs []string) (datatypes.JSON, error) {
	encoded, err := json.Marshal(map[string]any{
		"event_type": eventType, "revision": revision, "source_message_ids": messageIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("encode todo event detail: %w", err)
	}
	return datatypes.JSON(encoded), nil
}

func mergeStrings(existing, incoming []string) []string {
	result := append([]string(nil), existing...)
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range incoming {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func maxTime(first, second time.Time) time.Time {
	if second.After(first) {
		return second
	}
	return first
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
