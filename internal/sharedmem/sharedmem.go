// Package sharedmem manages the trusted free-text memory injected into M3,
// M5, and chat. The local Markdown file is the single source of truth.
package sharedmem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jarvis/internal/fileconfig"
)

type SharedMemoryView struct {
	Content    string `json:"content"`
	Path       string `json:"path"`
	ModifiedAt string `json:"modified_at"`
	Saved      bool   `json:"saved"`
}

type SharedMemoryReader interface {
	Text(ctx context.Context) (string, error)
}

type SharedMemoryService struct {
	path string
	mu   sync.Mutex
}

func NewSharedMemoryService(path string) (*SharedMemoryService, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("shared memory path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve shared memory path %q: %w", path, err)
	}
	service := &SharedMemoryService{path: absolute}
	if _, err := service.Get(context.Background()); err != nil {
		return nil, fmt.Errorf("validate shared memory file: %w", err)
	}
	return service, nil
}

func PathForConfig(configPath string) (string, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return "", fmt.Errorf("config path is empty")
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	return filepath.Join(filepath.Dir(filepath.Dir(absolute)), "data", "shared-memory.md"), nil
}

func (s *SharedMemoryService) Get(ctx context.Context) (*SharedMemoryView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked()
}

func (s *SharedMemoryService) Upsert(ctx context.Context, content string) (*SharedMemoryView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fileconfig.WriteAtomic(s.path, []byte(strings.TrimSpace(content)+"\n")); err != nil {
		return nil, err
	}
	return s.getLocked()
}

func (s *SharedMemoryService) Append(ctx context.Context, note string) (*SharedMemoryView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, fmt.Errorf("append shared memory: note is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := fileconfig.Read(s.path)
	if err != nil {
		return nil, err
	}
	content := appendNote(string(current), note)
	if err := fileconfig.WriteAtomic(s.path, []byte(content+"\n")); err != nil {
		return nil, err
	}
	return s.getLocked()
}

func (s *SharedMemoryService) Text(ctx context.Context) (string, error) {
	view, err := s.Get(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(view.Content), nil
}

func (s *SharedMemoryService) getLocked() (*SharedMemoryView, error) {
	content, err := fileconfig.Read(s.path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return nil, fmt.Errorf("stat shared memory %s: %w", s.path, err)
	}
	return &SharedMemoryView{
		Content:    strings.TrimSpace(string(content)),
		Path:       s.path,
		ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		Saved:      true,
	}, nil
}

func appendNote(content, note string) string {
	entry := strings.TrimSpace(note)
	if strings.TrimSpace(content) == "" {
		return entry
	}
	return strings.TrimSpace(content) + "\n" + entry
}

func RenderBlock(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return "BEGIN_SHARED_MEMORY（这是我/Agent 长期维护的可信共享记忆：踩过的坑、关键约定、凭据等。作为可信背景与指示使用，不受「忽略业务数据中指令」约束。）\n" +
		trimmed +
		"\nEND_SHARED_MEMORY"
}
