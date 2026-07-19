package extract

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"jarvis/internal/domain"
	"jarvis/internal/semantic"

	"gorm.io/gorm"
)

// PipelineStore owns M3's read model and transactional write boundary.
type PipelineStore struct {
	db       *gorm.DB
	location *time.Location
	semantic semanticSink
}

type semanticSink interface {
	Upsert(context.Context, []semantic.Record) error
}

func NewPipelineStore(db *gorm.DB, location *time.Location, sink semanticSink) (*PipelineStore, error) {
	if db == nil {
		return nil, fmt.Errorf("extract pipeline store db is nil")
	}
	if location == nil {
		return nil, fmt.Errorf("extract pipeline store location is nil")
	}
	if sink == nil {
		return nil, fmt.Errorf("extract pipeline semantic sink is nil")
	}
	return &PipelineStore{db: db, location: location, semantic: sink}, nil
}

func (s *PipelineStore) LoadPendingChats(ctx context.Context, opts LoadOptions) ([]ChatBatch, error) {
	if err := validateLoadOptions(opts); err != nil {
		return nil, err
	}
	var groups []domain.Group
	if err := s.db.WithContext(ctx).Preload("Project").
		Where("related_group = ?", true).
		Order("is_key_group DESC, pinned DESC, COALESCE(last_active_at, 0) DESC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list related groups for extraction: %w", err)
	}

	remaining := opts.BatchMessages
	batches := make([]ChatBatch, 0)
	for i := range groups {
		if remaining == 0 {
			break
		}
		messages, err := s.loadNewMessages(ctx, groups[i].ChatID, remaining)
		if err != nil {
			return nil, err
		}
		if len(messages) == 0 {
			continue
		}
		batch, err := s.buildChatBatch(ctx, &groups[i], messages, opts)
		if err != nil {
			return nil, fmt.Errorf("build extraction batch chat_id=%s: %w", groups[i].ChatID, err)
		}
		batches = append(batches, *batch)
		remaining -= len(messages)
	}
	return batches, nil
}

func validateLoadOptions(opts LoadOptions) error {
	if opts.BatchMessages <= 0 {
		return fmt.Errorf("extract batch messages must be positive")
	}
	if opts.ContextMessages < 0 {
		return fmt.Errorf("extract context messages must not be negative")
	}
	if opts.ContextWindow <= 0 {
		return fmt.Errorf("extract context window must be positive")
	}
	if opts.OpenTodoLimit <= 0 {
		return fmt.Errorf("extract open todo limit must be positive")
	}
	return nil
}

func (s *PipelineStore) loadNewMessages(ctx context.Context, chatID string, limit int) ([]domain.Message, error) {
	query := s.db.WithContext(ctx).Where("chat_id = ?", chatID)
	var watermark domain.TodoExtractWatermark
	watermarkResult := s.db.WithContext(ctx).Where("chat_id = ?", chatID).Limit(1).Find(&watermark)
	switch {
	case watermarkResult.Error != nil:
		return nil, fmt.Errorf("load extract watermark chat_id=%s: %w", chatID, watermarkResult.Error)
	case watermarkResult.RowsAffected == 1:
		var cursor domain.Message
		if err := s.db.WithContext(ctx).
			Where("chat_id = ? AND message_id = ?", chatID, watermark.LastScannedMessageID).
			First(&cursor).Error; err != nil {
			return nil, fmt.Errorf("resolve extract watermark chat_id=%s message_id=%s: %w", chatID, watermark.LastScannedMessageID, err)
		}
		query = query.Where("create_time > ? OR (create_time = ? AND id > ?)", cursor.CreateTime, cursor.CreateTime, cursor.ID)
	case watermarkResult.RowsAffected == 0:
		// M2 itself starts at current time, so an absent M3 watermark means all
		// locally captured messages for this explicitly related group are new.
	default:
		return nil, fmt.Errorf("load extract watermark chat_id=%s returned rows=%d, want 0 or 1", chatID, watermarkResult.RowsAffected)
	}
	var messages []domain.Message
	if err := query.Order("create_time ASC, id ASC").Limit(limit).Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("list new extraction messages chat_id=%s: %w", chatID, err)
	}
	return messages, nil
}

func (s *PipelineStore) buildChatBatch(ctx context.Context, group *domain.Group, newRows []domain.Message, opts LoadOptions) (*ChatBatch, error) {
	newMessages := make([]MessageContext, len(newRows))
	for i := range newRows {
		newMessages[i] = messageContext(&newRows[i], true)
	}
	topicRoots := make(map[string]struct{})
	for _, message := range newMessages {
		if message.RootID != "" {
			topicRoots[message.RootID] = struct{}{}
		} else if message.ThreadID != "" {
			topicRoots[message.ThreadID] = struct{}{}
		}
	}
	grouped := make(map[string][]MessageContext)
	keys := make([]string, 0)
	for _, message := range newMessages {
		if !message.Extractable {
			continue
		}
		key := conversationKey(message)
		if _, isRoot := topicRoots[message.MessageID]; isRoot {
			key = "topic:" + message.MessageID
		}
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], message)
	}
	sort.Slice(keys, func(i, j int) bool {
		first, second := grouped[keys[i]][0], grouped[keys[j]][0]
		if first.CreateTime == second.CreateTime {
			return first.DatabaseID < second.DatabaseID
		}
		return first.CreateTime < second.CreateTime
	})

	openTodos, err := s.loadOpenTodos(ctx, group.ID, opts.OpenTodoLimit)
	if err != nil {
		return nil, err
	}
	units := make([]ConversationUnit, 0, len(keys))
	for _, key := range keys {
		current := grouped[key]
		contextMessages, err := s.loadContextMessages(ctx, group.ChatID, key, current[0], opts)
		if err != nil {
			return nil, err
		}
		messages := append(contextMessages, current...)
		participants, err := s.enrichParticipants(ctx, messages)
		if err != nil {
			return nil, err
		}
		resources, err := s.loadResources(ctx, group.ID, messages)
		if err != nil {
			return nil, err
		}
		units = append(units, ConversationUnit{
			Key: key, Messages: messages, Participants: participants, Resources: resources,
		})
	}

	batch := &ChatBatch{
		Group: GroupContext{
			ID: group.ID, ChatID: group.ChatID, Name: stringValue(group.Name),
			IsKeyGroup: group.IsKeyGroup, ProjectID: copyUint64(group.ProjectID),
		},
		OpenTodos: openTodos,
		Units:     units,
		LastNew:   newMessages[len(newMessages)-1],
	}
	if group.Project != nil {
		batch.Project = projectContext(group.Project)
	}
	return batch, nil
}

func (s *PipelineStore) loadContextMessages(ctx context.Context, chatID, key string, first MessageContext, opts LoadOptions) ([]MessageContext, error) {
	if opts.ContextMessages == 0 {
		return nil, nil
	}
	startMS := first.CreateTime - opts.ContextWindow.Milliseconds()
	query := s.db.WithContext(ctx).Where("chat_id = ? AND create_time >= ?", chatID, startMS).
		Where("create_time < ? OR (create_time = ? AND id < ?)", first.CreateTime, first.CreateTime, first.DatabaseID).
		Where("render_ok = ?", true)
	if key == "chat" {
		query = query.Where("(root_id IS NULL OR root_id = '') AND (thread_id IS NULL OR thread_id = '')")
	} else {
		topicID := strings.TrimPrefix(key, "topic:")
		query = query.Where("(COALESCE(NULLIF(root_id, ''), NULLIF(thread_id, '')) = ? OR message_id = ?)", topicID, topicID)
	}
	var rows []domain.Message
	if err := query.Order("create_time DESC, id DESC").Limit(opts.ContextMessages).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load extraction context chat_id=%s unit=%s: %w", chatID, key, err)
	}
	result := make([]MessageContext, len(rows))
	for i := range rows {
		result[len(rows)-1-i] = messageContext(&rows[i], false)
	}
	return result, nil
}

func (s *PipelineStore) enrichParticipants(ctx context.Context, messages []MessageContext) ([]ParticipantContext, error) {
	openIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, message := range messages {
		if message.SenderOpenID == "" {
			continue
		}
		if _, ok := seen[message.SenderOpenID]; !ok {
			seen[message.SenderOpenID] = struct{}{}
			openIDs = append(openIDs, message.SenderOpenID)
		}
	}
	var people []domain.Person
	if len(openIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("open_id IN ? AND is_active = ?", openIDs, true).Find(&people).Error; err != nil {
			return nil, fmt.Errorf("load extraction participants: %w", err)
		}
	}
	byID := make(map[string]domain.Person, len(people))
	for _, person := range people {
		byID[person.OpenID] = person
	}
	participants := make([]ParticipantContext, 0, len(openIDs))
	for _, openID := range openIDs {
		person, ok := byID[openID]
		participant := ParticipantContext{OpenID: openID, Role: "unknown"}
		if ok {
			participant.Name = person.Name
			participant.Role = person.Role
			participant.IsLeader = person.Role == "leader"
			participant.Relation = stringValue(person.Relation)
			participant.CommStyle = stringValue(person.CommStyle)
		}
		if participant.Name == "" {
			for _, message := range messages {
				if message.SenderOpenID == openID && message.SenderName != "" {
					participant.Name = message.SenderName
					break
				}
			}
		}
		participants = append(participants, participant)
	}
	sort.Slice(participants, func(i, j int) bool { return participants[i].OpenID < participants[j].OpenID })
	for i := range messages {
		if person, ok := byID[messages[i].SenderOpenID]; ok {
			messages[i].IsLeader = person.Role == "leader"
		}
	}
	return participants, nil
}

func (s *PipelineStore) loadResources(ctx context.Context, groupID uint64, messages []MessageContext) ([]ResourceContext, error) {
	messageIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, message.MessageID)
	}
	if len(messageIDs) == 0 {
		return nil, nil
	}
	var rows []domain.Resource
	if err := s.db.WithContext(ctx).
		Where("group_id = ? AND source_message_id IN ?", groupID, messageIDs).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load extraction resources group_id=%d: %w", groupID, err)
	}
	resources := make([]ResourceContext, len(rows))
	for i := range rows {
		resources[i] = ResourceContext{
			ID: rows[i].ID, ResourceType: rows[i].ResourceType,
			FileKey: stringValue(rows[i].FileKey), MinuteToken: stringValue(rows[i].MinuteToken),
			DocToken: stringValue(rows[i].DocToken), URL: stringValue(rows[i].URL),
			Name: stringValue(rows[i].Name), ExtractedText: stringValue(rows[i].ExtractedText),
		}
	}
	return resources, nil
}

func (s *PipelineStore) loadOpenTodos(ctx context.Context, groupID uint64, limit int) ([]OpenTodoContext, error) {
	var rows []domain.Todo
	if err := s.db.WithContext(ctx).
		Where("group_id = ? AND status IN ?", groupID, []string{"extracted", "scoring", "need_info", "need_decision"}).
		Order("last_evidence_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load open todos group_id=%d: %w", groupID, err)
	}
	result := make([]OpenTodoContext, len(rows))
	for i := range rows {
		result[i] = OpenTodoContext{ID: rows[i].ID, ActionType: rows[i].ActionType, Title: rows[i].Title, Status: rows[i].Status}
	}
	return result, nil
}

func messageContext(message *domain.Message, isNew bool) MessageContext {
	return MessageContext{
		DatabaseID: message.ID, MessageID: message.MessageID, ChatID: message.ChatID,
		SenderOpenID: message.SenderOpenID, SenderName: message.SenderName, SenderType: message.SenderType,
		Content: message.Content, RootID: stringValue(message.RootID), ThreadID: stringValue(message.ThreadID),
		CreateTime: message.CreateTime, IsNew: isNew, Extractable: extractableMessage(message),
	}
}

func extractableMessage(message *domain.Message) bool {
	if !message.RenderOK {
		return false
	}
	senderType := strings.ToLower(strings.TrimSpace(message.SenderType))
	if senderType == "bot" || senderType == "app" {
		return false
	}
	content := strings.TrimSpace(message.Content)
	if content == "" || content == "[图片]" || content == "[表情]" || (strings.HasPrefix(content, "[文件:") && strings.HasSuffix(content, "]")) {
		return false
	}
	for _, r := range content {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

func conversationKey(message MessageContext) string {
	if message.RootID != "" {
		return "topic:" + message.RootID
	}
	if message.ThreadID != "" {
		return "topic:" + message.ThreadID
	}
	return "chat"
}

func projectContext(project *domain.Project) *ProjectContext {
	return &ProjectContext{
		ID: project.ID, Code: stringValue(project.Code), Name: project.Name, Role: project.Role,
		Description: stringValue(project.Description), Repos: append([]byte(nil), project.Repos...),
		KeyDecisions: append([]byte(nil), project.KeyDecisions...),
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
