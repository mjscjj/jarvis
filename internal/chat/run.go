package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
)

type SendInput struct {
	Message       string   `json:"message"`
	AttachmentIDs []string `json:"attachment_ids"`
	Sources       []Source `json:"sources"`
}

func (s *Service) StreamSession(ctx context.Context, sessionID string, input SendInput, emit func(Event) error) error {
	workCtx := context.WithoutCancel(ctx)
	session, err := s.GetSession(workCtx, sessionID)
	if err != nil {
		return err
	}
	message := strings.TrimSpace(input.Message)
	if message == "" && len(input.AttachmentIDs) == 0 {
		return fmt.Errorf("%w: chat message or attachment is required", ErrInvalidInput)
	}
	attachments, err := s.attachmentRecords(workCtx, sessionID, input.AttachmentIDs)
	if err != nil {
		return err
	}
	if len(attachments) > 10 {
		return fmt.Errorf("%w: each chat message supports at most 10 attachments", ErrInvalidInput)
	}
	var totalSize int64
	for _, attachment := range attachments {
		totalSize += attachment.View.SizeBytes
		if session.Agent == "cursor" && strings.HasPrefix(attachment.View.MIMEType, "image/") {
			return fmt.Errorf("%w: Cursor image input is unavailable for the current adapter; choose Codex or TRAE", ErrInvalidInput)
		}
	}
	if totalSize > 100<<20 {
		return fmt.Errorf("%w: chat attachments exceed 100 MiB in total", ErrInvalidInput)
	}

	s.activeMu.Lock()
	if _, exists := s.active[sessionID]; exists {
		s.activeMu.Unlock()
		return fmt.Errorf("%w: this chat session is already generating a reply", ErrConflict)
	}
	runCtx, cancel := context.WithCancel(workCtx)
	s.active[sessionID] = cancel
	s.activeMu.Unlock()
	defer func() { cancel(); s.activeMu.Lock(); delete(s.active, sessionID); s.activeMu.Unlock() }()

	userID, err := newID("cm_")
	if err != nil {
		return err
	}
	attachmentViews := make([]AttachmentView, 0, len(attachments))
	paths := make([]string, 0, len(attachments))
	imagePaths := make([]string, 0, len(attachments))
	for _, a := range attachments {
		attachmentViews = append(attachmentViews, a.View)
		paths = append(paths, a.LocalPath)
		if strings.HasPrefix(a.View.MIMEType, "image/") {
			imagePaths = append(imagePaths, a.LocalPath)
		}
	}
	meta, err := encodeJSON(map[string]any{"sources": input.Sources})
	if err != nil {
		return err
	}
	user := domain.ChatMessage{ID: userID, SessionID: sessionID, Role: "user", Text: message, Meta: meta}
	if err := s.db.WithContext(workCtx).Create(&user).Error; err != nil {
		return fmt.Errorf("save user chat message: %w", err)
	}
	if len(input.AttachmentIDs) > 0 {
		if err := s.db.WithContext(workCtx).Model(&domain.ChatAttachment{}).Where("id IN ?", input.AttachmentIDs).Update("message_id", userID).Error; err != nil {
			return fmt.Errorf("attach files to message: %w", err)
		}
	}
	updates := map[string]any{"updated_at": time.Now(), "sources": metaSources(input.Sources), "draft": datatypes.JSON([]byte(`{}`))}
	if session.Title == "新对话" {
		updates["title"] = deriveTitle(message, attachmentViews)
	}
	if err := s.db.WithContext(workCtx).Model(&domain.ChatSession{}).Where("id = ?", sessionID).Updates(updates).Error; err != nil {
		return fmt.Errorf("update chat session before run: %w", err)
	}
	// Once the user input is durable, a broken browser SSE connection is only
	// a lost observer. Keep the agent turn running so a refresh can recover it
	// from the persisted session state.
	streamOpen := true
	if err := emit(Event{Kind: EventAccepted}); err != nil {
		streamOpen = false
	}

	var row domain.ChatSession
	if err := s.db.WithContext(workCtx).First(&row, "id = ?", sessionID).Error; err != nil {
		return err
	}
	var response strings.Builder
	visibleHistory := ""
	// Keep durable visible history ready even while a native thread exists.
	// It is only injected if that thread disappears and a fresh one is needed.
	{
		var prior []domain.ChatMessage
		if err := s.db.WithContext(workCtx).Where("session_id = ? AND id <> ?", sessionID, userID).Order("created_at ASC, rowid ASC").Find(&prior).Error; err != nil {
			return fmt.Errorf("read carried chat history: %w", err)
		}
		var priorFiles []domain.ChatAttachment
		if err := s.db.WithContext(workCtx).Where("session_id = ? AND message_id IS NOT NULL AND message_id <> ?", sessionID, userID).Order("created_at ASC").Find(&priorFiles).Error; err != nil {
			return fmt.Errorf("read carried chat attachments: %w", err)
		}
		filesByMessage := map[string][]domain.ChatAttachment{}
		for _, file := range priorFiles {
			filesByMessage[*file.MessageID] = append(filesByMessage[*file.MessageID], file)
			if row.NativeThreadID == nil {
				paths = append(paths, file.LocalPath)
				if strings.HasPrefix(file.MIMEType, "image/") {
					imagePaths = append(imagePaths, file.LocalPath)
				}
			}
		}
		var history strings.Builder
		for _, item := range prior {
			history.WriteString(item.Role)
			history.WriteString(": ")
			history.WriteString(item.Text)
			for _, file := range filesByMessage[item.ID] {
				history.WriteString("\n附件：")
				history.WriteString(file.Name)
				history.WriteString("（")
				history.WriteString(file.LocalPath)
				history.WriteString("）")
			}
			history.WriteString("\n\n")
		}
		visibleHistory = history.String()
	}
	agentMessage := message
	if agentMessage == "" {
		agentMessage = "请阅读并处理我附上的文件。"
	}
	req := Request{SessionID: sessionID, Message: agentMessage, ThreadID: valueOrEmpty(row.NativeThreadID), Agent: row.Agent, Model: row.Model, ReasoningEffort: row.ReasoningEffort, AttachmentPaths: paths, ImagePaths: imagePaths, Sources: input.Sources, VisibleHistory: visibleHistory}
	assistantID, err := newID("cm_")
	if err != nil {
		return err
	}
	streamingMeta, err := encodeJSON(map[string]any{"status": "streaming"})
	if err != nil {
		return err
	}
	agent, model := row.Agent, row.Model
	assistant := domain.ChatMessage{ID: assistantID, SessionID: sessionID, Role: "assistant", Agent: &agent, Model: &model, Meta: streamingMeta}
	persist := s.db.WithContext(workCtx)
	if err := persist.Create(&assistant).Error; err != nil {
		return fmt.Errorf("save assistant chat message: %w", err)
	}
	runErr := s.Stream(runCtx, req, func(event Event) error {
		if event.Kind == EventThread && strings.TrimSpace(event.ThreadID) != "" {
			thread := strings.TrimSpace(event.ThreadID)
			if err := s.db.WithContext(workCtx).Model(&domain.ChatSession{}).Where("id = ?", sessionID).Update("native_thread_id", thread).Error; err != nil {
				return fmt.Errorf("save native chat thread: %w", err)
			}
		}
		if event.Kind == EventDelta {
			response.WriteString(event.Text)
			// Persist before delivery so refresh/restart keeps every received reply chunk.
			if err := persist.Model(&assistant).Update("text", response.String()).Error; err != nil {
				return fmt.Errorf("save assistant chat text: %w", err)
			}
		}
		if !streamOpen {
			return nil
		}
		if err := emit(event); err != nil {
			streamOpen = false
		}
		return nil
	})
	status := "completed"
	if runErr != nil {
		status = "interrupted"
	}
	resultMeta := map[string]any{"status": status}
	if runErr != nil {
		resultMeta["error"] = runErr.Error()
	}
	assistantMeta, err := encodeJSON(resultMeta)
	if err != nil {
		return err
	}
	if err := persist.Model(&assistant).Update("meta", assistantMeta).Error; err != nil {
		return fmt.Errorf("save assistant chat status: %w", err)
	}

	if runErr != nil {
		return runErr
	}
	return nil
}

func metaSources(sources []Source) datatypes.JSON {
	raw, _ := json.Marshal(sources)
	return datatypes.JSON(raw)
}

func deriveTitle(message string, attachments []AttachmentView) string {
	text := strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if text == "" && len(attachments) > 0 {
		text = attachments[0].Name
	}
	runes := []rune(text)
	if len(runes) > 32 {
		runes = runes[:32]
	}
	if len(runes) == 0 {
		return "新对话"
	}
	return string(runes)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Service) CancelSession(sessionID string) bool {
	s.activeMu.Lock()
	cancel, ok := s.active[strings.TrimSpace(sessionID)]
	s.activeMu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (s *Service) sessionRunning(sessionID string) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	_, ok := s.active[strings.TrimSpace(sessionID)]
	return ok
}
