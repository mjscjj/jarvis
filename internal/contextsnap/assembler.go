package contextsnap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// AssembleOptions identifies the scope combined with Jarvis' authoritative
// background. RequestContext is preserved as data; it never replaces common
// context. ChatID and GroupID are consumed only by AssembleConversation.
type AssembleOptions struct {
	ProjectID              *uint64
	ChatID                 string
	GroupID                *uint64
	AnchorMessageID        string
	ConversationLimit      int
	ConversationMessageIDs []string
	RequestContext         json.RawMessage
}

// Assembler builds one canonical background shape. Assemble serves ordinary
// Task creation; AssembleConversation adds live scope for CC Connect, backend
// chat and ScheduledTask wake-ups without duplicating lookup logic.
type Assembler struct {
	db              *gorm.DB
	principalOpenID string
	now             func() time.Time
}

func NewAssembler(db *gorm.DB, principalOpenID string) (*Assembler, error) {
	if db == nil {
		return nil, fmt.Errorf("context snapshot assembler db is nil")
	}
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return nil, fmt.Errorf("context snapshot assembler principal open_id is empty")
	}
	return &Assembler{db: db, principalOpenID: principalOpenID, now: time.Now}, nil
}

func (a *Assembler) Assemble(ctx context.Context, options AssembleOptions) (json.RawMessage, error) {
	return a.assemble(ctx, options, false)
}

// AssembleConversation adds live conversation scope and current work to the
// common background. It is intentionally reserved for interactive entrypoints
// and ScheduledTask wake-ups; ordinary Task execution keeps its frozen context.
func (a *Assembler) AssembleConversation(ctx context.Context, options AssembleOptions) (json.RawMessage, error) {
	return a.assemble(ctx, options, true)
}

func (a *Assembler) assemble(ctx context.Context, options AssembleOptions, includeLiveContext bool) (json.RawMessage, error) {
	requestContext, requestObject, hintedProjectID, err := normalizeRequestContext(options.RequestContext)
	if err != nil {
		return nil, err
	}
	var group *Group
	if includeLiveContext {
		chatID := strings.TrimSpace(options.ChatID)
		if chatID == "" {
			chatID, err = chatHint(requestObject)
			if err != nil {
				return nil, err
			}
		}
		group, err = a.loadGroup(ctx, chatID, options.GroupID)
		if err != nil {
			return nil, err
		}
	}
	projectID := options.ProjectID
	if projectID == nil {
		projectID = hintedProjectID
	}
	if projectID == nil && group != nil {
		projectID = group.ProjectID
	}
	if projectID != nil && *projectID == 0 {
		return nil, fmt.Errorf("assemble context snapshot: project_id must be positive")
	}

	principal, err := a.loadPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	project, err := a.loadProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	otherProjects, err := a.loadOtherProjects(ctx, projectID)
	if err != nil {
		return nil, err
	}
	managedResources, err := a.loadManagedResources(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var messages []Message
	var conversation []Message
	if includeLiveContext {
		messages, conversation, err = a.loadConversation(ctx, group, options.AnchorMessageID, options.ConversationLimit, options.ConversationMessageIDs)
		if err != nil {
			return nil, err
		}
	}

	snapshot := Snapshot{
		SnapshotVersion:  SnapshotVersion,
		CapturedAt:       a.now().UTC().Format(time.RFC3339),
		Principal:        principal,
		Project:          project,
		Group:            group,
		Messages:         messages,
		Conversation:     conversation,
		OtherProjects:    otherProjects,
		ManagedResources: managedResources,
		Memories:         make([]map[string]any, 0),
		RequestContext:   requestContext,
	}
	raw, err := snapshot.Encode()
	if err != nil {
		return nil, fmt.Errorf("assemble context snapshot: %w", err)
	}
	return raw, nil
}

func (a *Assembler) loadConversation(ctx context.Context, group *Group, anchorMessageID string, limit int, messageIDs []string) ([]Message, []Message, error) {
	anchorMessageID = strings.TrimSpace(anchorMessageID)
	if limit < 0 {
		return nil, nil, fmt.Errorf("assemble context snapshot: conversation_limit must not be negative")
	}
	if anchorMessageID == "" && limit == 0 {
		return nil, nil, nil
	}
	if anchorMessageID == "" || limit <= 0 {
		return nil, nil, fmt.Errorf("assemble context snapshot: anchor_message_id and positive conversation_limit must be set together")
	}
	if group == nil || strings.TrimSpace(group.ChatID) == "" {
		return nil, nil, fmt.Errorf("assemble context snapshot: conversation requires a configured group")
	}
	if len(messageIDs) > 0 {
		return a.loadSelectedConversation(ctx, group, anchorMessageID, limit, messageIDs)
	}
	var anchor domain.Message
	if err := a.db.WithContext(ctx).
		Where("chat_id = ? AND message_id = ?", group.ChatID, anchorMessageID).
		Take(&anchor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, fmt.Errorf("assemble context snapshot: anchor message_id=%s is not captured in chat_id=%s", anchorMessageID, group.ChatID)
		}
		return nil, nil, fmt.Errorf("assemble context snapshot: load anchor message_id=%s: %w", anchorMessageID, err)
	}
	query := a.db.WithContext(ctx).Where("chat_id = ?", group.ChatID).
		Where("render_ok = ?", true).
		Where("create_time < ? OR (create_time = ? AND id <= ?)", anchor.CreateTime, anchor.CreateTime, anchor.ID)
	topicID := strings.TrimSpace(stringValue(anchor.ThreadID))
	if topicID == "" {
		topicID = strings.TrimSpace(stringValue(anchor.RootID))
	}
	if topicID == "" {
		query = query.Where("(root_id IS NULL OR root_id = '') AND (thread_id IS NULL OR thread_id = '')")
	} else {
		query = query.Where("(COALESCE(NULLIF(thread_id, ''), NULLIF(root_id, '')) = ? OR message_id = ?)", topicID, topicID)
	}
	var rows []domain.Message
	if err := query.Order("create_time DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("assemble context snapshot: load conversation chat_id=%s: %w", group.ChatID, err)
	}
	conversation := make([]Message, len(rows))
	for i := range rows {
		conversation[len(rows)-1-i] = snapshotMessage(&rows[i])
	}
	return []Message{snapshotMessage(&anchor)}, conversation, nil
}

func (a *Assembler) loadSelectedConversation(ctx context.Context, group *Group, anchorMessageID string, limit int, messageIDs []string) ([]Message, []Message, error) {
	if len(messageIDs) > limit {
		return nil, nil, fmt.Errorf("assemble context snapshot: selected conversation has %d messages, limit is %d", len(messageIDs), limit)
	}
	normalized := make([]string, 0, len(messageIDs))
	seen := make(map[string]struct{}, len(messageIDs))
	anchorFound := false
	for _, raw := range messageIDs {
		messageID := strings.TrimSpace(raw)
		if messageID == "" {
			return nil, nil, fmt.Errorf("assemble context snapshot: selected conversation contains blank message_id")
		}
		if _, exists := seen[messageID]; exists {
			return nil, nil, fmt.Errorf("assemble context snapshot: selected conversation contains duplicate message_id=%s", messageID)
		}
		seen[messageID] = struct{}{}
		normalized = append(normalized, messageID)
		anchorFound = anchorFound || messageID == anchorMessageID
	}
	if !anchorFound {
		return nil, nil, fmt.Errorf("assemble context snapshot: selected conversation does not contain anchor message_id=%s", anchorMessageID)
	}
	var rows []domain.Message
	if err := a.db.WithContext(ctx).Where("chat_id = ? AND message_id IN ?", group.ChatID, normalized).Find(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("assemble context snapshot: load selected conversation chat_id=%s: %w", group.ChatID, err)
	}
	byID := make(map[string]*domain.Message, len(rows))
	for index := range rows {
		byID[rows[index].MessageID] = &rows[index]
	}
	conversation := make([]Message, 0, len(normalized))
	for _, messageID := range normalized {
		row := byID[messageID]
		if row == nil {
			return nil, nil, fmt.Errorf("assemble context snapshot: selected message_id=%s is not captured in chat_id=%s", messageID, group.ChatID)
		}
		conversation = append(conversation, snapshotMessage(row))
	}
	anchor := byID[anchorMessageID]
	return []Message{snapshotMessage(anchor)}, conversation, nil
}

func snapshotMessage(row *domain.Message) Message {
	return Message{
		MessageID: row.MessageID, ChatID: row.ChatID, SenderOpenID: row.SenderOpenID,
		SenderName: row.SenderName, Content: row.Content,
		RootID: stringValue(row.RootID), ThreadID: stringValue(row.ThreadID), CreateTime: row.CreateTime,
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (a *Assembler) loadGroup(ctx context.Context, chatID string, groupID *uint64) (*Group, error) {
	if chatID != "" && groupID != nil {
		return nil, fmt.Errorf("assemble context snapshot: chat_id and group_id cannot both be set")
	}
	if groupID != nil && *groupID == 0 {
		return nil, fmt.Errorf("assemble context snapshot: group_id must be positive")
	}
	if chatID == "" && groupID == nil {
		return nil, nil
	}
	var row domain.Group
	query := a.db.WithContext(ctx)
	identity := fmt.Sprintf("chat_id=%s", chatID)
	if groupID != nil {
		query = query.Where("id = ?", *groupID)
		identity = fmt.Sprintf("group_id=%d", *groupID)
	} else {
		query = query.Where("chat_id = ?", chatID)
	}
	err := query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("assemble context snapshot: %s is not configured", identity)
	}
	if err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load %s: %w", identity, err)
	}
	return &Group{
		ID: row.ID, ChatID: row.ChatID, Name: copyString(row.Name),
		Description: copyString(row.Description), Summary: copyString(row.Summary),
		IsKeyGroup: row.IsKeyGroup, ProjectID: copyUint64(row.ProjectID),
	}, nil
}

func (a *Assembler) loadPrincipal(ctx context.Context) (*Principal, error) {
	var row domain.PrincipalProfile
	err := a.db.WithContext(ctx).Where("open_id = ?", a.principalOpenID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("assemble context snapshot: principal profile open_id=%s is not configured", a.principalOpenID)
	}
	if err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load principal profile: %w", err)
	}
	return &Principal{
		OpenID: row.OpenID, Name: row.Name, Department: copyString(row.Department),
		Title: copyString(row.Title), Summary: copyString(row.Summary),
		LeaderOpenID: copyString(row.LeaderOpenID),
		LeaderName:   copyString(row.LeaderName),
	}, nil
}

func (a *Assembler) loadProject(ctx context.Context, projectID *uint64) (*Project, error) {
	if projectID == nil {
		return nil, nil
	}
	var row domain.Project
	err := a.db.WithContext(ctx).Where("id = ?", *projectID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("assemble context snapshot: project_id=%d does not exist", *projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load project_id=%d: %w", *projectID, err)
	}
	return projectFromDomain(&row), nil
}

func (a *Assembler) loadOtherProjects(ctx context.Context, selectedID *uint64) ([]ProjectBrief, error) {
	var rows []domain.Project
	query := a.db.WithContext(ctx).Where("status <> ?", "archived")
	if selectedID != nil {
		query = query.Where("id <> ?", *selectedID)
	}
	if err := query.Order("priority ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load project catalog: %w", err)
	}
	result := make([]ProjectBrief, len(rows))
	for i := range rows {
		result[i] = ProjectBrief{
			ID: rows[i].ID, Code: copyString(rows[i].Code), Name: rows[i].Name,
			Role: rows[i].Role, Status: rows[i].Status, Priority: rows[i].Priority,
		}
	}
	return result, nil
}

func (a *Assembler) loadManagedResources(ctx context.Context, projectID *uint64) ([]ManagedResource, error) {
	query := a.db.WithContext(ctx).Where("is_active = ?", true)
	if projectID == nil {
		query = query.Where("link_principal = ?", true)
	} else {
		query = query.Where("link_principal = ? OR project_id = ?", true, *projectID)
	}
	var rows []domain.ManagedResource
	if err := query.Order("datetime(last_active_at) DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load managed resources: %w", err)
	}
	result := make([]ManagedResource, len(rows))
	for i := range rows {
		result[i] = ManagedResource{
			ID: rows[i].ID, Title: rows[i].Title, ResourceType: rows[i].ResourceType,
			URL: copyString(rows[i].URL), Summary: copyString(rows[i].Summary),
			ProjectID: copyUint64(rows[i].ProjectID), LinkPrincipal: rows[i].LinkPrincipal,
			LastActiveAt: rows[i].LastActiveAt.UTC().Format(time.RFC3339),
		}
	}
	return result, nil
}

func projectFromDomain(row *domain.Project) *Project {
	if row == nil {
		return nil
	}
	return &Project{
		ID: row.ID, Code: copyString(row.Code), Name: row.Name, Role: row.Role,
		Status: row.Status, Priority: row.Priority, Summary: copyString(row.Summary),
	}
}

func normalizeRequestContext(raw json.RawMessage) (json.RawMessage, map[string]any, *uint64, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil, nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, nil, nil, fmt.Errorf("assemble context snapshot: request context must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, nil, fmt.Errorf("assemble context snapshot: request context has trailing data")
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("assemble context snapshot: encode request context: %w", err)
	}
	projectID, err := projectHint(object)
	if err != nil {
		return nil, nil, nil, err
	}
	return encoded, object, projectID, nil
}

func projectHint(object map[string]any) (*uint64, error) {
	value, ok := object["project_id"]
	if !ok {
		if project, projectOK := object["project"].(map[string]any); projectOK {
			value, ok = project["id"]
		}
	}
	if !ok {
		return nil, nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return nil, fmt.Errorf("assemble context snapshot: request context project id must be an integer")
	}
	id, err := number.Int64()
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("assemble context snapshot: request context project id must be a positive integer")
	}
	result := uint64(id)
	return &result, nil
}

func chatHint(object map[string]any) (string, error) {
	value, ok := object["chat_id"]
	if !ok {
		if group, groupOK := object["group"].(map[string]any); groupOK {
			value, ok = group["chat_id"]
		}
	}
	if !ok {
		return "", nil
	}
	chatID, ok := value.(string)
	if !ok || strings.TrimSpace(chatID) == "" {
		return "", fmt.Errorf("assemble context snapshot: request context chat_id must be a non-empty string")
	}
	return strings.TrimSpace(chatID), nil
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
