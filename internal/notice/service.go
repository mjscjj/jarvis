// Package notice delivers agent-authored cards to the configured principal.
// It owns presentation and delivery, never the decision to notify or Task state.
package notice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/uilink"

	"gorm.io/gorm"
)

var ErrInvalidInput = errors.New("invalid principal notice")

type runner interface {
	Run(context.Context, any, ...string) error
}

type Service struct {
	db        *gorm.DB
	lark      runner
	principal string
	auditPath string
	auditMu   sync.Mutex
	links     *uilink.Resolver
}

// cardInput is only a local rendering projection. The complete request is
// retained in the delivery audit; type and supplementary content remain open.
type cardInput struct {
	Type    string          `json:"type"`
	Content string          `json:"content"`
	Details string          `json:"details"`
	Extra   json.RawMessage `json:"extra"`
	Links   []struct {
		Label string `json:"label"`
		URL   string `json:"url"`
	} `json:"links"`
	TaskID         uint64 `json:"task_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type Delivery struct {
	MessageID string         `json:"message_id,omitempty"`
	ChatID    string         `json:"chat_id,omitempty"`
	URL       string         `json:"url,omitempty"`
	Verified  bool           `json:"verified"`
	Effect    map[string]any `json:"effect,omitempty"`
	// Preserve the upstream response even if sending or verification fails.
	SendResponse json.RawMessage `json:"send_response,omitempty"`
}

func NewService(db *gorm.DB, lark runner, principal, auditPath, serverAddr, publicURL string) (*Service, error) {
	if db == nil || lark == nil || strings.TrimSpace(principal) == "" || strings.TrimSpace(auditPath) == "" {
		return nil, fmt.Errorf("notice requires database, lark client, principal and audit path")
	}
	links, err := uilink.New(serverAddr, publicURL)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, lark: lark, principal: principal, auditPath: auditPath, links: links}, nil
}

func (s *Service) Send(ctx context.Context, raw json.RawMessage) (delivery *Delivery, err error) {
	in, card, err := prepare(raw, s.links)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if in.TaskID != 0 {
		if err := s.db.WithContext(ctx).First(&task, in.TaskID).Error; err != nil {
			return nil, fmt.Errorf("%w: task %d: %v", ErrInvalidInput, in.TaskID, err)
		}
	}
	delivery = &Delivery{}
	// Persist intent before sending; a failed audit must not trigger an action.
	if err := s.audit(ctx, task, raw, nil, ""); err != nil {
		return delivery, err
	}
	defer func() {
		errorText := ""
		if err != nil {
			errorText = err.Error()
		}
		// A disconnected caller must not erase evidence of a completed send.
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit(auditCtx, task, raw, delivery, errorText); auditErr != nil {
			err = errors.Join(err, auditErr)
		}
	}()

	var response struct {
		Data struct {
			MessageID string `json:"message_id"`
			ChatID    string `json:"chat_id"`
		} `json:"data"`
	}
	err = s.lark.Run(ctx, &delivery.SendResponse, "im", "+messages-send", "--user-id", s.principal,
		"--msg-type", "interactive", "--content", string(card), "--idempotency-key", in.IdempotencyKey, "--as", "bot")
	if len(delivery.SendResponse) > 0 {
		_ = json.Unmarshal(delivery.SendResponse, &response)
		delivery.MessageID, delivery.ChatID = response.Data.MessageID, response.Data.ChatID
	}
	if err != nil {
		return delivery, fmt.Errorf("send principal notice: %w", err)
	}
	if !strings.HasPrefix(delivery.MessageID, "om_") || !strings.HasPrefix(delivery.ChatID, "oc_") {
		return delivery, fmt.Errorf("notice send returned no valid message_id/chat_id; inspect send_response before retry")
	}
	delivery.URL = "https://applink.feishu.cn/client/chat/open?" + url.Values{
		"openChatId": {delivery.ChatID}, "openMessageId": {delivery.MessageID},
	}.Encode()
	var readback struct {
		Data struct {
			Messages []struct {
				MessageID string `json:"message_id"`
				ChatID    string `json:"chat_id"`
				Deleted   bool   `json:"deleted"`
				MsgType   string `json:"msg_type"`
				AppLink   string `json:"message_app_link"`
			} `json:"messages"`
		} `json:"data"`
	}
	if err := s.lark.Run(ctx, &readback, "im", "+messages-mget", "--message-ids", delivery.MessageID, "--no-reactions", "--as", "bot"); err != nil {
		return delivery, fmt.Errorf("notice %s sent, readback failed: %w", delivery.MessageID, err)
	}
	if len(readback.Data.Messages) != 1 {
		return delivery, fmt.Errorf("notice %s readback expected one message", delivery.MessageID)
	}
	m := readback.Data.Messages[0]
	if m.MessageID != delivery.MessageID || m.ChatID != delivery.ChatID || m.Deleted || m.MsgType != "interactive" {
		return delivery, fmt.Errorf("notice %s readback identity, chat or card type mismatch", delivery.MessageID)
	}
	if m.AppLink != "" {
		delivery.URL = m.AppLink
	}
	delivery.Verified = true
	extra, _ := json.Marshal(map[string]any{"message_id": delivery.MessageID, "chat_id": delivery.ChatID,
		"operation": "send", "idempotency_key": in.IdempotencyKey, "notice_type": in.Type})
	delivery.Effect = map[string]any{"kind": "feishu_message", "title": in.Type, "preview": in.Content,
		"url": delivery.URL, "target": s.principal, "extra": string(extra)}
	return delivery, nil
}

func (s *Service) audit(ctx context.Context, task domain.Task, raw json.RawMessage, delivery *Delivery, errorText string) error {
	detail := map[string]any{"at": time.Now().UTC(), "task_id": task.ID, "request": raw, "delivery": delivery, "error": errorText}
	slog.InfoContext(ctx, "principal notice", "task_id", task.ID, "error", errorText)
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode notice audit: %w", err)
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.auditPath), 0700); err != nil {
		return fmt.Errorf("create notice audit directory: %w", err)
	}
	f, err := os.OpenFile(s.auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open notice audit: %w", err)
	}
	_, writeErr := f.Write(append(encoded, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("record principal notice: %w", err)
	}
	return nil
}

// Render previews the authored content without a runtime task link.
// It has no side effects and can also be used to preview or update a card.
func Render(raw json.RawMessage) (json.RawMessage, error) {
	_, card, err := prepare(raw, nil)
	return card, err
}

func prepare(raw json.RawMessage, links *uilink.Resolver) (cardInput, json.RawMessage, error) {
	var in cardInput
	var object map[string]json.RawMessage
	invalid := func(reason string) (cardInput, json.RawMessage, error) {
		return in, nil, fmt.Errorf("%w: %s", ErrInvalidInput, reason)
	}
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return invalid("payload must be a JSON object")
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return invalid(err.Error())
	}
	if _, exists := object["type"]; !exists {
		in.Type = "Notice"
	}
	if strings.TrimSpace(in.Type) == "" || strings.TrimSpace(in.Content) == "" {
		return invalid("type and content must be non-empty strings")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" || len(in.IdempotencyKey) > 50 {
		return invalid("idempotency_key must be non-empty and at most 50 bytes")
	}
	elements := []any{markdown(in.Type), markdown(in.Content)}
	for _, link := range in.Links {
		u, err := url.Parse(link.URL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || strings.TrimSpace(link.Label) == "" {
			return invalid("links require a label and an absolute http(s) URL")
		}
		elements = append(elements, map[string]any{"tag": "button", "text": plain(link.Label), "type": "default", "size": "small",
			"behaviors": []any{map[string]any{"type": "open_url", "default_url": link.URL}}})
	}
	if strings.TrimSpace(in.Details) != "" {
		elements = append(elements, panel("详情", in.Details))
	}
	if len(in.Extra) > 0 {
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(in.Extra)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return invalid(err.Error())
		}
		if text := supplemental(value); text != "" {
			elements = append(elements, panel("补充", text))
		}
	}
	if in.TaskID != 0 && links != nil {
		detail, err := links.Task(in.TaskID)
		if err != nil {
			return in, nil, err
		}
		elements = append(elements, markdown("["+detail.Label+"]("+detail.URL+")"))
	}
	// Links render one element each; folded sections include a nested markdown.
	elementCount := len(elements)
	for _, element := range elements {
		if element.(map[string]any)["tag"] == "collapsible_panel" {
			elementCount++
		}
	}
	if elementCount > 200 {
		return invalid("rendered card exceeds 200 elements; reduce links or supplementary sections")
	}
	card, err := json.Marshal(map[string]any{
		"schema": "2.0", "config": map[string]any{"width_mode": "fill"},
		"body": map[string]any{"direction": "vertical", "padding": "12px", "vertical_spacing": "8px", "elements": elements},
	})
	if err != nil {
		return invalid(err.Error())
	}
	// Feishu's card size limit is a transport constraint, not a writing policy.
	if len(card) > 30*1024 {
		return invalid("rendered card exceeds 30 KB; shorten content or link to full material")
	}
	return in, card, nil
}

func plain(s string) map[string]any { return map[string]any{"tag": "plain_text", "content": s} }
func markdown(s string) map[string]any {
	return map[string]any{"tag": "markdown", "content": s, "text_size": "notation"}
}
func panel(title, content string) map[string]any {
	return map[string]any{"tag": "collapsible_panel", "expanded": false, "header": map[string]any{"title": plain(title)}, "elements": []any{markdown(content)}}
}

// Extra is optional, open semantic content, rendered without a field registry.
func supplemental(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		lines := make([]string, 0, len(keys))
		for _, k := range keys {
			lines = append(lines, k+"："+supplemental(v[k]))
		}
		return strings.Join(lines, "\n")
	case []any:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, "- "+supplemental(item))
		}
		return strings.Join(lines, "\n")
	default:
		return fmt.Sprint(v)
	}
}
