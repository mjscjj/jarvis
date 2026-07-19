package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
)

// buildContextSnapshot freezes the background for one candidate at extraction
// time (docs/design-context-pipeline.md §2.2). It assembles from the already
// loaded ChatBatch/unit data; the only DB read is the full project detail when
// the project was resolved from a hint (the bound project detail is already in
// the batch). M4/M5 replay this exact snapshot without re-querying.
func (s *PipelineStore) buildContextSnapshot(ctx context.Context, batch ChatBatch, unit ConversationUnit, candidate Candidate, projectID *uint64, memories []map[string]any) (contextsnap.Snapshot, error) {
	snapshot := contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		CapturedAt:      s.now().UTC().Format(time.RFC3339),
		Principal:       snapshotPrincipal(batch.Principal),
		Group:           snapshotGroup(batch.Group),
		Messages:        snapshotMessages(unit, candidate),
		Memories:        memories,
	}
	if snapshot.Memories == nil {
		snapshot.Memories = make([]map[string]any, 0)
	}

	project, err := s.snapshotProject(ctx, batch, projectID)
	if err != nil {
		return contextsnap.Snapshot{}, err
	}
	snapshot.Project = project

	if candidate.AssignerOpenID != nil {
		snapshot.Assigner = snapshotAssigner(*candidate.AssignerOpenID, unit.Participants)
	}
	return snapshot, nil
}

// now returns the pipeline store clock. PipelineStore has no injected clock, so
// this uses time.Now; snapshots are for display/replay, not deterministic tests.
func (s *PipelineStore) now() time.Time { return time.Now() }

func snapshotPrincipal(principal *PrincipalContext) *contextsnap.Principal {
	if principal == nil {
		return nil
	}
	return &contextsnap.Principal{
		OpenID:       principal.OpenID,
		Name:         principal.Name,
		Department:   nonEmptyPtr(principal.Department),
		Title:        nonEmptyPtr(principal.Title),
		Background:   nonEmptyPtr(principal.Background),
		Preferences:  nonEmptyPtr(principal.Preferences),
		LeaderOpenID: nonEmptyPtr(principal.LeaderOpenID),
		LeaderName:   nonEmptyPtr(principal.LeaderName),
	}
}

func snapshotGroup(group GroupContext) *contextsnap.Group {
	return &contextsnap.Group{
		ID:          group.ID,
		ChatID:      group.ChatID,
		Name:        nonEmptyPtr(group.Name),
		Description: nonEmptyPtr(group.Description),
	}
}

func (s *PipelineStore) snapshotProject(ctx context.Context, batch ChatBatch, projectID *uint64) (*contextsnap.Project, error) {
	if projectID == nil {
		return nil, nil
	}
	// Bound project detail is already loaded in the batch (with repos/decisions).
	if batch.Project != nil && batch.Project.ID == *projectID {
		p := batch.Project
		return &contextsnap.Project{
			ID: p.ID, Code: nonEmptyPtr(p.Code), Name: p.Name, Role: p.Role,
			Description:  nonEmptyPtr(p.Description),
			Repos:        rawJSONOrNull(p.Repos),
			KeyDecisions: rawJSONOrNull(p.KeyDecisions),
		}, nil
	}
	// Hint-resolved project: read full detail once so repos/description are frozen.
	var row domain.Project
	if err := s.db.WithContext(ctx).First(&row, *projectID).Error; err != nil {
		return nil, fmt.Errorf("load resolved project id=%d for snapshot: %w", *projectID, err)
	}
	return &contextsnap.Project{
		ID: row.ID, Code: row.Code, Name: row.Name, Role: row.Role,
		Description:  row.Description,
		Repos:        rawJSONOrNull(row.Repos),
		KeyDecisions: rawJSONOrNull(row.KeyDecisions),
	}, nil
}

func snapshotAssigner(openID string, participants []ParticipantContext) *contextsnap.Assigner {
	assigner := &contextsnap.Assigner{OpenID: openID}
	for _, participant := range participants {
		if participant.OpenID == openID {
			assigner.Name = nonEmptyPtr(participant.Name)
			assigner.Role = nonEmptyPtr(participant.Role)
			assigner.Relation = nonEmptyPtr(participant.Relation)
			break
		}
	}
	return assigner
}

// snapshotMessages returns the candidate's cited source evidence messages, in
// the order the candidate cited them, copied verbatim from the unit.
func snapshotMessages(unit ConversationUnit, candidate Candidate) []contextsnap.Message {
	byID := make(map[string]MessageContext, len(unit.Messages))
	for _, message := range unit.Messages {
		byID[message.MessageID] = message
	}
	messages := make([]contextsnap.Message, 0, len(candidate.SourceMessageIDs))
	for _, id := range candidate.SourceMessageIDs {
		message, ok := byID[id]
		if !ok {
			continue
		}
		messages = append(messages, contextsnap.Message{
			MessageID: message.MessageID, ChatID: message.ChatID,
			SenderOpenID: message.SenderOpenID, SenderName: message.SenderName,
			Content: message.Content, CreateTime: message.CreateTime,
		})
	}
	return messages
}

func rawJSONOrNull(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}
