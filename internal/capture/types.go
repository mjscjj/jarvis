// Package capture implements M2 Feishu conversation discovery and incremental
// message capture.
package capture

// ChatListResponse mirrors lark-cli im +chat-list.
type ChatListResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Chats     []CLIChat `json:"chats"`
		HasMore   bool      `json:"has_more"`
		PageToken string    `json:"page_token"`
	} `json:"data"`
}

// CLIChat is the subset of chat metadata persisted by M2.
type CLIChat struct {
	ChatID      string `json:"chat_id"`
	ChatMode    string `json:"chat_mode"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerID     string `json:"owner_id"`
	External    bool   `json:"external"`
	TenantKey   string `json:"tenant_key"`
	// P2PTargetType 区分私聊对端类型：user=真人，bot=服务号/机器人。
	// 只有真人私聊才是自动纳入监听的候选；服务号私聊即便 external=false 也排除。
	P2PTargetType string `json:"p2p_target_type"`
}

// MessageListResponse mirrors lark-cli im +chat-messages-list.
type MessageListResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Messages  []CLIMessage `json:"messages"`
		HasMore   bool         `json:"has_more"`
		PageToken string       `json:"page_token"`
		Total     int          `json:"total"`
	} `json:"data"`
}

// MessageSearchResponse mirrors lark-cli im +messages-search. The shortcut
// handles server pagination itself when called with --page-all.
type MessageSearchResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Messages  []SearchedMessage `json:"messages"`
		HasMore   bool              `json:"has_more"`
		PageToken string            `json:"page_token"`
		Total     int               `json:"total"`
	} `json:"data"`
}

// MessageSearchListResponse is the full rendered message shape returned by
// lark-cli im +messages-search for one related group or topic chat. It is separate from
// MessageSearchResponse because principal-activity discovery only needs chat
// metadata, while ordinary M2 capture persists the complete message payload.
type MessageSearchListResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Messages  []CLIMessage `json:"messages"`
		HasMore   bool         `json:"has_more"`
		PageToken string       `json:"page_token"`
		Total     int          `json:"total"`
	} `json:"data"`
}

// SearchedMessage contains only the fields required to identify a group where
// the principal spoke and to bound that group's first capture window.
type SearchedMessage struct {
	ChatID     string    `json:"chat_id"`
	ChatName   string    `json:"chat_name"`
	ChatType   string    `json:"chat_type"`
	CreateTime string    `json:"create_time"`
	MessageID  string    `json:"message_id"`
	Deleted    bool      `json:"deleted"`
	Sender     CLISender `json:"sender"`
}

// CLIMessage is the rendered message shape returned by lark-cli 1.0.72.
type CLIMessage struct {
	ChatID        string       `json:"chat_id"`
	Content       string       `json:"content"`
	CreateTime    string       `json:"create_time"`
	MessageID     string       `json:"message_id"`
	MessageType   string       `json:"msg_type"`
	ParentID      string       `json:"parent_id"`
	RootID        string       `json:"root_id"`
	ThreadID      string       `json:"thread_id"`
	UpdateTime    string       `json:"update_time"`
	Updated       bool         `json:"updated"`
	Sender        CLISender    `json:"sender"`
	ThreadReplies []CLIMessage `json:"thread_replies"`
}

// CLISender covers both human senders and app/bot senders.
type CLISender struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OpenBotID  string `json:"open_bot_id"`
	SenderType string `json:"sender_type"`
}
