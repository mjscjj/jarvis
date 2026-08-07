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
	db              *gorm.DB
	location        *time.Location
	semantic        semanticSink
	principalOpenID string
}

type semanticSink interface {
	Upsert(context.Context, []semantic.Record) error
}

func NewPipelineStore(db *gorm.DB, location *time.Location, sink semanticSink, principalOpenID string) (*PipelineStore, error) {
	if db == nil {
		return nil, fmt.Errorf("extract pipeline store db is nil")
	}
	if location == nil {
		return nil, fmt.Errorf("extract pipeline store location is nil")
	}
	if sink == nil {
		return nil, fmt.Errorf("extract pipeline semantic sink is nil")
	}
	if strings.TrimSpace(principalOpenID) == "" {
		return nil, fmt.Errorf("extract pipeline principal open_id is empty")
	}
	return &PipelineStore{db: db, location: location, semantic: sink, principalOpenID: principalOpenID}, nil
}

// PendingChatIDs lists the related chats holding messages beyond their
// extraction watermark, in the same priority order LoadPendingChats uses. It
// lets scheduled reconciliation hand work to the per-chat extraction path
// instead of running a competing global pass of its own.
func (s *PipelineStore) PendingChatIDs(ctx context.Context) ([]string, error) {
	var groups []domain.Group
	if err := s.db.WithContext(ctx).
		Where("related_group = ?", true).
		Order("is_key_group DESC, pinned DESC, COALESCE(last_active_at, 0) DESC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list related groups for extraction: %w", err)
	}
	pending := make([]string, 0, len(groups))
	for i := range groups {
		messages, err := s.loadNewMessages(ctx, groups[i].ChatID, 1)
		if err != nil {
			return nil, err
		}
		if len(messages) > 0 {
			pending = append(pending, groups[i].ChatID)
		}
	}
	return pending, nil
}

// LoadPendingChat builds the same M3 batch as LoadPendingChats, scoped to the
// chat that just committed new messages. A nil batch means the chat has no work
// beyond its extraction watermark (for example, a duplicate wake-up).
func (s *PipelineStore) LoadPendingChat(ctx context.Context, chatID string, opts LoadOptions) (*ChatBatch, error) {
	if err := validateLoadOptions(opts); err != nil {
		return nil, err
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, fmt.Errorf("load pending extraction chat_id is empty")
	}
	var group domain.Group
	result := s.db.WithContext(ctx).Preload("Project").Where("chat_id = ?", chatID).Limit(1).Find(&group)
	if result.Error != nil {
		return nil, fmt.Errorf("load extraction group chat_id=%s: %w", chatID, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("load extraction group chat_id=%s: not found", chatID)
	}
	if !group.RelatedGroup {
		return nil, fmt.Errorf("load extraction group chat_id=%s: not related", chatID)
	}
	messages, err := s.loadNewMessages(ctx, chatID, opts.BatchMessages)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	batch, err := s.buildChatBatch(ctx, &group, messages, opts)
	if err != nil {
		return nil, fmt.Errorf("build extraction batch chat_id=%s: %w", chatID, err)
	}
	principal, err := s.loadPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := s.loadProjectSummaries(ctx)
	if err != nil {
		return nil, err
	}
	batch.Principal = principal
	batch.OtherProjects = otherProjectsExcluding(projects, batch.Group.ProjectID)
	return batch, nil
}

// loadPrincipal returns the principal profile, resolving the leader name
// from the person table when the profile did not capture it. Returns nil when no
// profile row has been saved yet (extraction still works, just without the self
// background section).
func (s *PipelineStore) loadPrincipal(ctx context.Context) (*PrincipalContext, error) {
	var profile domain.PrincipalProfile
	found := s.db.WithContext(ctx).Where("open_id = ?", s.principalOpenID).Limit(1).Find(&profile)
	if found.Error != nil {
		return nil, fmt.Errorf("load principal profile: %w", found.Error)
	}
	if found.RowsAffected == 0 {
		return nil, nil
	}
	principal := &PrincipalContext{
		OpenID: profile.OpenID, Name: profile.Name,
		Department: stringValue(profile.Department), Title: stringValue(profile.Title),
		Background: stringValue(profile.Background), Preferences: stringValue(profile.Preferences),
		LeaderOpenID: stringValue(profile.LeaderOpenID), LeaderName: stringValue(profile.LeaderName),
	}
	if principal.LeaderOpenID != "" && principal.LeaderName == "" {
		var leader domain.Person
		leaderFound := s.db.WithContext(ctx).Where("open_id = ?", principal.LeaderOpenID).Limit(1).Find(&leader)
		if leaderFound.Error != nil {
			return nil, fmt.Errorf("resolve principal leader name: %w", leaderFound.Error)
		}
		if leaderFound.RowsAffected == 1 {
			principal.LeaderName = leader.Name
		}
	}
	return principal, nil
}

// loadProjectSummaries returns the concise view of every non-archived project,
// used to build the "other projects" map fed to the model.
func (s *PipelineStore) loadProjectSummaries(ctx context.Context) ([]OtherProjectContext, error) {
	var rows []domain.Project
	if err := s.db.WithContext(ctx).
		Where("status <> ?", "archived").
		Order("priority ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load project summaries: %w", err)
	}
	summaries := make([]OtherProjectContext, len(rows))
	for i := range rows {
		summaries[i] = OtherProjectContext{
			ID: rows[i].ID, Code: stringValue(rows[i].Code), Name: rows[i].Name,
			Role: rows[i].Role, Status: rows[i].Status, Priority: rows[i].Priority,
			Description: stringValue(rows[i].Description),
		}
	}
	return summaries, nil
}

func otherProjectsExcluding(all []OtherProjectContext, boundID *uint64) []OtherProjectContext {
	result := make([]OtherProjectContext, 0, len(all))
	for _, project := range all {
		if boundID != nil && project.ID == *boundID {
			continue
		}
		result = append(result, project)
	}
	return result
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
	if opts.RecentTaskLimit <= 0 {
		return fmt.Errorf("extract recent task limit must be positive")
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
	groupContext := GroupContext{
		ID: group.ID, ChatID: group.ChatID, Name: stringValue(group.Name),
		Description: stringValue(group.Description), BackgroundNote: stringValue(group.BackgroundNote),
		IsKeyGroup: group.IsKeyGroup, ProjectID: copyUint64(group.ProjectID),
	}
	recentTasks, err := s.loadRecentTasks(ctx, groupContext, opts.RecentTaskLimit)
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
		Group:           groupContext,
		OpenTodos:       openTodos,
		RecentTasks:     recentTasks,
		Units:           units,
		LastNew:         newMessages[len(newMessages)-1],
		NewMessageCount: len(newMessages),
	}
	if group.Project != nil {
		batch.Project = projectContext(group.Project)
	}
	return batch, nil
}

// LoadChatMessages returns the subset of the given message IDs that really
// exist in this chat, ordered chronologically and marked as context (not [new]).
// The extraction worker uses it to hydrate a unit with evidence the model cited
// after finding it with its own tools — bot replies, sibling topics, messages
// that landed after the batch was loaded — none of which the unit slice holds.
func (s *PipelineStore) LoadChatMessages(ctx context.Context, chatID string, messageIDs []string) ([]MessageContext, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	var rows []domain.Message
	if err := s.db.WithContext(ctx).
		Where("chat_id = ? AND message_id IN ?", chatID, messageIDs).
		Order("create_time ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load cited chat messages chat_id=%s: %w", chatID, err)
	}
	result := make([]MessageContext, len(rows))
	for i := range rows {
		result[i] = messageContext(&rows[i], false)
	}
	if _, err := s.enrichParticipants(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
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
			personID := person.ID
			participant.PersonID = &personID
			participant.Name = person.Name
			participant.Role = person.Role
			participant.Title = stringValue(person.Title)
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
		Where("group_id = ? AND status IN ?", groupID, []string{"extracted", "observing"}).
		Order("last_evidence_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load open todos group_id=%d: %w", groupID, err)
	}
	assignerOpenIDs := make([]string, 0)
	seenAssigner := make(map[string]struct{})
	for _, row := range rows {
		if row.AssignerOpenID == nil || strings.TrimSpace(*row.AssignerOpenID) == "" {
			continue
		}
		openID := strings.TrimSpace(*row.AssignerOpenID)
		if _, ok := seenAssigner[openID]; ok {
			continue
		}
		seenAssigner[openID] = struct{}{}
		assignerOpenIDs = append(assignerOpenIDs, openID)
	}
	personByOpenID := map[string]uint64{}
	if len(assignerOpenIDs) > 0 {
		var people []domain.Person
		if err := s.db.WithContext(ctx).Select("id", "open_id").
			Where("open_id IN ? AND is_active = ?", assignerOpenIDs, true).Find(&people).Error; err != nil {
			return nil, fmt.Errorf("load open todo assigners group_id=%d: %w", groupID, err)
		}
		for _, person := range people {
			personByOpenID[person.OpenID] = person.ID
		}
	}
	result := make([]OpenTodoContext, len(rows))
	for i := range rows {
		item := OpenTodoContext{ID: rows[i].ID, ActionType: rows[i].ActionType, Title: rows[i].Title, Status: rows[i].Status}
		if rows[i].AssignerOpenID != nil {
			openID := strings.TrimSpace(*rows[i].AssignerOpenID)
			if openID != "" {
				item.AssignerOpenID = &openID
				if personID, ok := personByOpenID[openID]; ok {
					item.AssignerPersonID = &personID
				}
			}
		}
		result[i] = item
	}
	return result, nil
}

// loadRecentTasks returns tasks that recently progressed and belong to this
// conversation via their source todo (same group or same project). Tasks without
// a todo (scheduled/manual) are excluded on purpose.
func (s *PipelineStore) loadRecentTasks(ctx context.Context, group GroupContext, limit int) ([]RecentTaskContext, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("recent task limit must be positive")
	}
	query := s.db.WithContext(ctx).Table("task AS t").
		Joins("JOIN todo AS td ON td.id = t.todo_id").
		Where("t.todo_id IS NOT NULL")
	switch {
	case group.ProjectID != nil:
		query = query.Where("td.group_id = ? OR td.project_id = ?", group.ID, *group.ProjectID)
	default:
		query = query.Where("td.group_id = ?", group.ID)
	}
	type row struct {
		ID             uint64
		Title          string
		Status         string
		Summary        *string
		LastProgressAt *time.Time
	}
	var rows []row
	if err := query.Select("t.id, t.title, t.status, t.summary, t.last_progress_at").
		Order("COALESCE(t.last_progress_at, t.created_at) DESC, t.id DESC").
		Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load recent tasks group_id=%d: %w", group.ID, err)
	}
	result := make([]RecentTaskContext, len(rows))
	for i := range rows {
		item := RecentTaskContext{ID: rows[i].ID, Title: rows[i].Title, Status: rows[i].Status}
		if rows[i].Summary != nil {
			item.Summary = *rows[i].Summary
		}
		if rows[i].LastProgressAt != nil {
			item.LastProgressAt = rows[i].LastProgressAt.UTC().Format(time.RFC3339)
		}
		result[i] = item
	}
	return result, nil
}

func messageContext(message *domain.Message, isNew bool) MessageContext {
	return MessageContext{
		DatabaseID: message.ID, MessageID: message.MessageID, ChatID: message.ChatID,
		SenderOpenID: message.SenderOpenID, SenderName: message.SenderName, SenderType: message.SenderType,
		Source: message.Source, MessageType: message.MessageType, Content: message.Content,
		RootID: stringValue(message.RootID), ThreadID: stringValue(message.ThreadID),
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
		Status: project.Status, Priority: project.Priority, Description: stringValue(project.Description),
		Repos: append([]byte(nil), project.Repos...), TechStack: append([]byte(nil), project.TechStack...),
		KeyDecisions: append([]byte(nil), project.KeyDecisions...), Timeline: append([]byte(nil), project.Timeline...),
		Notes: stringValue(project.Notes),
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
