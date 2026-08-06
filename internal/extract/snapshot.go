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
// the batch). M5 gets a compact projection first and can query this exact
// snapshot when it needs creation-time detail.
func (s *PipelineStore) buildContextSnapshot(ctx context.Context, batch ChatBatch, unit ConversationUnit, candidate Candidate, projectID *uint64, assignerOpenID *string, facts []contextsnap.Fact) (contextsnap.Snapshot, error) {
	snapshot := contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		CapturedAt:      s.now().UTC().Format(time.RFC3339),
		Principal:       snapshotPrincipal(batch.Principal),
		Group:           snapshotGroup(batch.Group),
		Messages:        snapshotMessages(unit, candidate),
		Conversation:    snapshotConversation(unit),
		Participants:    snapshotParticipants(unit.Participants),
		Resources:       snapshotResources(unit.Resources),
		OpenTodos:       snapshotOpenTodos(batch.OpenTodos),
		RecentTasks:     snapshotRecentTasks(batch.RecentTasks),
		OtherProjects:   snapshotOtherProjects(batch.OtherProjects, projectID),
		Facts:           facts,
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
		Background:   nonEmptyPtr(principal.Background),
		Preferences:  nonEmptyPtr(principal.Preferences),
		LeaderOpenID: nonEmptyPtr(principal.LeaderOpenID),
		LeaderName:   nonEmptyPtr(principal.LeaderName),
	}
}

func snapshotGroup(group GroupContext) *contextsnap.Group {
	return &contextsnap.Group{
		ID:             group.ID,
		ChatID:         group.ChatID,
		Name:           nonEmptyPtr(group.Name),
		Description:    nonEmptyPtr(group.Description),
		BackgroundNote: nonEmptyPtr(group.BackgroundNote),
		IsKeyGroup:     group.IsKeyGroup,
		ProjectID:      copyUint64(group.ProjectID),
	}
}

func (s *PipelineStore) snapshotProject(ctx context.Context, batch ChatBatch, projectID *uint64) (*contextsnap.Project, error) {
	if projectID == nil {
		return nil, nil
	}
	// Bound project detail is already loaded in the batch (with repos/key project decisions).
	if batch.Project != nil && batch.Project.ID == *projectID {
		p := batch.Project
		return &contextsnap.Project{
			ID: p.ID, Code: nonEmptyPtr(p.Code), Name: p.Name, Role: p.Role,
			Status: p.Status, Priority: p.Priority, Description: nonEmptyPtr(p.Description),
			Repos:        rawJSONOrNull(p.Repos),
			TechStack:    rawJSONOrNull(p.TechStack),
			KeyDecisions: rawJSONOrNull(p.KeyDecisions),
			Timeline:     rawJSONOrNull(p.Timeline),
			Notes:        nonEmptyPtr(p.Notes),
		}, nil
	}
	// Hint-resolved project: read full detail once so repos/description are frozen.
	var row domain.Project
	if err := s.db.WithContext(ctx).First(&row, *projectID).Error; err != nil {
		return nil, fmt.Errorf("load resolved project id=%d for snapshot: %w", *projectID, err)
	}
	return &contextsnap.Project{
		ID: row.ID, Code: row.Code, Name: row.Name, Role: row.Role,
		Status: row.Status, Priority: row.Priority, Description: row.Description,
		Repos:        rawJSONOrNull(row.Repos),
		TechStack:    rawJSONOrNull(row.TechStack),
		KeyDecisions: rawJSONOrNull(row.KeyDecisions),
		Timeline:     rawJSONOrNull(row.Timeline),
		Notes:        row.Notes,
	}, nil
}

func snapshotAssigner(openID string, participants []ParticipantContext) *contextsnap.Assigner {
	assigner := &contextsnap.Assigner{OpenID: openID}
	for _, participant := range participants {
		if participant.OpenID == openID {
			assigner.Name = nonEmptyPtr(participant.Name)
			assigner.Role = nonEmptyPtr(participant.Role)
			assigner.Title = nonEmptyPtr(participant.Title)
			assigner.Relation = nonEmptyPtr(participant.Relation)
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
			IsLeader: participants[i].IsLeader, Relation: nonEmptyPtr(participants[i].Relation),
			CommStyle: nonEmptyPtr(participants[i].CommStyle),
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

func snapshotOpenTodos(todos []OpenTodoContext) []contextsnap.OpenTodo {
	result := make([]contextsnap.OpenTodo, len(todos))
	for i := range todos {
		result[i] = contextsnap.OpenTodo{
			ID: todos[i].ID, ActionType: todos[i].ActionType,
			Title: todos[i].Title, Status: todos[i].Status,
		}
	}
	return result
}

func snapshotRecentTasks(tasks []RecentTaskContext) []contextsnap.RecentTask {
	result := make([]contextsnap.RecentTask, len(tasks))
	for i := range tasks {
		result[i] = contextsnap.RecentTask{
			ID: tasks[i].ID, Title: tasks[i].Title, Status: tasks[i].Status,
			Summary: tasks[i].Summary, LastProgressAt: tasks[i].LastProgressAt,
		}
	}
	return result
}

func snapshotOtherProjects(projects []OtherProjectContext, selectedID *uint64) []contextsnap.ProjectBrief {
	result := make([]contextsnap.ProjectBrief, 0, len(projects))
	for i := range projects {
		if selectedID != nil && projects[i].ID == *selectedID {
			continue
		}
		result = append(result, contextsnap.ProjectBrief{
			ID: projects[i].ID, Code: nonEmptyPtr(projects[i].Code), Name: projects[i].Name,
			Role: projects[i].Role, Status: projects[i].Status, Priority: projects[i].Priority,
			Description: nonEmptyPtr(projects[i].Description),
		})
	}
	return result
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

// maxConversationMessages bounds how many surrounding messages we freeze into
// the snapshot's conversation context. Enough for several rounds of背景, small
// enough to keep the snapshot from bloating.
const maxConversationMessages = 25

// snapshotConversation freezes the surrounding chat thread (the whole
// conversation unit, capped) so M5 read more than the single cited message.
// It keeps the most recent messages (chronological order preserved) when the
// unit exceeds the cap, since recent context is the most relevant.
func snapshotConversation(unit ConversationUnit) []contextsnap.Message {
	messages := unit.Messages
	if len(messages) == 0 {
		return nil
	}
	if len(messages) > maxConversationMessages {
		messages = messages[len(messages)-maxConversationMessages:]
	}
	conversation := make([]contextsnap.Message, 0, len(messages))
	for _, message := range messages {
		conversation = append(conversation, contextsnap.Message{
			MessageID: message.MessageID, ChatID: message.ChatID,
			SenderOpenID: message.SenderOpenID, SenderName: message.SenderName,
			Content: message.Content, CreateTime: message.CreateTime,
		})
	}
	return conversation
}

func rawJSONOrNull(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}
