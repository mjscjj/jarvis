package extract

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type PromptOptions struct {
	PrincipalOpenID string
	Location        *time.Location
	MaxChars        int
}

const systemPromptTemplate = `你是「个人 Jarvis 管家」的行动线索抽取器，服务对象是研发工程师 chujiejie.1（open_id=%s）。
你的唯一任务：从给定飞书会话中抽取 principal 需要执行、或其助手可代其执行的真实、可落地行动线索。输出必须严格符合 JSON schema；你不做最终确认。

必须遵守：
1. 只抽取真实承诺、明确交办或明确行动倾向；忽略寒暄、情绪和无动作讨论。
2. leader 发出的行动线索必须输出，即使措辞较软；commitment_strength 如实填写。
3. 每条线索映射到唯一 action_type。slot 只能来自输入中的明确证据；缺失或歧义时设 info_sufficient=false，并把缺项写入 missing_info，禁止猜测。
4. 每条线索必须包含 source_message_ids 和逐字 source_quote；source_quote 必须从某条被引用的 [new] 消息中连续复制粘贴，必须是原文的 exact contiguous substring，不得改写、补字、纠错或拼接多条消息。至少一条证据必须标记为 [new]，禁止仅从 [context] 或背景生成线索。
5. 相对时间按输入的当前时间及时区解析为 YYYY-MM-DD；无明确时间则 due_date=null。
6. commitment_strength：firm=明确承诺/交办，tentative=软建议待确认，mentioned=仅提及无归属。
7. 同一件事在多条消息重复出现时合并证据，只输出一条。
8. 无行动线索时返回 candidates=[]。
9. Resource 只能引用输入中给出的标识。相关记忆和已有 Todo 仅作背景，不得直接当成新证据。

action_type 与必填 slot：
- code_change：repo_ref, change_summary
- summary_post：source_ref, target_chat_id, summary_scope
- investigate：question, lookup_sources
- schedule_meeting：meeting_title, attendees, proposed_time
- reply_message：target_chat_id, message_body
- doc_write：doc_title, summary_scope
- manual_followup：followup_action（具体且可验证）

只输出 JSON，不输出解释。`

func BuildPrompt(batch ChatBatch, unit ConversationUnit, memories []map[string]any, now time.Time, opts PromptOptions) (Prompt, error) {
	if strings.TrimSpace(opts.PrincipalOpenID) == "" {
		return Prompt{}, fmt.Errorf("extract principal open_id is empty")
	}
	if opts.Location == nil {
		return Prompt{}, fmt.Errorf("extract prompt location is nil")
	}
	if opts.MaxChars <= 0 {
		return Prompt{}, fmt.Errorf("extract prompt max chars must be positive")
	}
	if len(unit.Messages) == 0 {
		return Prompt{}, fmt.Errorf("extract conversation unit %q has no messages", unit.Key)
	}

	trimmed := unit
	trimmed.Messages = append([]MessageContext(nil), unit.Messages...)
	system := fmt.Sprintf(systemPromptTemplate, opts.PrincipalOpenID)
	filteredMemories := filterMemories(memories)
	for i, item := range filteredMemories {
		if _, err := json.Marshal(item); err != nil {
			return Prompt{}, fmt.Errorf("encode extraction memory index=%d: %w", i, err)
		}
	}
	for {
		user := renderUserPrompt(batch, trimmed, filteredMemories, now.In(opts.Location), opts.Location)
		if utf8.RuneCountInString(system)+utf8.RuneCountInString(user) <= opts.MaxChars {
			return Prompt{System: system, User: user}, nil
		}
		index := firstContextIndex(trimmed.Messages)
		if index < 0 {
			return Prompt{}, fmt.Errorf("extract prompt unit=%s exceeds max_chars=%d after removing all context messages", unit.Key, opts.MaxChars)
		}
		trimmed.Messages = append(trimmed.Messages[:index], trimmed.Messages[index+1:]...)
	}
}

func SalientQuery(unit ConversationUnit) (string, error) {
	parts := make([]string, 0)
	for _, message := range unit.Messages {
		if message.IsNew && message.Extractable {
			content := strings.TrimSpace(message.Content)
			if content != "" {
				parts = append(parts, content)
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("extract conversation unit %q has no extractable new messages", unit.Key)
	}
	return strings.Join(parts, "\n"), nil
}

func renderUserPrompt(batch ChatBatch, unit ConversationUnit, memories []map[string]any, now time.Time, location *time.Location) string {
	sections := []string{
		"# 当前时间\n" + now.Format(time.RFC3339) + "（时区 " + location.String() + "）",
		"# 项目背景\n" + renderProject(batch.Project),
		"# 来源会话（Group）\n" + fmt.Sprintf("chat_id=%s name=%q is_key_group=%t project_id=%s", batch.Group.ChatID, batch.Group.Name, batch.Group.IsKeyGroup, uint64PointerText(batch.Group.ProjectID)),
		"# 参与者\n" + renderParticipants(unit.Participants),
		"# 相关资源\n" + renderResources(unit.Resources),
		"# 相关记忆（仅作背景）\n" + renderMemories(memories),
		"# 已存在的未闭环 Todo（仅作背景）\n" + renderOpenTodos(batch.OpenTodos),
		"# 会话记录\n" + renderConversation(unit.Messages, location),
	}
	return strings.Join(sections, "\n\n")
}

func renderProject(project *ProjectContext) string {
	if project == nil {
		return "(none)"
	}
	return fmt.Sprintf("id=%d code=%q name=%q role=%s description=%q repos=%s key_decisions=%s",
		project.ID, project.Code, project.Name, project.Role, project.Description, jsonOrNull(project.Repos), jsonOrNull(project.KeyDecisions))
}

func renderParticipants(participants []ParticipantContext) string {
	if len(participants) == 0 {
		return "(none)"
	}
	lines := make([]string, len(participants))
	for i, participant := range participants {
		lines[i] = fmt.Sprintf("open_id=%s name=%q role=%s is_leader=%t", participant.OpenID, participant.Name, participant.Role, participant.IsLeader)
	}
	return strings.Join(lines, "\n")
}

func renderResources(resources []ResourceContext) string {
	if len(resources) == 0 {
		return "(none)"
	}
	lines := make([]string, len(resources))
	for i, resource := range resources {
		lines[i] = fmt.Sprintf("id=%d type=%s file_key=%q minute_token=%q doc_token=%q url=%q name=%q extracted_text=%q",
			resource.ID, resource.ResourceType, resource.FileKey, resource.MinuteToken, resource.DocToken, resource.URL, resource.Name, resource.ExtractedText)
	}
	return strings.Join(lines, "\n")
}

func renderMemories(memories []map[string]any) string {
	if len(memories) == 0 {
		return "(none)"
	}
	lines := make([]string, 0, len(memories))
	for _, item := range memories {
		encoded, err := json.Marshal(item)
		if err != nil {
			lines = append(lines, fmt.Sprintf("(invalid memory: %v)", err))
			continue
		}
		lines = append(lines, string(encoded))
	}
	return strings.Join(lines, "\n")
}

func renderOpenTodos(todos []OpenTodoContext) string {
	if len(todos) == 0 {
		return "(none)"
	}
	lines := make([]string, len(todos))
	for i, todo := range todos {
		lines[i] = fmt.Sprintf("todo_id=%d action_type=%s title=%q status=%s", todo.ID, todo.ActionType, todo.Title, todo.Status)
	}
	return strings.Join(lines, "\n")
}

func renderConversation(messages []MessageContext, location *time.Location) string {
	lines := make([]string, len(messages))
	for i, message := range messages {
		kind := "context"
		if message.IsNew {
			kind = "new"
		}
		content := strings.ReplaceAll(strings.TrimSpace(message.Content), "\r\n", "\n")
		content = strings.ReplaceAll(content, "\n", "\n    ")
		lines[i] = fmt.Sprintf("[%s] msg_id=%s time=%s sender_open_id=%s is_leader=%t sender_name=%q: %s",
			kind, message.MessageID, time.UnixMilli(message.CreateTime).In(location).Format(time.RFC3339),
			message.SenderOpenID, message.IsLeader, message.SenderName, content)
	}
	return strings.Join(lines, "\n")
}

func filterMemories(memories []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(memories))
	for _, item := range memories {
		metadata, _ := item["metadata"].(map[string]any)
		if source, _ := metadata["source"].(string); source == "m3" {
			continue
		}
		result = append(result, item)
	}
	return result
}

func firstContextIndex(messages []MessageContext) int {
	for i, message := range messages {
		if !message.IsNew {
			return i
		}
	}
	return -1
}

func jsonOrNull(value []byte) string {
	if len(value) == 0 {
		return "null"
	}
	return string(value)
}

func uint64PointerText(value *uint64) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprintf("%d", *value)
}
