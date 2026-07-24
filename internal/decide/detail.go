package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/extract"

	"gorm.io/gorm"
)

type ConfirmationDetail struct {
	Todo            *extract.TodoView     `json:"todo"`
	SourceMessages  []ConfirmationMessage `json:"source_messages"`
	Assigner        *ConfirmationAssigner `json:"assigner"`
	Events          []ConfirmationEvent   `json:"events"`
	Audits          []DecisionAuditView   `json:"audits"`
	Plan            json.RawMessage       `json:"plan"`
	DecisionPayload json.RawMessage       `json:"decision_payload"`
}

type ConfirmationMessage struct {
	MessageID    string  `json:"message_id"`
	ChatID       string  `json:"chat_id"`
	SenderOpenID string  `json:"sender_open_id"`
	SenderName   string  `json:"sender_name"`
	MessageType  string  `json:"message_type"`
	Content      string  `json:"content"`
	ReplyTo      *string `json:"reply_to"`
	RootID       *string `json:"root_id"`
	ThreadID     *string `json:"thread_id"`
	CreateTime   int64   `json:"create_time"`
}

type ConfirmationAssigner struct {
	OpenID         string   `json:"open_id"`
	Name           *string  `json:"name"`
	Role           *string  `json:"role"`
	Title          *string  `json:"title"`
	Department     *string  `json:"department"`
	Relation       *string  `json:"relation"`
	PriorityWeight *float64 `json:"priority_weight"`
}

type ConfirmationEvent struct {
	ID         uint64          `json:"id"`
	FromStatus *string         `json:"from_status"`
	ToStatus   string          `json:"to_status"`
	Actor      string          `json:"actor"`
	Detail     json.RawMessage `json:"detail"`
	CreatedAt  time.Time       `json:"created_at"`
}

type DecisionAuditView struct {
	ID                     uint64          `json:"id"`
	TS                     time.Time       `json:"ts"`
	Route                  string          `json:"route"`
	RouteReason            string          `json:"route_reason"`
	Confidence             *float64        `json:"confidence"`
	Risk                   *float64        `json:"risk"`
	MatchedRules           json.RawMessage `json:"matched_rules"`
	DecisionEngine         string          `json:"decision_engine"`
	CodexSessionID         *string         `json:"codex_session_id"`
	ThresholdConfigVersion string          `json:"threshold_config_version"`
	FinalStatus            string          `json:"final_status"`
}

type ConfirmationDetailReader interface {
	GetConfirmation(context.Context, uint64) (*ConfirmationDetail, error)
}

type ConfirmationDetailStore struct {
	db    *gorm.DB
	todos extract.TodoReader
}

func NewConfirmationDetailStore(db *gorm.DB, todos extract.TodoReader) (*ConfirmationDetailStore, error) {
	if db == nil {
		return nil, fmt.Errorf("confirmation detail db is nil")
	}
	if todos == nil {
		return nil, fmt.Errorf("confirmation detail Todo reader is nil")
	}
	return &ConfirmationDetailStore{db: db, todos: todos}, nil
}

func (s *ConfirmationDetailStore) GetConfirmation(ctx context.Context, todoID uint64) (*ConfirmationDetail, error) {
	if todoID == 0 {
		return nil, fmt.Errorf("%w: todo_id must be positive", ErrInvalidInput)
	}
	todo, err := s.todos.GetTodo(ctx, todoID)
	if errors.Is(err, extract.ErrTodoNotFound) {
		return nil, fmt.Errorf("%w: todo_id=%d", ErrTodoNotFound, todoID)
	}
	if err != nil {
		return nil, fmt.Errorf("load confirmation detail Todo id=%d: %w", todoID, err)
	}
	if todo.Status != RouteNeedInfo && todo.Status != RouteNeedDecision {
		return nil, transitionError(todoID, todo.Status, "read_confirmation")
	}

	messages, err := s.loadSourceMessages(ctx, todo)
	if err != nil {
		return nil, err
	}
	assigner, err := s.loadAssigner(ctx, todo.AssignerOpenID)
	if err != nil {
		return nil, err
	}
	events, plan, decisionPayload, err := s.loadEvents(ctx, todoID)
	if err != nil {
		return nil, err
	}
	audits, err := s.loadAudits(ctx, todoID)
	if err != nil {
		return nil, err
	}
	return &ConfirmationDetail{
		Todo: todo, SourceMessages: messages, Assigner: assigner,
		Events: events, Audits: audits, Plan: plan,
		DecisionPayload: decisionPayload,
	}, nil
}

func (s *ConfirmationDetailStore) loadSourceMessages(ctx context.Context, todo *extract.TodoView) ([]ConfirmationMessage, error) {
	var sourceIDs []string
	if err := json.Unmarshal(todo.SourceMessageIDs, &sourceIDs); err != nil {
		return nil, fmt.Errorf("decode confirmation source message IDs todo_id=%d: %w", todo.ID, err)
	}
	if len(sourceIDs) == 0 {
		return nil, fmt.Errorf("confirmation source message IDs are empty todo_id=%d", todo.ID)
	}
	var rows []domain.Message
	if err := s.db.WithContext(ctx).Where("message_id IN ?", sourceIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load confirmation source messages todo_id=%d: %w", todo.ID, err)
	}
	byID := make(map[string]domain.Message, len(rows))
	for _, row := range rows {
		byID[row.MessageID] = row
	}
	result := make([]ConfirmationMessage, 0, len(sourceIDs))
	for _, messageID := range sourceIDs {
		row, ok := byID[messageID]
		if !ok {
			return nil, fmt.Errorf("confirmation source message %q not found todo_id=%d", messageID, todo.ID)
		}
		result = append(result, ConfirmationMessage{
			MessageID: row.MessageID, ChatID: row.ChatID, SenderOpenID: row.SenderOpenID,
			SenderName: row.SenderName, MessageType: row.MessageType, Content: row.Content,
			ReplyTo: copyString(row.ReplyTo), RootID: copyString(row.RootID), ThreadID: copyString(row.ThreadID), CreateTime: row.CreateTime,
		})
	}
	return result, nil
}

func (s *ConfirmationDetailStore) loadAssigner(ctx context.Context, openID *string) (*ConfirmationAssigner, error) {
	if openID == nil || strings.TrimSpace(*openID) == "" {
		return nil, nil
	}
	result := &ConfirmationAssigner{OpenID: *openID}
	var person domain.Person
	query := s.db.WithContext(ctx).Where("open_id = ?", *openID).Limit(1).Find(&person)
	if query.Error != nil {
		return nil, fmt.Errorf("load confirmation assigner open_id=%s: %w", *openID, query.Error)
	}
	if query.RowsAffected == 0 {
		return result, nil
	}
	result.Name = copyString(&person.Name)
	result.Role = copyString(&person.Role)
	result.Title = copyString(person.Title)
	result.Department = copyString(person.Department)
	result.Relation = copyString(person.Relation)
	result.PriorityWeight = float64Pointer(person.PriorityWeight)
	return result, nil
}

func (s *ConfirmationDetailStore) loadEvents(ctx context.Context, todoID uint64) ([]ConfirmationEvent, json.RawMessage, json.RawMessage, error) {
	var rows []domain.TodoEvent
	if err := s.db.WithContext(ctx).Where("todo_id = ?", todoID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, nil, nil, fmt.Errorf("load confirmation events todo_id=%d: %w", todoID, err)
	}
	result := make([]ConfirmationEvent, len(rows))
	var plan json.RawMessage
	var decisionPayload json.RawMessage
	for index, row := range rows {
		result[index] = ConfirmationEvent{
			ID: row.ID, FromStatus: copyString(row.FromStatus), ToStatus: row.ToStatus,
			Actor: row.Actor, Detail: rawJSON(row.Detail), CreatedAt: row.CreatedAt,
		}
		if len(row.Detail) != 0 {
			var detail struct {
				EventType string          `json:"event_type"`
				Plan      json.RawMessage `json:"plan"`
				Payload   json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(row.Detail, &detail); err != nil {
				return nil, nil, nil, fmt.Errorf("decode confirmation event detail event_id=%d: %w", row.ID, err)
			}
			if detail.EventType == "evaluated" {
				// Latest evaluated event wins as a whole. A null plan explicitly
				// clears an older actionable plan after re-evaluation.
				plan = nil
				decisionPayload = nil
				if len(detail.Plan) != 0 && string(detail.Plan) != "null" {
					canonical, err := canonicalJSONValue(detail.Plan, "confirmation plan", false)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("invalid plan event_id=%d: %w", row.ID, err)
					}
					plan = canonical
				}
				if len(detail.Payload) != 0 && string(detail.Payload) != "null" {
					canonical, err := canonicalJSONValue(detail.Payload, "confirmation decision payload", false)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("invalid decision payload event_id=%d: %w", row.ID, err)
					}
					decisionPayload = canonical
				}
			}
		}
	}
	return result, plan, decisionPayload, nil
}

func (s *ConfirmationDetailStore) loadAudits(ctx context.Context, todoID uint64) ([]DecisionAuditView, error) {
	var rows []domain.DecisionAudit
	if err := s.db.WithContext(ctx).Where("todo_id = ?", todoID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load confirmation audits todo_id=%d: %w", todoID, err)
	}
	result := make([]DecisionAuditView, len(rows))
	for index, row := range rows {
		result[index] = DecisionAuditView{
			ID: row.ID, TS: row.TS, Route: row.Route, RouteReason: row.RouteReason,
			Confidence: float64Copy(row.ConfidenceEff), Risk: float64Copy(row.RiskEff),
			MatchedRules:   rawJSON(row.MatchedRules),
			DecisionEngine: row.DecisionEngine, CodexSessionID: copyString(row.CodexSessionID),
			ThresholdConfigVersion: row.ThresholdConfigVersion, FinalStatus: row.FinalStatus,
		}
	}
	return result, nil
}

func float64Copy(value *float64) *float64 {
	if value == nil {
		return nil
	}
	return float64Pointer(*value)
}
