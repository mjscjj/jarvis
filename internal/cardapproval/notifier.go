package cardapproval

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

type larkRunner interface {
	Run(context.Context, any, ...string) error
}

// Notifier sends the approval card only after the proposal has been durably
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
		return nil, fmt.Errorf("card approval lark client is nil")
	}
	agentName = strings.TrimSpace(agentName)
	if err := agentidentity.ValidateName(agentName); err != nil {
		return nil, fmt.Errorf("card approval agent name: %w", err)
	}
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return nil, fmt.Errorf("card approval principal open_id is empty")
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(serverAddr))
	if err != nil || port == "" {
		return nil, fmt.Errorf("card approval server address %q is invalid", serverAddr)
	}
	if resolveLANIPv4 == nil {
		return nil, fmt.Errorf("card approval LAN IPv4 resolver is nil")
	}
	return &Notifier{lark: lark, agentName: agentName, principal: principalOpenID, serverPort: port, resolveLANIPv4: resolveLANIPv4}, nil
}

func (n *Notifier) SendApproval(ctx context.Context, notice execute.ApprovalNotification) (*execute.ApprovalDelivery, error) {
	if notice.TaskID == 0 || notice.RunID == 0 || notice.Version <= 0 {
		return nil, fmt.Errorf("approval notification task/run/version is invalid")
	}
	for name, value := range map[string]string{
		"title": notice.Title, "action": notice.Action, "target": notice.Target, "artifact": notice.Artifact,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("approval notification %s is empty", name)
		}
	}
	detailURL, err := n.detailURL(notice.TaskID)
	if err != nil {
		return nil, err
	}
	card := approvalCard(notice, detailURL, n.agentName)
	content, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("encode approval card task_id=%d: %w", notice.TaskID, err)
	}
	var response any
	if err := n.lark.Run(ctx, &response,
		"im", "+messages-send",
		"--user-id", n.principal,
		"--msg-type", "interactive",
		"--content", string(content),
		"--idempotency-key", fmt.Sprintf("jarvis-approval-%d-v%d", notice.TaskID, notice.Version),
		"--as", "bot",
	); err != nil {
		return nil, fmt.Errorf("send approval card task_id=%d version=%d: %w", notice.TaskID, notice.Version, err)
	}
	messageIDs := distinctMessageIDs(response)
	if len(messageIDs) != 1 {
		return nil, fmt.Errorf("send approval card task_id=%d returned %d message_ids, want exactly one", notice.TaskID, len(messageIDs))
	}
	return &execute.ApprovalDelivery{
		MessageID: messageIDs[0],
		Target:    n.principal,
		Preview:   truncateRunes(notice.Artifact, 160),
		URL:       detailURL,
	}, nil
}

func (n *Notifier) detailURL(taskID uint64) (string, error) {
	ip, err := n.resolveLANIPv4()
	if err != nil {
		return "", fmt.Errorf("resolve approval detail LAN IPv4: %w", err)
	}
	ip = ip.To4()
	if ip == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() {
		return "", fmt.Errorf("resolved approval detail address %q is not a private LAN IPv4", ip)
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

func approvalCard(notice execute.ApprovalNotification, detailURL, agentName string) map[string]any {
	decisionValue := func(decision string) map[string]any {
		return map[string]any{
			"action": "jarvis_approval", "decision": decision,
			"task_id": notice.TaskID, "version": notice.Version,
		}
	}
	button := func(text, style, decision string) map[string]any {
		return map[string]any{
			"tag": "button", "text": map[string]any{"tag": "plain_text", "content": text},
			"type": style, "width": "fill",
			"behaviors": []any{map[string]any{"type": "callback", "value": decisionValue(decision)}},
		}
	}
	approve := button("确认", "primary_filled", "approve")
	approve["confirm"] = map[string]any{
		"title": map[string]any{"tag": "plain_text", "content": "确认执行？"},
		"text":  map[string]any{"tag": "plain_text", "content": fmt.Sprintf("确认后 %s 会按卡片中的方案继续执行。", agentName)},
	}
	reject := button("拒绝", "danger", "reject")
	column := func(element map[string]any) map[string]any {
		return map[string]any{"tag": "column", "width": "weighted", "weight": 1, "elements": []any{element}}
	}
	details := map[string]any{
		"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "查看详情"},
		"type": "default", "width": "fill",
		"behaviors": []any{map[string]any{"type": "open_url", "default_url": detailURL}},
	}
	body := fmt.Sprintf("**要做的事**\n%s\n\n**作用对象**\n%s\n\n**待执行内容**\n%s", strings.TrimSpace(notice.Action), strings.TrimSpace(notice.Target), truncateRunes(strings.TrimSpace(notice.Artifact), 1200))
	if summary := strings.TrimSpace(notice.Summary); summary != "" {
		body += "\n\n**判断**\n" + summary
	}
	elements := []any{map[string]any{"tag": "markdown", "content": body}}
	if followup := strings.TrimSpace(notice.NeedsFollowup); followup != "" {
		// Feishu recognises a submit button only among the form container's
		// direct children and only when it carries no behaviors: either a
		// column_set wrapper or a behaviors array makes card creation fail with
		// 300123 "there is no submit button in the form container". So the
		// buttons stack instead of sharing one row, and their callback payload
		// moves to the top-level value the form submission returns.
		markSubmit := func(control map[string]any, name, decision string) {
			control["name"] = name
			control["action_type"] = "form_submit"
			delete(control, "behaviors")
			control["value"] = decisionValue(decision)
		}
		markSubmit(approve, "jarvis_approval_approve", "approve")
		markSubmit(reject, "jarvis_approval_reject", "reject")
		details["name"], details["action_type"] = "jarvis_approval_details", "link"
		elements = append(elements, map[string]any{
			"tag": "form", "name": "jarvis_approval_form",
			"elements": []any{
				map[string]any{"tag": "markdown", "content": "**需要你补充**\n" + followup},
				map[string]any{
					"tag": "input", "name": "approval_note", "width": "fill", "max_length": 500,
					"placeholder": map[string]any{"tag": "plain_text", "content": "可填写补充说明或驳回原因"},
				},
				approve, reject, details,
			},
		})
	} else {
		elements = append(elements, map[string]any{
			"tag": "column_set", "flex_mode": "flow", "horizontal_spacing": "medium",
			"columns": []any{column(approve), column(reject), column(details)},
		})
	}
	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "需要你拍板：" + strings.TrimSpace(notice.Title)},
			"subtitle": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("任务 #%d", notice.TaskID)},
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
