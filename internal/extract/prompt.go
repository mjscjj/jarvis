package extract

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"jarvis/internal/sharedmem"
)

type PromptOptions struct {
	PrincipalOpenID string
	Location        *time.Location
	MaxChars        int
	// ToolGuidance, when non-empty, appends a "你可用的工具" section telling the codex
	// engine which shell tools it may self-run to fill in missing context. It is
	// empty for the kimi engine (which uses Go function-calling instead).
	ToolGuidance string
	// SharedMemory 是可信共享记忆文本（见 internal/sharedmem）。非空时以 RenderBlock
	// 渲染后追加到 system 段末尾（受信任指令区）；为空则不注入。
	SharedMemory string
	// WorkRules 是已按 extract 阶段过滤并渲染好的可信工作规则 block。
	WorkRules string
}

// CodexToolGuidance is the tool section injected for the codex engine. codex is
// a full agent that can run shell; this lists the tools it may self-run while
// doing its homework (职责二) and reminds it to fold findings into context.
const CodexToolGuidance = `你本地可信、能执行 shell。替我把功课做足时，需要什么就自己去查，把查到的关键事实和链接写进 context：
- ` + "`jarvis-tools <子命令>`" + `：查项目/仓库/人物/群的归属与背景，输出 JSON。可用子命令：list-projects、get-project --id N | --code C、get-group --chat-id ID、get-principal、get-person --open-id ID。
- 共享记忆（所有 agent 共用的踩坑/关键约定/凭据）：` + "`jarvis-tools get-shared-memory`" + ` 查看；发现对后续任务有用的关键事实/凭据/约定，或踩到坑（权限缺失、环境陷阱）时，用 ` + "`jarvis-tools append-shared-memory --note -`" + `（长文本走 stdin）追加一条，让后续 agent 复用；别写一次性琐碎信息。
- ` + "`lark-cli`" + `：查飞书信息（群/文档/日历/成员/会议纪要等）。优先 ` + "`+shortcut`" + ` 高层命令，其次 ` + "`<domain> <resource> <method>`" + `，最后 ` + "`api GET <path>`" + ` 兜底。只读查询直接跑；写操作(high-risk-write)要 ` + "`--yes`" + ` 且必须先经我确认。多数 +shortcut 需带 ` + "`--as user`" + `。常用（不确定参数先 ` + "`lark-cli <domain> --help`" + `）：
    - 姓名转 open_id：` + "`lark-cli contact +search-user --query 姓名 --as user`" + `
    - 按群名找 chat_id：` + "`lark-cli im +chat-search --query 群名 --as user`" + `
    - 拉群/私聊消息：` + "`lark-cli im +chat-messages-list --chat-id oc_xxx --as user`" + `
    - 群成员：` + "`lark-cli im +chat-members-list --chat-id oc_xxx --as user`" + `
    - 读文档正文：` + "`lark-cli docs +fetch --doc <飞书文档URL或token> --as user`" + `；搜文档：` + "`lark-cli docs +search --query 关键词 --as user`" + `
    - 日历：` + "`lark-cli calendar +agenda --as user`" + `
    - ` + "`--jq <expr>`" + ` 过滤输出，` + "`--dry-run`" + ` 只预览不执行。
- ` + "`bytedcli`" + `：查内部研发信息（代码/commit/MR/issue）。加 ` + "`-j`" + ` 输出纯 JSON。命令形如 ` + "`bytedcli codebase <资源> <动作>`" + `，仓库用 ` + "`-R <repo>`" + ` 指定（在仓库目录内可省略，默认取当前 git origin）。常用（不确定先 ` + "`bytedcli codebase <资源> --help`" + `）：
    - 找仓库：` + "`bytedcli codebase repo list --query 关键词 -j`" + `；看仓库：` + "`bytedcli codebase repo get <namespace/repo> -j`" + `
    - 看提交：` + "`bytedcli codebase commit list -R <repo> -j`" + `
    - 搜 issue/MR：` + "`bytedcli codebase search issue --query 关键词 -j`" + ` / ` + "`bytedcli codebase search mr --query 关键词 -j`" + `
- ` + "`git`" + `：本地仓库信息（` + "`git -C <repo> log --oneline -20`" + ` 看近期提交、` + "`git -C <repo> remote -v`" + ` 看远端）。
project_hint：能确定线索归属的项目就把项目 code 或 name 填进去（优先用 jarvis-tools list-projects 里存在的 code），确定不了填 null。`

const systemPromptTemplate = `你是 principal（open_id=%s，也就是「我」）的私人管家和参谋。
你存在的意义只有一个：让我更省心、更高效。我每天泡在很多飞书群里，信息太多、待办太杂，你替我盯着这些对话，把值得我处理的事拎出来，并且在把它交到我面前之前，尽你所能替我把功课做足。
我的详细背景见用户消息「# 我的背景(principal)」区块，请以该区块为准判断「谁是我、我负责什么、我的直属 leader 是谁」。

你眼前是我参与的一段会话。请像一个真正懂我、又能干的管家那样工作：

一、替我发现值得处理的事
   从对话里识别出我需要亲自做、或你可以替我推进的行动线索——别人明确交办给我的、我自己承诺要做的、或明显在等我表态推进的事。leader 发出的即使措辞较软也要拎出来。闲聊、情绪、与我无关的讨论就跳过。拿不准的宁可少拎，别硬凑；没有值得处理的事就返回 candidates=[]。
   每条事都要能追溯到对话里的具体原话：source_quote 从某条 [new] 消息里逐字连续复制（原文的 exact contiguous substring，不要改写、补字或拼接多条），source_message_ids 指向它，这样我一眼就知道这事从哪来的。

二、替我把功课做足（这是你最有价值的地方）
   把一件事摆到我面前之前，先站在我的角度想：我要推进它，需要先知道什么？然后主动去把这些背景查清楚、想明白，写进 context：
   - 这事归属哪个项目、涉及哪个仓库、牵扯到谁、和哪些系统相关；
   - 相关的代码、commit、文档、会议、历史决定——有链接就把链接找出来给我；
   - 任何能让我少点几下、少问几句就能上手的信息。
   你手上有工具（见下方「可用工具与自查指引」），需要什么就自己去查。记住：能自己查明白的，就别留着来问我。你查得越多，我越省心。

三、只把真正需要我拍板的留给我
   如果有些事你查遍了也确定不了、必须由我本人决定或提供（比如只有我知道的意图、需要我权衡的取舍），就写进 open_questions，问得具体、让我能直接回答。没有这种就让 open_questions 留空——那说明你已经替我搞定了，这最好。

其余字段：
- action_type：给这件事归一个类别（code_change/summary_post/investigate/schedule_meeting/reply_message/doc_write/manual_followup）。
- target：用一句话点出这件事的对象/主题，作为去重标识。
- commitment_strength：firm=明确承诺/交办，tentative=软建议待确认，mentioned=仅提及无归属。
- due_date：相对时间按当前时间与时区解析为 YYYY-MM-DD，无明确时间填 null。
- 同一件事在多条消息重复出现时合并成一条。

只输出约定的 JSON，不输出解释。`

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
	if guidance := strings.TrimSpace(opts.ToolGuidance); guidance != "" {
		system += "\n\n可用工具与自查指引：\n" + guidance
	}
	if block := sharedmem.RenderBlock(opts.SharedMemory); block != "" {
		system += "\n\n" + block
	}
	if block := strings.TrimSpace(opts.WorkRules); block != "" {
		system += "\n\n" + block
	}
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

// salientQueryMaxMessages caps how many of the most recent extractable [new]
// messages feed the memory-retrieval query. Concatenating the whole unit makes
// the query long and noisy, hurting recall; the latest messages carry the
// actionable intent, so only the last N are used.
const salientQueryMaxMessages = 20

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
	if len(parts) > salientQueryMaxMessages {
		parts = parts[len(parts)-salientQueryMaxMessages:]
	}
	return strings.Join(parts, "\n"), nil
}

func renderUserPrompt(batch ChatBatch, unit ConversationUnit, memories []map[string]any, now time.Time, location *time.Location) string {
	sections := []string{
		"# 当前时间\n" + now.Format(time.RFC3339) + "（时区 " + location.String() + "）",
		"# 我的背景(principal)\n" + renderPrincipal(batch.Principal),
		"# 当前会话所属项目（详细）\n" + renderProject(batch.Project),
		"# 我的其他项目（精简，仅作归属参考）\n" + renderOtherProjects(batch.OtherProjects),
		"# 来源会话（Group）\n" + fmt.Sprintf("chat_id=%s name=%q is_key_group=%t project_id=%s", batch.Group.ChatID, batch.Group.Name, batch.Group.IsKeyGroup, uint64PointerText(batch.Group.ProjectID)),
		"# 参与者\n" + renderParticipants(unit.Participants),
		"# 相关资源\n" + renderResources(unit.Resources),
		"# 相关记忆（仅作背景）\n" + renderMemories(memories),
		"# 已存在的未闭环 Todo（仅作背景）\n" + renderOpenTodos(batch.OpenTodos),
		"# 会话记录\n" + renderConversation(unit.Messages, location),
	}
	return strings.Join(sections, "\n\n")
}

func renderPrincipal(principal *PrincipalContext) string {
	if principal == nil {
		return "(未设置——请在后台「我」中完善决策主体背景)"
	}
	parts := []string{fmt.Sprintf("open_id=%s name=%q", principal.OpenID, principal.Name)}
	if principal.Department != "" {
		parts = append(parts, fmt.Sprintf("department=%q", principal.Department))
	}
	if principal.Title != "" {
		parts = append(parts, fmt.Sprintf("title=%q", principal.Title))
	}
	if principal.LeaderOpenID != "" {
		parts = append(parts, fmt.Sprintf("leader_open_id=%s leader_name=%q", principal.LeaderOpenID, principal.LeaderName))
	}
	line := strings.Join(parts, " ")
	if principal.Background != "" {
		line += "\n负责方向/背景：" + principal.Background
	}
	if principal.Preferences != "" {
		line += "\n喜好/工作偏好：" + principal.Preferences
	}
	return line
}

func renderOtherProjects(projects []OtherProjectContext) string {
	if len(projects) == 0 {
		return "(none)"
	}
	lines := make([]string, len(projects))
	for i, project := range projects {
		line := fmt.Sprintf("id=%d code=%q name=%q role=%s", project.ID, project.Code, project.Name, project.Role)
		if project.Description != "" {
			line += fmt.Sprintf(" desc=%q", project.Description)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
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
		line := fmt.Sprintf("open_id=%s name=%q role=%s is_leader=%t", participant.OpenID, participant.Name, participant.Role, participant.IsLeader)
		if participant.Relation != "" {
			line += fmt.Sprintf(" relation=%q", participant.Relation)
		}
		if participant.CommStyle != "" {
			line += fmt.Sprintf(" comm_style=%q", participant.CommStyle)
		}
		lines[i] = line
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
	return FilterMemoriesForSnapshot(memories)
}

// FilterMemoriesForSnapshot drops M3's own memories (metadata.source == "m3") so
// neither the prompt nor the frozen context_snapshot self-reinforces.
func FilterMemoriesForSnapshot(memories []map[string]any) []map[string]any {
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
