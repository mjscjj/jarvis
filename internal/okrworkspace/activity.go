package okrworkspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ActivityEntry is one small, human-readable audit fact for the OKR pages.
// The business data remains in SQLite; this append-only file is only for the
// lightweight "who changed what" drawer.
type ActivityEntry struct {
	At        time.Time `json:"at"`
	ActorID   string    `json:"actor_id"`
	ActorName string    `json:"actor_name"`
	Surface   string    `json:"surface"`
	Quarter   string    `json:"quarter,omitempty"`
	Week      string    `json:"week,omitempty"`
	PlanID    string    `json:"plan_id,omitempty"`
	Action    string    `json:"action"`
	TargetID  string    `json:"target_id,omitempty"`
	Summary   string    `json:"summary"`
}

type ActivityQuery struct {
	Surface string
	Quarter string
	Week    string
	PlanID  string
	Limit   int
}

type ActivityStore struct {
	directory string
	mu        sync.Mutex
}

func NewActivityStore(directory string) (*ActivityStore, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("create OKR activity store: directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create OKR activity directory: %w", err)
	}
	return &ActivityStore{directory: directory}, nil
}

func (s *ActivityStore) Append(entry ActivityEntry) error {
	if s == nil {
		return fmt.Errorf("append OKR activity: store is nil")
	}
	entry.Surface = strings.TrimSpace(entry.Surface)
	entry.Action = strings.TrimSpace(entry.Action)
	entry.Summary = compactActivityText(entry.Summary, 180)
	entry.ActorID = strings.TrimSpace(entry.ActorID)
	entry.ActorName = compactActivityText(entry.ActorName, 60)
	entry.Quarter = strings.TrimSpace(entry.Quarter)
	entry.Week = strings.TrimSpace(entry.Week)
	entry.PlanID = strings.TrimSpace(entry.PlanID)
	entry.TargetID = strings.TrimSpace(entry.TargetID)
	if entry.Surface == "" || entry.Action == "" || entry.Summary == "" {
		return fmt.Errorf("append OKR activity: surface, action and summary are required")
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	} else {
		entry.At = entry.At.UTC()
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode OKR activity: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.directory, entry.At.Format("2006-01")+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open OKR activity file: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("append OKR activity file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close OKR activity file: %w", err)
	}
	return nil
}

func (s *ActivityStore) List(query ActivityQuery) ([]ActivityEntry, error) {
	if s == nil {
		return nil, fmt.Errorf("list OKR activity: store is nil")
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 100 {
		query.Limit = 100
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	files, err := os.ReadDir(s.directory)
	if err != nil {
		return nil, fmt.Errorf("list OKR activity files: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() > files[j].Name() })
	result := make([]ActivityEntry, 0, query.Limit)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.directory, file.Name()))
		if err != nil {
			return nil, fmt.Errorf("read OKR activity file %s: %w", file.Name(), err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		for index := len(lines) - 1; index >= 0; index-- {
			if strings.TrimSpace(lines[index]) == "" {
				continue
			}
			var entry ActivityEntry
			if err := json.Unmarshal([]byte(lines[index]), &entry); err != nil {
				return nil, fmt.Errorf("decode OKR activity file %s line %d: %w", file.Name(), index+1, err)
			}
			if !matchesActivity(entry, query) {
				continue
			}
			result = append(result, entry)
			if len(result) == query.Limit {
				return result, nil
			}
		}
	}
	return result, nil
}

func matchesActivity(entry ActivityEntry, query ActivityQuery) bool {
	return (query.Surface == "" || entry.Surface == query.Surface) &&
		(query.Quarter == "" || entry.Quarter == query.Quarter) &&
		(query.Week == "" || entry.Week == query.Week) &&
		(query.PlanID == "" || entry.PlanID == query.PlanID)
}

func compactActivityText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
