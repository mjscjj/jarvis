package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var (
	ErrNotFound     = errors.New("chat record not found")
	ErrInvalidInput = errors.New("invalid chat input")
	ErrConflict     = errors.New("chat state conflict")
)

type SessionView struct {
	ID                 string           `json:"id"`
	Title              string           `json:"title"`
	Agent              string           `json:"agent"`
	Model              string           `json:"model"`
	ReasoningEffort    string           `json:"reasoning_effort"`
	Sources            []Source         `json:"sources"`
	Draft              json.RawMessage  `json:"draft,omitempty"`
	Archived           bool             `json:"archived"`
	Running            bool             `json:"running"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	Messages           []MessageView    `json:"messages,omitempty"`
	PendingAttachments []AttachmentView `json:"pending_attachments,omitempty"`
}

type MessageView struct {
	ID          string           `json:"id"`
	Role        string           `json:"role"`
	Text        string           `json:"text"`
	Agent       string           `json:"agent,omitempty"`
	Model       string           `json:"model,omitempty"`
	Attachments []AttachmentView `json:"attachments,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

type AttachmentView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MIMEType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateSessionInput struct {
	Title           string          `json:"title"`
	Agent           string          `json:"agent"`
	Model           string          `json:"model"`
	ReasoningEffort string          `json:"reasoning_effort"`
	Sources         []Source        `json:"sources"`
	Draft           json.RawMessage `json:"draft"`
	FromSessionID   string          `json:"from_session_id"`
}

type UpdateSessionInput struct {
	Title           *string          `json:"title"`
	Agent           *string          `json:"agent"`
	Model           *string          `json:"model"`
	ReasoningEffort *string          `json:"reasoning_effort"`
	Sources         *[]Source        `json:"sources"`
	Draft           *json.RawMessage `json:"draft"`
	Archived        *bool            `json:"archived"`
}

type AttachmentRecord struct {
	View      AttachmentView
	LocalPath string
}

func (s *Service) requireDB() error {
	if s.db == nil {
		return fmt.Errorf("chat persistence is unavailable")
	}
	return nil
}

func newID(prefix string) (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate chat id: %w", err)
	}
	return prefix + hex.EncodeToString(data[:]), nil
}

func normalizeAgent(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "codex":
		return "codex", nil
	case "trae", "traex":
		return "trae", nil
	case "cursor":
		return "cursor", nil
	default:
		return "", fmt.Errorf("%w: unknown chat agent %q", ErrInvalidInput, value)
	}
}

func encodeJSON(value any) (datatypes.JSON, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(raw), nil
}

func decodeSources(raw datatypes.JSON) []Source {
	var result []Source
	_ = json.Unmarshal(raw, &result)
	return result
}

func (s *Service) CreateSession(ctx context.Context, input CreateSessionInput) (*SessionView, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	agent, err := normalizeAgent(input.Agent)
	if err != nil {
		return nil, err
	}
	var prior []domain.ChatMessage
	priorAttachments := map[string][]domain.ChatAttachment{}
	if from := strings.TrimSpace(input.FromSessionID); from != "" {
		if _, err := s.GetSession(ctx, from); err != nil {
			return nil, fmt.Errorf("get source chat session: %w", err)
		}
		if err := s.db.WithContext(ctx).Where("session_id = ?", from).Order("created_at ASC, rowid ASC").Find(&prior).Error; err != nil {
			return nil, fmt.Errorf("read source chat history: %w", err)
		}
		var files []domain.ChatAttachment
		if err := s.db.WithContext(ctx).Where("session_id = ? AND message_id IS NOT NULL", from).Order("created_at ASC").Find(&files).Error; err != nil {
			return nil, fmt.Errorf("read source chat attachments: %w", err)
		}
		for _, file := range files {
			priorAttachments[*file.MessageID] = append(priorAttachments[*file.MessageID], file)
		}
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return nil, fmt.Errorf("%w: chat model is required", ErrInvalidInput)
	}
	effort := strings.TrimSpace(input.ReasoningEffort)
	if effort == "" {
		return nil, fmt.Errorf("%w: chat reasoning effort is required", ErrInvalidInput)
	}
	id, err := newID("cs_")
	if err != nil {
		return nil, err
	}
	sources, err := encodeJSON(input.Sources)
	if err != nil {
		return nil, fmt.Errorf("encode chat sources: %w", err)
	}
	draft := datatypes.JSON(input.Draft)
	if len(draft) == 0 {
		draft = datatypes.JSON(`{}`)
	}
	row := domain.ChatSession{ID: id, Title: strings.TrimSpace(input.Title), Agent: agent, Model: model, ReasoningEffort: effort, Sources: sources, Draft: draft}
	if row.Title == "" {
		row.Title = "新对话"
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("create chat session: %w", err)
	}
	if len(prior) > 0 {
		for _, message := range prior {
			originalMessageID := message.ID
			copyID, idErr := newID("cm_")
			if idErr != nil {
				return nil, idErr
			}
			message.ID = copyID
			message.SessionID = row.ID
			message.Session = nil
			message.CreatedAt = time.Time{}
			if err := s.db.WithContext(ctx).Create(&message).Error; err != nil {
				return nil, fmt.Errorf("copy chat history: %w", err)
			}
			if files := priorAttachments[originalMessageID]; len(files) > 0 {
				for _, file := range files {
					if _, err := s.copyAttachment(ctx, row.ID, copyID, file); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return sessionView(row), nil
}

func (s *Service) copyAttachment(ctx context.Context, sessionID, messageID string, source domain.ChatAttachment) (*AttachmentView, error) {
	id, err := newID("ca_")
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(s.filesRoot, sessionID, "uploads", id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create copied attachment directory: %w", err)
	}
	targetPath := filepath.Join(directory, source.Name)
	input, err := os.Open(source.LocalPath)
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("open source chat attachment %s: %w", source.Name, err)
	}
	output, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = input.Close()
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("create copied chat attachment %s: %w", source.Name, err)
	}
	_, copyErr := io.Copy(output, input)
	closeOutputErr := output.Close()
	closeInputErr := input.Close()
	if copyErr != nil || closeOutputErr != nil || closeInputErr != nil {
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("copy chat attachment %s: %w", source.Name, errors.Join(copyErr, closeOutputErr, closeInputErr))
	}
	row := domain.ChatAttachment{ID: id, SessionID: sessionID, MessageID: &messageID, Name: source.Name, MIMEType: source.MIMEType, SizeBytes: source.SizeBytes, LocalPath: targetPath}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("record copied chat attachment: %w", err)
	}
	view := attachmentView(row)
	return &view, nil
}

func (s *Service) ListSessions(ctx context.Context, query string, archived bool) ([]SessionView, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	var rows []domain.ChatSession
	db := s.db.WithContext(ctx).Order("updated_at DESC")
	if archived {
		db = db.Where("archived_at IS NOT NULL")
	} else {
		db = db.Where("archived_at IS NULL")
	}
	query = strings.TrimSpace(query)
	if query != "" {
		like := "%" + query + "%"
		db = db.Where("title LIKE ? OR id IN (?)", like, s.db.Model(&domain.ChatMessage{}).Select("session_id").Where("text LIKE ?", like))
	}
	if err := db.Limit(200).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list chat sessions: %w", err)
	}
	result := make([]SessionView, 0, len(rows))
	for _, row := range rows {
		view := sessionView(row)
		view.Running = s.sessionRunning(row.ID)
		result = append(result, *view)
	}
	return result, nil
}

func sessionView(row domain.ChatSession) *SessionView {
	return &SessionView{ID: row.ID, Title: row.Title, Agent: row.Agent, Model: row.Model, ReasoningEffort: row.ReasoningEffort, Sources: decodeSources(row.Sources), Draft: json.RawMessage(row.Draft), Archived: row.ArchivedAt != nil, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s *Service) GetSession(ctx context.Context, id string) (*SessionView, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	var row domain.ChatSession
	if err := s.db.WithContext(ctx).First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get chat session: %w", err)
	}
	view := sessionView(row)
	view.Running = s.sessionRunning(row.ID)
	var messages []domain.ChatMessage
	if err := s.db.WithContext(ctx).Where("session_id = ?", row.ID).Order("created_at ASC, rowid ASC").Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	var sentAttachments []domain.ChatAttachment
	if err := s.db.WithContext(ctx).Where("session_id = ? AND message_id IS NOT NULL", row.ID).Order("created_at ASC").Find(&sentAttachments).Error; err != nil {
		return nil, fmt.Errorf("list sent chat attachments: %w", err)
	}
	attachmentsByMessage := make(map[string][]AttachmentView)
	for _, file := range sentAttachments {
		attachmentsByMessage[*file.MessageID] = append(attachmentsByMessage[*file.MessageID], attachmentView(file))
	}
	for _, message := range messages {
		mv := MessageView{ID: message.ID, Role: message.Role, Text: message.Text, Attachments: attachmentsByMessage[message.ID], CreatedAt: message.CreatedAt}
		if message.Agent != nil {
			mv.Agent = *message.Agent
		}
		if message.Model != nil {
			mv.Model = *message.Model
		}
		view.Messages = append(view.Messages, mv)
	}
	var pending []domain.ChatAttachment
	if err := s.db.WithContext(ctx).Where("session_id = ? AND message_id IS NULL", row.ID).Order("created_at ASC").Find(&pending).Error; err != nil {
		return nil, fmt.Errorf("list pending chat attachments: %w", err)
	}
	for _, file := range pending {
		view.PendingAttachments = append(view.PendingAttachments, attachmentView(file))
	}
	return view, nil
}

func (s *Service) UpdateSession(ctx context.Context, id string, input UpdateSessionInput) (*SessionView, error) {
	if (input.Agent != nil || input.Model != nil || input.ReasoningEffort != nil) && s.sessionRunning(id) {
		return nil, fmt.Errorf("%w: stop the active reply before changing its agent or model", ErrConflict)
	}
	view, err := s.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if input.Title != nil {
		if v := strings.TrimSpace(*input.Title); v == "" {
			return nil, fmt.Errorf("%w: chat title is required", ErrInvalidInput)
		} else {
			updates["title"] = v
		}
	}
	if input.Agent != nil {
		v, err := normalizeAgent(*input.Agent)
		if err != nil {
			return nil, err
		}
		updates["agent"] = v
		updates["native_thread_id"] = nil
	}
	if input.Model != nil {
		if v := strings.TrimSpace(*input.Model); v == "" {
			return nil, fmt.Errorf("%w: chat model is required", ErrInvalidInput)
		} else {
			updates["model"] = v
		}
	}
	if input.ReasoningEffort != nil {
		if v := strings.TrimSpace(*input.ReasoningEffort); v == "" {
			return nil, fmt.Errorf("%w: chat reasoning effort is required", ErrInvalidInput)
		} else {
			updates["reasoning_effort"] = v
		}
	}
	if input.Sources != nil {
		raw, err := encodeJSON(*input.Sources)
		if err != nil {
			return nil, err
		}
		updates["sources"] = raw
	}
	if input.Draft != nil {
		updates["draft"] = datatypes.JSON(*input.Draft)
	}
	if input.Archived != nil {
		if *input.Archived {
			now := time.Now()
			updates["archived_at"] = &now
		} else {
			updates["archived_at"] = nil
		}
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&domain.ChatSession{}).Where("id = ?", view.ID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("update chat session: %w", err)
		}
	}
	return s.GetSession(ctx, id)
}

func (s *Service) DeleteSession(ctx context.Context, id string) error {
	if s.sessionRunning(id) {
		return fmt.Errorf("%w: stop the active reply before deleting its session", ErrConflict)
	}
	if _, err := s.GetSession(ctx, id); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&domain.ChatSession{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete chat session: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(s.filesRoot, id)); err != nil {
		return fmt.Errorf("delete chat session files: %w", err)
	}
	return nil
}

func (s *Service) SaveUpload(ctx context.Context, sessionID, name, mime string, size int64, copyFile func(string) error) (*AttachmentView, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, fmt.Errorf("%w: attachment is empty", ErrInvalidInput)
	}
	if size > 25<<20 {
		return nil, fmt.Errorf("%w: attachment exceeds 25 MiB", ErrInvalidInput)
	}
	id, err := newID("ca_")
	if err != nil {
		return nil, err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		return nil, fmt.Errorf("%w: attachment name is required", ErrInvalidInput)
	}
	directory := filepath.Join(s.filesRoot, sessionID, "uploads", id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create attachment directory: %w", err)
	}
	path := filepath.Join(directory, name)
	if err := copyFile(path); err != nil {
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("save attachment: %w", err)
	}
	row := domain.ChatAttachment{ID: id, SessionID: sessionID, Name: name, MIMEType: strings.TrimSpace(mime), SizeBytes: size, LocalPath: path}
	if row.MIMEType == "" {
		row.MIMEType = "application/octet-stream"
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("record attachment: %w", err)
	}
	view := attachmentView(row)
	return &view, nil
}

func (s *Service) DeletePendingAttachment(ctx context.Context, sessionID, id string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	var row domain.ChatAttachment
	if err := s.db.WithContext(ctx).Where("session_id = ? AND id = ? AND message_id IS NULL", strings.TrimSpace(sessionID), strings.TrimSpace(id)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("get pending attachment: %w", err)
	}
	directory := filepath.Join(s.filesRoot, row.SessionID, "uploads", row.ID)
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("delete pending attachment file: %w", err)
	}
	if err := s.db.WithContext(ctx).Delete(&row).Error; err != nil {
		return fmt.Errorf("delete pending attachment: %w", err)
	}
	return nil
}

func (s *Service) GetAttachment(ctx context.Context, id string) (*AttachmentRecord, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	var row domain.ChatAttachment
	if err := s.db.WithContext(ctx).First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &AttachmentRecord{View: attachmentView(row), LocalPath: row.LocalPath}, nil
}

func (s *Service) attachmentRecords(ctx context.Context, sessionID string, ids []string) ([]AttachmentRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []domain.ChatAttachment
	if err := s.db.WithContext(ctx).Where("session_id = ? AND message_id IS NULL AND id IN ?", sessionID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := map[string]domain.ChatAttachment{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	result := make([]AttachmentRecord, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: attachment %s", ErrNotFound, id)
		}
		result = append(result, AttachmentRecord{View: attachmentView(row), LocalPath: row.LocalPath})
	}
	return result, nil
}

func attachmentView(row domain.ChatAttachment) AttachmentView {
	return AttachmentView{ID: row.ID, Name: row.Name, MIMEType: row.MIMEType, SizeBytes: row.SizeBytes, CreatedAt: row.CreatedAt}
}
