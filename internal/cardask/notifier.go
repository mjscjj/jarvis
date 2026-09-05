package cardask

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"unicode/utf8"

	"jarvis/internal/agentidentity"
	"jarvis/internal/execute"
)

// callbackAction is the action name CC Connect routes on. It is historical and
// deliberately unchanged: renaming it would require re-patching and
// reinstalling CC Connect on every machine for no behavior difference.
const callbackAction = "jarvis_approval"

type larkRunner interface {
	Run(context.Context, any, ...string) error
}

// Notifier sends the question card only after the question has been durably
// parked. It is intentionally a direct call: no outbox, polling, or fallback
// path is needed for this local single-user runtime.
type Notifier struct {
	lark           larkRunner
	agentName      string
	principal      string
	serverPort     string
	resolveLANIPv4 func() (net.IP, error)
}

func NewNotifier(lark larkRunner, agentName, principalOpenID, serverAddr string) (*Notifier, error) {
	return newNotifier(lark, agentName, principalOpenID, serverAddr, currentLANIPv4)
}

func newNotifier(lark larkRunner, agentName, principalOpenID, serverAddr string, resolveLANIPv4 func() (net.IP, error)) (*Notifier, error) {
	if lark == nil {
		return nil, fmt.Errorf("question card lark client is nil")
	}
	agentName = strings.TrimSpace(agentName)
	if err := agentidentity.ValidateName(agentName); err != nil {
		return nil, fmt.Errorf("question card agent name: %w", err)
	}
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return nil, fmt.Errorf("question card principal open_id is empty")
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(serverAddr))
	if err != nil || port == "" {
		return nil, fmt.Errorf("question card server address %q is invalid", serverAddr)
	}
	if resolveLANIPv4 == nil {
		return nil, fmt.Errorf("question card LAN IPv4 resolver is nil")
	}
	return &Notifier{lark: lark, agentName: agentName, principal: principalOpenID, serverPort: port, resolveLANIPv4: resolveLANIPv4}, nil
}

func (n *Notifier) SendQuestion(ctx context.Context, notice execute.QuestionNotification) (*execute.QuestionDelivery, error) {
	if notice.TaskID == 0 || notice.RunID == 0 || notice.Version <= 0 {
		return nil, fmt.Errorf("question notification task/run/version is invalid")
	}
	detailURL, err := n.detailURL(notice.TaskID)
	if err != nil {
		return nil, err
	}
	content, err := json.Marshal(questionCard(notice, detailURL, ""))
	if err != nil {
		return nil, fmt.Errorf("encode question card task_id=%d: %w", notice.TaskID, err)
	}
	var response any
	if err := n.lark.Run(ctx, &response,
		"im", "+messages-send",
		"--user-id", n.principal,
		"--msg-type", "interactive",
		"--content", string(content),
		"--idempotency-key", fmt.Sprintf("jarvis-ask-%d-v%d", notice.TaskID, notice.Version),
		"--as", "bot",
	); err != nil {
		return nil, fmt.Errorf("send question card task_id=%d version=%d: %w", notice.TaskID, notice.Version, err)
	}
	messageIDs := distinctMessageIDs(response)
	if len(messageIDs) != 1 {
		return nil, fmt.Errorf("send question card task_id=%d returned %d message_ids, want exactly one", notice.TaskID, len(messageIDs))
	}
	return &execute.QuestionDelivery{
		MessageID: messageIDs[0],
		Target:    n.principal,
		Preview:   truncateRunes(notice.Question.Title, 160),
		URL:       detailURL,
	}, nil
}

// AnsweredCard re-renders the question card in its answered state. CC Connect
// replaces the original message with this card wholesale, so Jarvis owns the
// card content end to end and never reads it back from Feishu.
func (n *Notifier) AnsweredCard(notice execute.QuestionNotification, outcome string) (json.RawMessage, error) {
	if strings.TrimSpace(outcome) == "" {
		return nil, fmt.Errorf("answered question card outcome is empty")
	}
	detailURL, err := n.detailURL(notice.TaskID)
	if err != nil {
		return nil, err
	}
	content, err := json.Marshal(questionCard(notice, detailURL, outcome))
	if err != nil {
		return nil, fmt.Errorf("encode answered question card task_id=%d: %w", notice.TaskID, err)
	}
	return content, nil
}

func (n *Notifier) detailURL(taskID uint64) (string, error) {
	ip, err := n.resolveLANIPv4()
	if err != nil {
		return "", fmt.Errorf("resolve question detail LAN IPv4: %w", err)
	}
	ip = ip.To4()
	if ip == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() {
		return "", fmt.Errorf("resolved question detail address %q is not a private LAN IPv4", ip)
	}
	return fmt.Sprintf("http://%s/#/work/task/%d", net.JoinHostPort(ip.String(), n.serverPort), taskID), nil
}

func currentLANIPv4() (net.IP, error) {
	connection, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("1.1.1.1"), Port: 53})
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	address, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok || address.IP == nil {
		return nil, fmt.Errorf("default route returned no local IPv4")
	}
	return address.IP, nil
}

// questionCard renders the pending question when outcome is empty and the
// answered card once outcome carries what the principal chose. Both states are
// built from the same persisted notification, so the answered card repeats the
// exact wording the principal read without Feishu having to hand the original
// card back.
//
// Everything the principal can interact with lives inside one form container:
// Feishu returns its inputs as form_value in a single callback, so one click
// carries both the chosen button and whatever was typed or selected.
func questionCard(notice execute.QuestionNotification, detailURL, outcome string) map[string]any {
	question := notice.Question
	body := strings.TrimSpace(question.Body)
	if summary := strings.TrimSpace(notice.Summary); summary != "" {
		body = strings.TrimSpace(body + "\n\n**目前进展**\n" + summary)
	}
	elements := []any{}
	if body != "" {
		elements = append(elements, markdown(truncateRunes(body, 2000)))
	}

	details := map[string]any{
		"tag": "button", "text": plainText("查看详情"),
		"type": "default", "width": "fill",
		"behaviors": []any{map[string]any{"type": "open_url", "default_url": detailURL}},
	}
	if outcome = strings.TrimSpace(outcome); outcome != "" {
		return card(append(elements, markdown(outcome), details), notice)
	}

	controls := make([]any, 0, len(question.Fields)+1)
	for _, field := range question.Fields {
		controls = append(controls, renderField(field, notice)...)
	}
	details["name"], details["action_type"] = "jarvis_ask_details", "link"
	controls = append(controls, details)
	elements = append(elements, map[string]any{
		"tag": "form", "name": "jarvis_ask_form", "elements": controls,
	})
	return card(elements, notice)
}

// renderField maps one question field to its Feishu elements. Labels for
// select/multi_select are rendered as their own markdown line because those
// elements have no label attribute of their own.
func renderField(field execute.QuestionField, notice execute.QuestionNotification) []any {
	switch field.Type {
	case execute.FieldButton:
		// A form's submit button must be a direct child of the form and must
		// carry no behaviors, otherwise card creation fails with 300123 "there
		// is no submit button in the form container". Its callback payload
		// therefore rides on the top-level value the form submission returns.
		return []any{map[string]any{
			"tag": "button", "name": field.Name, "action_type": "form_submit",
			"text": plainText(field.Label), "type": buttonStyle(field.Style), "width": "fill",
			"value": map[string]any{
				"action": callbackAction, "task_id": notice.TaskID,
				"version": notice.Version, "clicked": field.Name,
			},
		}}
	case execute.FieldSelect:
		return []any{markdown("**" + field.Label + "**"), map[string]any{
			"tag": "select_static", "name": field.Name, "width": "fill",
			"placeholder": plainText("请选择"), "options": selectOptions(field.Options),
		}}
	case execute.FieldMultiSelect:
		return []any{markdown("**" + field.Label + "**"), map[string]any{
			"tag": "multi_select_static", "name": field.Name, "width": "fill",
			"placeholder": plainText("可多选"), "options": selectOptions(field.Options),
		}}
	case execute.FieldInput:
		return []any{map[string]any{
			"tag": "input", "name": field.Name, "width": "fill", "max_length": 1000,
			"label": plainText(field.Label), "placeholder": plainText("可不填"),
		}}
	case execute.FieldLink:
		// A link answers nothing, so the model is not asked to name it. Feishu
		// rejects an empty name inside a form container, so the key is omitted
		// rather than sent blank.
		link := map[string]any{
			"tag": "button", "action_type": "link",
			"text": plainText(field.Label), "type": "default", "width": "fill",
			"behaviors": []any{map[string]any{"type": "open_url", "default_url": field.URL}},
		}
		if field.Name != "" {
			link["name"] = field.Name
		}
		return []any{link}
	}
	// ParseQuestion rejects unknown types before a card is ever rendered.
	return nil
}

func buttonStyle(style string) string {
	switch strings.TrimSpace(style) {
	case "primary":
		return "primary_filled"
	case "danger":
		return "danger"
	default:
		return "default"
	}
}

func selectOptions(options []string) []any {
	rendered := make([]any, 0, len(options))
	for _, option := range options {
		rendered = append(rendered, map[string]any{"text": plainText(option), "value": option})
	}
	return rendered
}

func markdown(content string) map[string]any {
	return map[string]any{"tag": "markdown", "content": content}
}

func plainText(content string) map[string]any {
	return map[string]any{"tag": "plain_text", "content": content}
}

func card(elements []any, notice execute.QuestionNotification) map[string]any {
	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    plainText(truncateRunes(strings.TrimSpace(notice.Question.Title), 80)),
			"subtitle": plainText(fmt.Sprintf("任务 #%d · %s", notice.TaskID, truncateRunes(strings.TrimSpace(notice.TaskTitle), 40))),
			"template": "orange",
		},
		"body": map[string]any{
			"direction": "vertical", "padding": "12px 12px 20px 12px",
			"elements": elements,
		},
	}
}

func distinctMessageIDs(value any) []string {
	set := make(map[string]struct{})
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if key == "message_id" {
					if id, ok := nested.(string); ok && strings.HasPrefix(strings.TrimSpace(id), "om_") {
						set[strings.TrimSpace(id)] = struct{}{}
					}
				}
				visit(nested)
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		case json.RawMessage:
			var nested any
			if json.Unmarshal(typed, &nested) == nil {
				visit(nested)
			}
		}
	}
	visit(value)
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…（完整内容见任务详情）"
}
