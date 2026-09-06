package chat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrHistoryNotFound = errors.New("chat history not found")
	ErrInvalidThreadID = errors.New("chat thread_id is invalid")
)

var threadIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

const (
	userHeadingPrefix      = "## 用户 · "
	assistantHeadingPrefix = "## Jarvis · "
)

type HistoryMessage struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type History struct {
	ThreadID string           `json:"thread_id"`
	Messages []HistoryMessage `json:"messages"`
}

type ThreadSummary struct {
	ThreadID  string    `json:"thread_id"`
	Title     string    `json:"title"`
	Preview   string    `json:"preview"`
	MessageAt time.Time `json:"message_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HistoryWarning struct {
	ThreadID string `json:"thread_id,omitempty"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message"`
}

type ThreadList struct {
	Threads  []ThreadSummary  `json:"threads"`
	Warnings []HistoryWarning `json:"warnings,omitempty"`
}

// HistoryStore keeps one human-readable Markdown transcript per Agent CLI thread.
// The browser owns the active thread ID; this store only persists and reloads
// the visible conversation, while the selected Agent CLI remains the actual agent memory.
type HistoryStore struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

func NewHistoryStore(dir string) (*HistoryStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("chat history directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create chat history directory %q: %w", dir, err)
	}
	return &HistoryStore{dir: dir, now: time.Now}, nil
}

func (s *HistoryStore) AppendTurn(threadID, userText, assistantText string) error {
	path, err := s.path(threadID)
	if err != nil {
		return err
	}
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if userText == "" {
		return fmt.Errorf("chat history user message is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open chat history %q: %w", path, err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set chat history permissions %q: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat chat history %q: %w", path, err)
	}
	if info.Size() == 0 {
		if _, err := fmt.Fprintf(file, "# Jarvis 对话\n\n- Agent 会话：`%s`\n", threadID); err != nil {
			return fmt.Errorf("write chat history header %q: %w", path, err)
		}
	}
	at := s.now().UTC().Format(time.RFC3339)
	if _, err := fmt.Fprintf(file, "\n%s%s\n\n%s\n", userHeadingPrefix, at, escapeHistoryText(userText)); err != nil {
		return fmt.Errorf("append chat user message %q: %w", path, err)
	}
	if assistantText != "" {
		if _, err := fmt.Fprintf(file, "\n%s%s\n\n%s\n", assistantHeadingPrefix, at, escapeHistoryText(assistantText)); err != nil {
			return fmt.Errorf("append chat assistant message %q: %w", path, err)
		}
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync chat history %q: %w", path, err)
	}
	return nil
}

func (s *HistoryStore) Read(threadID string) (History, error) {
	path, err := s.path(threadID)
	if err != nil {
		return History{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return History{}, ErrHistoryNotFound
	}
	if err != nil {
		return History{}, fmt.Errorf("read chat history %q: %w", path, err)
	}
	messages, err := parseHistory(raw)
	if err != nil {
		return History{}, fmt.Errorf("parse chat history %q: %w", path, err)
	}
	return History{ThreadID: strings.TrimSpace(threadID), Messages: messages}, nil
}

func (s *HistoryStore) List(limit int) (ThreadList, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return ThreadList{}, fmt.Errorf("list chat history directory %q: %w", s.dir, err)
	}
	threads := make([]ThreadSummary, 0, len(entries))
	warnings := make([]HistoryWarning, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		threadID := strings.TrimSuffix(entry.Name(), ".md")
		if !threadIDPattern.MatchString(threadID) {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, HistoryWarning{ThreadID: threadID, File: entry.Name(), Message: fmt.Sprintf("read chat history: %s", strings.TrimSpace(err.Error()))})
			continue
		}
		messages, err := parseHistory(raw)
		if err != nil {
			warnings = append(warnings, HistoryWarning{ThreadID: threadID, File: entry.Name(), Message: fmt.Sprintf("parse chat history: %s", strings.TrimSpace(err.Error()))})
			continue
		}
		if len(messages) == 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			warnings = append(warnings, HistoryWarning{ThreadID: threadID, File: entry.Name(), Message: fmt.Sprintf("stat chat history: %s", strings.TrimSpace(err.Error()))})
			continue
		}
		threads = append(threads, summarizeThread(threadID, messages, info.ModTime()))
	}
	sort.Slice(threads, func(i, j int) bool {
		if !threads[i].MessageAt.Equal(threads[j].MessageAt) {
			return threads[i].MessageAt.After(threads[j].MessageAt)
		}
		return threads[i].UpdatedAt.After(threads[j].UpdatedAt)
	})
	if len(threads) > limit {
		threads = threads[:limit]
	}
	return ThreadList{Threads: threads, Warnings: warnings}, nil
}

func (s *HistoryStore) path(threadID string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	if !threadIDPattern.MatchString(threadID) {
		return "", ErrInvalidThreadID
	}
	return filepath.Join(s.dir, threadID+".md"), nil
}

func escapeHistoryText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, userHeadingPrefix) || strings.HasPrefix(line, assistantHeadingPrefix) {
			lines[index] = "\\" + line
		}
	}
	return strings.Join(lines, "\n")
}

func summarizeThread(threadID string, messages []HistoryMessage, updatedAt time.Time) ThreadSummary {
	summary := ThreadSummary{ThreadID: strings.TrimSpace(threadID), UpdatedAt: updatedAt.UTC()}
	for _, message := range messages {
		if summary.MessageAt.IsZero() || message.At.After(summary.MessageAt) {
			summary.MessageAt = message.At
		}
		if summary.Title == "" && message.Role == "user" {
			summary.Title = summarizeText(message.Text, 34)
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.TrimSpace(messages[i].Text) != "" {
			summary.Preview = summarizeText(messages[i].Text, 56)
			break
		}
	}
	if summary.Title == "" {
		summary.Title = "未命名对话"
	}
	if summary.Preview == "" {
		summary.Preview = summary.Title
	}
	if summary.MessageAt.IsZero() {
		summary.MessageAt = summary.UpdatedAt
	}
	return summary
}

func summarizeText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return text
}

func parseHistory(raw []byte) ([]HistoryMessage, error) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	messages := make([]HistoryMessage, 0)
	var current *HistoryMessage
	content := make([]string, 0)
	flush := func() {
		if current == nil {
			return
		}
		current.Text = strings.TrimSpace(strings.Join(content, "\n"))
		if current.Text != "" {
			messages = append(messages, *current)
		}
		current = nil
		content = content[:0]
	}
	for _, line := range lines {
		role, rawTime, ok := historyHeading(line)
		if ok {
			flush()
			at, err := time.Parse(time.RFC3339, rawTime)
			if err != nil {
				return nil, fmt.Errorf("invalid %s message timestamp %q: %w", role, rawTime, err)
			}
			current = &HistoryMessage{Role: role, At: at}
			continue
		}
		if current != nil {
			if strings.HasPrefix(line, "\\"+userHeadingPrefix) || strings.HasPrefix(line, "\\"+assistantHeadingPrefix) {
				line = strings.TrimPrefix(line, "\\")
			}
			content = append(content, line)
		}
	}
	flush()
	return messages, nil
}

func historyHeading(line string) (role, rawTime string, ok bool) {
	switch {
	case strings.HasPrefix(line, userHeadingPrefix):
		return "user", strings.TrimSpace(strings.TrimPrefix(line, userHeadingPrefix)), true
	case strings.HasPrefix(line, assistantHeadingPrefix):
		return "assistant", strings.TrimSpace(strings.TrimPrefix(line, assistantHeadingPrefix)), true
	default:
		return "", "", false
	}
}
