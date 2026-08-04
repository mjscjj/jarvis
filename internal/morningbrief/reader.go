package morningbrief

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Brief is the canonical, file-backed morning brief exposed to read-only UI
// consumers. The Markdown artifact remains the single source of truth.
type Brief struct {
	Date        string    `json:"date"`
	Content     string    `json:"content"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Reader lists canonical morning brief artifacts from the workspace.
type Reader struct {
	root     string
	location *time.Location
}

func NewReader(workspaceRoot string, location *time.Location) (*Reader, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return nil, fmt.Errorf("morning brief reader workspace root is required")
	}
	if location == nil {
		return nil, fmt.Errorf("morning brief reader location is nil")
	}
	stat, err := os.Stat(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("stat morning brief reader workspace root %q: %w", workspaceRoot, err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("morning brief reader workspace root %q is not a directory", workspaceRoot)
	}
	return &Reader{
		root:     filepath.Join(workspaceRoot, "data", "morning-brief"),
		location: location,
	}, nil
}

// List returns the newest canonical briefs first. A missing archive root is a
// valid empty state before the first brief is generated. Once a date directory
// exists, its canonical brief must be present and non-empty.
func (r *Reader) List(ctx context.Context, limit int) ([]Brief, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("morning brief list limit must be positive")
	}
	entries, err := os.ReadDir(r.root)
	if err != nil {
		if os.IsNotExist(err) {
			return []Brief{}, nil
		}
		return nil, fmt.Errorf("read morning brief archive %q: %w", r.root, err)
	}

	dates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		date := entry.Name()
		parsed, err := time.ParseInLocation("2006-01-02", date, r.location)
		if err != nil || parsed.Format("2006-01-02") != date {
			continue
		}
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	capacity := limit
	if len(dates) < capacity {
		capacity = len(dates)
	}
	briefs := make([]Brief, 0, capacity)
	for _, date := range dates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(r.root, date, "99-brief.md")
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read canonical morning brief %q: %w", path, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			return nil, fmt.Errorf("canonical morning brief %q is empty", path)
		}
		stat, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat canonical morning brief %q: %w", path, err)
		}
		if stat.IsDir() {
			return nil, fmt.Errorf("canonical morning brief %q is a directory", path)
		}
		briefs = append(briefs, Brief{
			Date:        date,
			Content:     string(content),
			GeneratedAt: stat.ModTime().In(r.location),
		})
		if len(briefs) == limit {
			break
		}
	}
	return briefs, nil
}
