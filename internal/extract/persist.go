package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/semantic"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jarvis/internal/datatypes"
)

type preparedCandidate struct {
	Candidate       Candidate
	Fingerprint     string
	AssignerOpenID  *string
	LeaderAssigned  bool
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
	// ExtractionResult 是抽取吐出的完整结论原文（整个 Candidate 的 JSON），随 Todo
	// 落库并随 Task 固化，供 M5 执行整块复用，避免下游逐字段拷贝抽取结构造成耦合。
	ExtractionResult datatypes.JSON
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
	prepared, skipped, err := s.prepareResults(ctx, batch, results)
	if err != nil {
		return PersistStats{}, err
	}

	stats := PersistStats{Skipped: skipped}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		records := make(map[uint64]semantic.Record, len(prepared))
		todoRefs := make(map[uint64]TodoRef, len(prepared))
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
			todoRefs[todo.ID] = TodoRef{ID: todo.ID, Version: todo.Version, Status: todo.Status}
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
			stats.Todos = append(stats.Todos, todoRefs[todoID])
		}
		// Qdrant is called at the end of the database transaction so a sync failure
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

func (s *PipelineStore) prepareResults(ctx context.Context, batch ChatBatch, results []UnitExtraction) ([]preparedCandidate, int, error) {
	units := make(map[string]ConversationUnit, len(batch.Units))
	for _, unit := range batch.Units {
		if _, exists := units[unit.Key]; exists {
			return nil, 0, fmt.Errorf("duplicate conversation unit key %q", unit.Key)
		}
		units[unit.Key] = unit
	}
	seenResults := make(map[string]struct{}, len(results))
	prepared := make([]preparedCandidate, 0)
	skipped := 0
	for _, result := range results {
		unit, ok := units[result.UnitKey]
		if !ok {
			return nil, 0, fmt.Errorf("extraction result references unknown unit %q", result.UnitKey)
		}
		if _, exists := seenResults[result.UnitKey]; exists {
			return nil, 0, fmt.Errorf("duplicate extraction result for unit %q", result.UnitKey)
		}
		seenResults[result.UnitKey] = struct{}{}
		for i := range result.Candidates {
			candidate, err := s.prepareCandidate(ctx, batch, unit, result.Candidates[i].Candidate, result.Facts)
			if err != nil {
				// target is a required field, so a missing dedup identity is a hard
				// contract violation from the model, not a skip. Fail fast.
				return nil, 0, fmt.Errorf("prepare candidate unit=%s index=%d: %w", unit.Key, i, err)
			}
			if len(result.Candidates[i].Semantic.Vector) == 0 {
				return nil, 0, fmt.Errorf("prepare candidate unit=%s index=%d: semantic vector is empty", unit.Key, i)
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
		return nil, 0, fmt.Errorf("extraction results missing units: %s", strings.Join(missing, ","))
	}
	return prepared, skipped, nil
}

func (s *PipelineStore) prepareCandidate(ctx context.Context, batch ChatBatch, unit ConversationUnit, candidate Candidate, facts []contextsnap.Fact) (*preparedCandidate, error) {
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
	var assigner *string
	if len(leaders) == 1 {
		for openID := range leaders {
			assigner = &openID
		}
	}
	snapshot, err := s.buildContextSnapshot(ctx, batch, unit, candidate, projectID, assigner, facts)
	if err != nil {
		return nil, err
	}
	snapshotRaw, err := snapshot.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode context snapshot: %w", err)
	}
	snapshotJSON := datatypes.JSON(snapshotRaw)

	extractionRaw, err := json.Marshal(candidate)
	if err != nil {
		return nil, fmt.Errorf("encode extraction result: %w", err)
	}
	extractionJSON := datatypes.JSON(extractionRaw)

	return &preparedCandidate{
		Candidate: candidate, Fingerprint: fingerprint, AssignerOpenID: assigner,
		LeaderAssigned: len(leaders) > 0, FirstEvidenceAt: first, LastEvidenceAt: last,
		ProjectID: projectID, Resolution: resolutionJSON, ContextSnapshot: snapshotJSON,
		ExtractionResult: extractionJSON,
	}, nil
}

func (s *PipelineStore) persistCandidate(tx *gorm.DB, batch ChatBatch, prepared *preparedCandidate, modelName string) (bool, *domain.Todo, error) {
	var existing domain.Todo
	query := tx
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
	sourceIDs, err := json.Marshal(prepared.Candidate.SourceMessageIDs)
	if err != nil {
		return false, nil, fmt.Errorf("encode todo source message IDs: %w", err)
	}
	todo := domain.Todo{
		Title: prepared.Candidate.Title, Description: prepared.Candidate.Payload,
		ActionType: prepared.Candidate.ActionType, Target: prepared.Candidate.Target,
		Context: "", OpenQuestions: datatypes.JSON(`[]`), CommitmentStrength: "",
		SourceMessageIDs: datatypes.JSON(sourceIDs), SourceQuote: prepared.Candidate.SourceQuote,
		GroupID: &batch.Group.ID, ProjectID: prepared.ProjectID,
		AssignerOpenID: prepared.AssignerOpenID, IsLeaderAssigned: prepared.LeaderAssigned,
		Status:           prepared.Candidate.Status,
		DedupFingerprint: prepared.Fingerprint,
		Resolution:       prepared.Resolution, ContextSnapshot: prepared.ContextSnapshot,
		ExtractionResult: prepared.ExtractionResult,
		Revision:         1, Version: 0, FirstSeenAt: prepared.FirstEvidenceAt, LastEvidenceAt: prepared.LastEvidenceAt,
	}
	if err := tx.Create(&todo).Error; err != nil {
		return false, nil, fmt.Errorf("create todo fingerprint=%s: %w", prepared.Fingerprint, err)
	}
	detail, err := eventDetail("created", todo.Revision, prepared.Candidate.SourceMessageIDs)
	if err != nil {
		return false, nil, err
	}
	snapshot, err := domain.EncodeTodoEventSnapshot(&todo)
	if err != nil {
		return false, nil, err
	}
	event := domain.TodoEvent{
		TodoID: todo.ID, ToStatus: todo.Status, Actor: "m3",
		Detail: detail, Snapshot: snapshot,
	}
	if err := tx.Create(&event).Error; err != nil {
		return false, nil, fmt.Errorf("create todo event todo_id=%d: %w", todo.ID, err)
	}
	return true, &todo, nil
}

func (s *PipelineStore) updateTodo(tx *gorm.DB, existing *domain.Todo, prepared *preparedCandidate, modelName string) error {
	if err := ValidateCandidate(&prepared.Candidate); err != nil {
		return fmt.Errorf("validate merged todo todo_id=%d: %w", existing.ID, err)
	}

	var existingIDs []string
	if err := json.Unmarshal(existing.SourceMessageIDs, &existingIDs); err != nil {
		return fmt.Errorf("decode existing source IDs todo_id=%d: %w", existing.ID, err)
	}
	mergedIDs := mergeStrings(existingIDs, prepared.Candidate.SourceMessageIDs)
	sourceIDs, err := json.Marshal(mergedIDs)
	if err != nil {
		return fmt.Errorf("encode merged source IDs todo_id=%d: %w", existing.ID, err)
	}
	// target is the dedup identity, so it is stable across re-extractions. The
	// latest opaque payload replaces the previous semantic body verbatim.
	updates := map[string]any{
		"title": prepared.Candidate.Title, "description": prepared.Candidate.Payload,
		"target": prepared.Candidate.Target, "context": "",
		"open_questions": datatypes.JSON(`[]`), "commitment_strength": "",
		"source_message_ids": datatypes.JSON(sourceIDs), "source_quote": prepared.Candidate.SourceQuote,
		"is_leader_assigned": existing.IsLeaderAssigned || prepared.LeaderAssigned,
		"revision":           existing.Revision + 1,
		"last_evidence_at":   maxTime(existing.LastEvidenceAt, prepared.LastEvidenceAt),
		"version":            gorm.Expr("version + 1"),
		// Refresh the frozen snapshot/resolution/extraction on new evidence so
		// M5 can query the latest creation-time evidence for this clue.
		"context_snapshot":  prepared.ContextSnapshot,
		"extraction_result": prepared.ExtractionResult,
		"resolution":        prepared.Resolution,
	}
	if prepared.AssignerOpenID != nil {
		updates["assigner_open_id"] = *prepared.AssignerOpenID
	}
	// The latest extraction wins on status, so new evidence can promote an
	// observing clue into materialization or demote one that turned out to need
	// nobody. This only applies while the clue still sits in a state M3 owns:
	// once it has been materialized, re-extraction must not reset it — resetting
	// a materialized Todo would try to mint a duplicate Task.
	nextStatus := existing.Status
	if m3OwnedTodoStatuses[existing.Status] && prepared.Candidate.Status != existing.Status {
		nextStatus = prepared.Candidate.Status
		updates["status"] = nextStatus
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
	var updated domain.Todo
	if err := tx.First(&updated, existing.ID).Error; err != nil {
		return fmt.Errorf("reload updated todo id=%d for event snapshot: %w", existing.ID, err)
	}
	snapshot, err := domain.EncodeTodoEventSnapshot(&updated)
	if err != nil {
		return err
	}
	event := domain.TodoEvent{
		TodoID: existing.ID, FromStatus: &status, ToStatus: nextStatus,
		Actor: "m3", Detail: detail, Snapshot: snapshot,
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
