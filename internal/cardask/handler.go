// Package cardask maps authenticated Feishu question-card callbacks from CC
// Connect back into the Codex session that asked. The card carries the exact
// durable Task version it was created for, so stale or repeated clicks fail
// through the existing optimistic lock without polling or extra state.
//
// The answer is handed to the model verbatim as JSON. Jarvis reads none of it:
// which button means yes, what a blank note implies, whether a selection
// changes the plan — all of that is the agent's judgment, in the session that
// wrote the question in the first place.
package cardask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"jarvis/internal/execute"
)

// Resumer hands a principal's answer back to the parked Codex session.
// *execute.AgentExecutor satisfies this via KickResumeAfterHuman.
type Resumer interface {
	KickResumeAfterHuman(ctx context.Context, taskID uint64, expectedVersion int32, response, channel string) (*execute.ExecuteResult, error)
}

// CardActionEvent is the strict relay payload needed to land one Feishu click.
// CC Connect owns the Feishu callback connection.
type CardActionEvent struct {
	EventID     string
	OperatorID  string
	MessageID   string
	ChatID      string
	ActionTag   string
	ActionValue string
	FormValue   map[string]any
}

type cardAction struct {
	Action  string `json:"action"`
	TaskID  uint64 `json:"task_id"`
	Version int32  `json:"version"`
	Clicked string `json:"clicked"`
}

// QuestionSnapshots reads the parked question a card was rendered from.
// *execute.AgentExecutor satisfies this via QuestionSnapshot.
type QuestionSnapshots interface {
	QuestionSnapshot(ctx context.Context, taskID uint64) (execute.QuestionNotification, error)
}

// CardRenderer re-renders a question card in its answered state. *Notifier
// satisfies this via AnsweredCard.
type CardRenderer interface {
	AnsweredCard(notice execute.QuestionNotification, outcome string) (json.RawMessage, error)
}

type Handler struct {
	resumer       Resumer
	snapshots     QuestionSnapshots
	cards         CardRenderer
	principalOpen string
	logger        *log.Logger
}

// NewRelayHandler builds the card processor behind the authenticated localhost
// relay. Jarvis never opens a Feishu event connection itself.
func NewRelayHandler(resumer Resumer, snapshots QuestionSnapshots, cards CardRenderer, principalOpenID string, logger *log.Logger) (*Handler, error) {
	if resumer == nil {
		return nil, fmt.Errorf("question card resumer is nil")
	}
	if snapshots == nil {
		return nil, fmt.Errorf("question card snapshot reader is nil")
	}
	if cards == nil {
		return nil, fmt.Errorf("question card renderer is nil")
	}
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return nil, fmt.Errorf("question card principal open_id is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("question card logger is nil")
	}
	return &Handler{resumer: resumer, snapshots: snapshots, cards: cards, principalOpen: principalOpenID, logger: logger}, nil
}

// ProcessCardAction immediately lands one version-bound callback and returns
// the answered card in full. The snapshot is taken before the answer lands,
// because resuming replaces the question it renders from.
func (h *Handler) ProcessCardAction(ctx context.Context, event CardActionEvent) (json.RawMessage, error) {
	action, err := authorizeCardAction(event, h.principalOpen)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", execute.ErrInvalidInput, err)
	}
	answer, err := encodeAnswer(action.Clicked, event.FormValue)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", execute.ErrInvalidInput, err)
	}
	notice, err := h.snapshots.QuestionSnapshot(ctx, action.TaskID)
	if err != nil {
		return nil, fmt.Errorf("read question snapshot task_id=%d: %w", action.TaskID, err)
	}
	if _, err := h.resumer.KickResumeAfterHuman(ctx, action.TaskID, action.Version, answer, "feishu_card"); err != nil {
		if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) {
			h.logger.Printf("job=card-ask status=info task_id=%d version=%d skipped=already-answered: %v", action.TaskID, action.Version, err)
			return h.answeredCard(notice, "这条已经回答过了，请去后台查看。")
		}
		return nil, fmt.Errorf("resume after question task_id=%d version=%d: %w", action.TaskID, action.Version, err)
	}
	return h.answeredCard(notice, "已收到你的回答，正在继续。\n\n"+answerDigest(notice, action.Clicked, event.FormValue))
}

// answeredCard renders the whole card Jarvis wants shown after the answer. CC
// Connect replaces the original message with it, so the question stays visible
// as audit text without Feishu having to return the original card.
func (h *Handler) answeredCard(notice execute.QuestionNotification, outcome string) (json.RawMessage, error) {
	card, err := h.cards.AnsweredCard(notice, outcome)
	if err != nil {
		return nil, fmt.Errorf("render answered question card task_id=%d: %w", notice.TaskID, err)
	}
	return card, nil
}

func authorizeCardAction(event CardActionEvent, principalOpenID string) (cardAction, error) {
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return cardAction{}, fmt.Errorf("card action principal open_id is not configured")
	}
	if strings.TrimSpace(event.OperatorID) != principalOpenID {
		return cardAction{}, fmt.Errorf("card action operator_id=%q is not the principal", event.OperatorID)
	}
	if strings.TrimSpace(event.ActionTag) != "button" {
		return cardAction{}, fmt.Errorf("card action tag=%q is not button", event.ActionTag)
	}
	raw := strings.TrimSpace(event.ActionValue)
	if raw == "" {
		return cardAction{}, fmt.Errorf("card action value is empty")
	}
	var action cardAction
	if err := json.Unmarshal([]byte(raw), &action); err != nil {
		return cardAction{}, fmt.Errorf("decode card action value %q: %w", raw, err)
	}
	if strings.TrimSpace(action.Action) != callbackAction {
		return cardAction{}, fmt.Errorf("card action %q is not %s", action.Action, callbackAction)
	}
	action.Clicked = strings.TrimSpace(action.Clicked)
	if action.Clicked == "" {
		return cardAction{}, fmt.Errorf("card action carries no clicked button")
	}
	if action.TaskID == 0 {
		return cardAction{}, fmt.Errorf("card action task_id must be positive")
	}
	if action.Version <= 0 {
		return cardAction{}, fmt.Errorf("card action version must be positive")
	}
	return action, nil
}

// encodeAnswer packages the click into the JSON the resumed session reads:
// every form input keyed by the name the model chose, plus which button was
// pressed. Feishu also echoes the submit button itself into form_value; it is
// dropped so "clicked" stays the single answer to "which one did they press".
func encodeAnswer(clicked string, form map[string]any) (string, error) {
	answer := map[string]any{"clicked": clicked}
	for name, value := range form {
		if name == clicked {
			continue
		}
		answer[name] = value
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		return "", fmt.Errorf("encode card answer: %w", err)
	}
	return string(encoded), nil
}

// answerDigest echoes the choice back on the answered card so the principal can
// still read what they submitted after the controls are gone.
func answerDigest(notice execute.QuestionNotification, clicked string, form map[string]any) string {
	labels := make(map[string]string, len(notice.Question.Fields))
	for _, field := range notice.Question.Fields {
		labels[field.Name] = field.Label
	}
	lines := []string{"**你的选择**：" + label(labels, clicked)}
	names := make([]string, 0, len(form))
	for name := range form {
		if name != clicked {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if value := strings.TrimSpace(fmt.Sprintf("%v", form[name])); value != "" && value != "<nil>" && value != "[]" {
			lines = append(lines, label(labels, name)+"："+value)
		}
	}
	return strings.Join(lines, "\n")
}

func label(labels map[string]string, name string) string {
	if text := strings.TrimSpace(labels[name]); text != "" {
		return text
	}
	return name
}
