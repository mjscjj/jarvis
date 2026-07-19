package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type memoryAdder interface {
	Add(context.Context, AddInput) error
}

type WorkerOptions struct {
	BatchLimit        int
	WindowGap         time.Duration
	WindowMaxMessages int
	Location          *time.Location
}

type Stats struct {
	Loaded            int
	Processed         int
	MemorizedMessages int
	SkippedMessages   int
	Windows           int
}

// Worker turns pending plaintext messages into bounded conversation windows.
// It marks a window only after mem0 accepts it; failed windows remain pending.
type Worker struct {
	store  messageStore
	memory memoryAdder
	opts   WorkerOptions
	now    func() time.Time
}

func NewWorker(store messageStore, memory memoryAdder, opts WorkerOptions) (*Worker, error) {
	if store == nil {
		return nil, fmt.Errorf("memory worker store is nil")
	}
	if memory == nil {
		return nil, fmt.Errorf("memory worker client is nil")
	}
	if opts.BatchLimit <= 0 {
		return nil, fmt.Errorf("memory worker batch limit must be positive")
	}
	if opts.WindowGap <= 0 {
		return nil, fmt.Errorf("memory worker window gap must be positive")
	}
	if opts.WindowMaxMessages <= 0 {
		return nil, fmt.Errorf("memory worker window max messages must be positive")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("memory worker location is nil")
	}
	return &Worker{store: store, memory: memory, opts: opts, now: time.Now}, nil
}

func (w *Worker) MemorizeOnce(ctx context.Context) (Stats, error) {
	messages, err := w.store.ListPending(ctx, w.opts.BatchLimit)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Loaded: len(messages)}
	for _, group := range groupByChat(messages) {
		for _, window := range splitWindows(group, w.opts.WindowGap, w.opts.WindowMaxMessages) {
			meaningful := filterMeaningful(window)
			if len(meaningful) > 0 {
				input := AddInput{
					Transcript: renderTranscript(meaningful, w.opts.Location),
					Metadata:   windowMetadata(meaningful),
					Infer:      true,
				}
				if err := w.memory.Add(ctx, input); err != nil {
					return stats, fmt.Errorf("memorize chat_id=%s window_id=%s: %w", group[0].ChatID, input.Metadata["window_id"], err)
				}
				stats.Windows++
				stats.MemorizedMessages += len(meaningful)
			}

			ids := databaseIDs(window)
			if err := w.store.MarkProcessed(ctx, ids, w.now()); err != nil {
				return stats, fmt.Errorf("finish memory window chat_id=%s: %w", group[0].ChatID, err)
			}
			stats.Processed += len(window)
			stats.SkippedMessages += len(window) - len(meaningful)
		}
	}
	return stats, nil
}

func groupByChat(messages []PendingMessage) [][]PendingMessage {
	groups := make([][]PendingMessage, 0)
	for _, message := range messages {
		if len(groups) == 0 || groups[len(groups)-1][0].ChatID != message.ChatID {
			groups = append(groups, []PendingMessage{message})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], message)
	}
	return groups
}

func splitWindows(messages []PendingMessage, gap time.Duration, maxMessages int) [][]PendingMessage {
	if len(messages) == 0 {
		return nil
	}
	windows := make([][]PendingMessage, 0, 1)
	start := 0
	for i := 1; i < len(messages); i++ {
		timeGap := time.Duration(messages[i].CreateTime-messages[i-1].CreateTime) * time.Millisecond
		if i-start >= maxMessages || timeGap > gap {
			windows = append(windows, messages[start:i])
			start = i
		}
	}
	return append(windows, messages[start:])
}

func filterMeaningful(messages []PendingMessage) []PendingMessage {
	result := make([]PendingMessage, 0, len(messages))
	for _, message := range messages {
		if isMeaningful(message) {
			result = append(result, message)
		}
	}
	return result
}

func isMeaningful(message PendingMessage) bool {
	if !message.RenderOK {
		return false
	}
	senderType := strings.ToLower(strings.TrimSpace(message.SenderType))
	if senderType == "bot" || senderType == "app" {
		return false
	}
	content := strings.TrimSpace(message.Content)
	if content == "" || isResourcePlaceholder(content) {
		return false
	}
	for _, r := range content {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

func isResourcePlaceholder(content string) bool {
	if content == "[图片]" || content == "[表情]" {
		return true
	}
	return strings.HasPrefix(content, "[文件:") && strings.HasSuffix(content, "]")
}

func renderTranscript(messages []PendingMessage, location *time.Location) string {
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		content = strings.ReplaceAll(content, "\r\n", "\n")
		content = strings.ReplaceAll(content, "\n", "\n    ")
		at := time.UnixMilli(message.CreateTime).In(location).Format("2006-01-02 15:04")
		lines = append(lines, fmt.Sprintf("%s %s: %s", at, message.SenderName, content))
	}
	return strings.Join(lines, "\n")
}

func windowMetadata(messages []PendingMessage) map[string]any {
	messageIDs := make([]string, 0, len(messages))
	senderSet := make(map[string]struct{})
	for _, message := range messages {
		messageIDs = append(messageIDs, message.MessageID)
		if message.SenderName != "" {
			senderSet[message.SenderName] = struct{}{}
		}
	}
	senders := make([]string, 0, len(senderSet))
	for sender := range senderSet {
		senders = append(senders, sender)
	}
	sort.Strings(senders)
	meta := map[string]any{
		"source":      "message",
		"window_id":   windowID(messages[0].ChatID, messageIDs),
		"chat_id":     messages[0].ChatID,
		"chat_name":   messages[0].ChatName,
		"message_ids": messageIDs,
		"start_ms":    messages[0].CreateTime,
		"end_ms":      messages[len(messages)-1].CreateTime,
		"senders":     senders,
	}
	if messages[0].ProjectID != nil {
		meta["project_id"] = *messages[0].ProjectID
	}
	return meta
}

func windowID(chatID string, messageIDs []string) string {
	hash := sha256.Sum256([]byte(chatID + "\x00" + strings.Join(messageIDs, "\x00")))
	return hex.EncodeToString(hash[:])
}

func databaseIDs(messages []PendingMessage) []uint64 {
	ids := make([]uint64, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	return ids
}
