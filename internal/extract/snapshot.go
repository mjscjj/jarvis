package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
)

// buildContextSnapshot freezes the background for one candidate at extraction
// time (docs/decisions/context-snapshot.md). It assembles from the already
// loaded ChatBatch/unit data; the only DB read is the full project detail when
// the project was resolved from a hint (the bound project detail is already in
// the batch).
//
// Open Todos and recent Tasks are loaded into the M3 prompt for dedup judgement
// but deliberately are not frozen here: they are world state, and M5 loads them
// fresh at execution time instead.
func (s *PipelineStore) buildContextSnapshot(ctx context.Context, batch ChatBatch, unit ConversationUnit, candidate Candidate, projectID *uint64, assignerOpenID *string) (contextsnap.Snapshot, error) {
	snapshot := contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		CapturedAt:      s.now().UTC().Format(time.RFC3339),
		Principal:       snapshotPrincipal(batch.Principal),
		Group:           snapshotGroup(batch.Group),
		Messages:        snapshotConversation(unit),
		Participants:    snapshotParticipants(unit.Participants),
		Resources:       snapshotResources(unit.Resources),
		OtherProjects:   snapshotOtherProjects(batch.OtherProjects),
	}

	project, err := s.snapshotProject(ctx, batch, projectID)
	if err != nil {
		return contextsnap.Snapshot{}, err
	}
	snapshot.Project = project

	if assignerOpenID != nil {
		snapshot.Assigner = snapshotAssigner(*assignerOpenID, unit.Participants)
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
		Summary:      nonEmptyPtr(principal.Summary),
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
		Summary:     nonEmptyPtr(group.Summary),
		IsKeyGroup:  group.IsKeyGroup,
		ProjectID:   copyUint64(group.ProjectID),
	}
}

func (s *PipelineStore) snapshotProject(ctx context.Context, batch ChatBatch, projectID *uint64) (*contextsnap.Project, error) {
	if projectID == nil {
		return nil, nil
	}
	if batch.Project != nil && batch.Project.ID == *projectID {
		p := batch.Project
		return &contextsnap.Project{
			ID: p.ID, Code: nonEmptyPtr(p.Code), Name: p.Name, Role: p.Role,
			Status: p.Status, Priority: p.Priority, Summary: nonEmptyPtr(p.Summary),
		}, nil
	}
	var row domain.Project
	if err := s.db.WithContext(ctx).First(&row, *projectID).Error; err != nil {
		return nil, fmt.Errorf("load resolved project id=%d for snapshot: %w", *projectID, err)
	}
	return &contextsnap.Project{
		ID: row.ID, Code: row.Code, Name: row.Name, Role: row.Role,
		Status: row.Status, Priority: row.Priority, Summary: copyStringPtr(row.Summary),
	}, nil
}

func snapshotAssigner(openID string, participants []ParticipantContext) *contextsnap.Assigner {
	assigner := &contextsnap.Assigner{OpenID: openID}
	for _, participant := range participants {
		if participant.OpenID == openID {
			assigner.Name = nonEmptyPtr(participant.Name)
			assigner.Role = nonEmptyPtr(participant.Role)
			assigner.Title = nonEmptyPtr(participant.Title)
			assigner.Summary = nonEmptyPtr(participant.Summary)
			break
		}
	}
	return assigner
}

func snapshotParticipants(participants []ParticipantContext) []contextsnap.Participant {
	result := make([]contextsnap.Participant, len(participants))
	for i := range participants {
		result[i] = contextsnap.Participant{
			OpenID: participants[i].OpenID, Name: nonEmptyPtr(participants[i].Name),
			Role: nonEmptyPtr(participants[i].Role), Title: nonEmptyPtr(participants[i].Title),
			IsLeader: participants[i].IsLeader, Summary: nonEmptyPtr(participants[i].Summary),
		}
	}
	return result
}

func snapshotResources(resources []ResourceContext) []contextsnap.Resource {
	result := make([]contextsnap.Resource, len(resources))
	for i := range resources {
		result[i] = contextsnap.Resource{
			ID: resources[i].ID, ResourceType: resources[i].ResourceType,
			FileKey: nonEmptyPtr(resources[i].FileKey), MinuteToken: nonEmptyPtr(resources[i].MinuteToken),
			DocToken: nonEmptyPtr(resources[i].DocToken), URL: nonEmptyPtr(resources[i].URL),
			Name: nonEmptyPtr(resources[i].Name), ExtractedText: nonEmptyPtr(resources[i].ExtractedText),
		}
	}
	return result
}

// Preserve the catalog M3 saw even when one entry becomes the resolved project.
// Its advertised material key and contents must survive inference unchanged.
func snapshotOtherProjects(projects []OtherProjectContext) []contextsnap.ProjectBrief {
	result := make([]contextsnap.ProjectBrief, 0, len(projects))
	for i := range projects {
		result = append(result, contextsnap.ProjectBrief{
			ID: projects[i].ID, Code: nonEmptyPtr(projects[i].Code), Name: projects[i].Name,
			Role: projects[i].Role, Status: projects[i].Status, Priority: projects[i].Priority,
		})
	}
	return result
}

// snapshotConversation preserves the entire admitted unit for later reads.
func snapshotConversation(unit ConversationUnit) []contextsnap.Message {
	messages := unit.Messages
	if len(messages) == 0 {
		return nil
	}
	conversation := make([]contextsnap.Message, 0, len(messages))
	for _, message := range messages {
		conversation = append(conversation, contextsnap.Message{
			MessageID: message.MessageID, ChatID: message.ChatID, ChatMode: message.ChatMode,
			SenderOpenID: message.SenderOpenID, SenderName: message.SenderName,
			SourceURL: message.SourceURL, Mentions: append(json.RawMessage(nil), message.Mentions...),
			Content: message.Content, RootID: message.RootID, ThreadID: message.ThreadID,
			CreateTime: message.CreateTime,
		})
	}
	return conversation
}

func copyStringPtr(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	copied := *value
	return &copied
}
