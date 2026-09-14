// Package worldview owns the shared, live navigation projection. It never
// freezes evidence or summarizes it with a model.
package worldview

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

const Budget = 6000

type Filter struct {
	Section, Query string
	Offset, Limit  int
	TaskID         uint64
}
type Section struct {
	Name       string           `json:"name"`
	ReadAt     string           `json:"read_at"`
	Items      []map[string]any `json:"items"`
	Total      int64            `json:"total"`
	Offset     int              `json:"offset"`
	NextOffset int              `json:"next_offset"`
	HasMore    bool             `json:"has_more"`
}
type View struct {
	ReadAt    string         `json:"read_at"`
	Principal map[string]any `json:"principal"`
	Sections  []Section      `json:"sections"`
	Read      string         `json:"read"`
	Note      string         `json:"note"`
}

func Preview(s string, n int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	r := []rune(line)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return line
}
func (v *View) JSON() (json.RawMessage, error) { return json.Marshal(v) }
func (v *View) refresh() {
	for i := range v.Sections {
		s := &v.Sections[i]
		s.NextOffset = s.Offset + len(s.Items)
		s.HasMore = int64(s.NextOffset) < s.Total
	}
}

func Read(ctx context.Context, db *gorm.DB, f Filter) (*View, error) {
	if db == nil {
		return nil, fmt.Errorf("world overview database is nil")
	}
	if f.Offset < 0 || f.Limit < 0 || f.Limit > 100 {
		return nil, fmt.Errorf("invalid world overview pagination")
	}
	v := &View{ReadAt: time.Now().UTC().Format(time.RFC3339), Sections: []Section{}, Read: "get-world-overview --section TYPE --query TEXT --offset N --limit N; get-page/get-task/get-todo for details", Note: "Live partial directory. Task/Todo labels are unverified indexes, not original instructions or progress reports. Person/group/resource details are queried on demand."}
	var profiles []map[string]any
	if err := db.WithContext(ctx).Table("principal_profile").Select("open_id,name,title,department,leader_open_id,leader_name,summary,updated_at").Order("id ASC").Limit(1).Find(&profiles).Error; err != nil {
		return nil, err
	}
	if len(profiles) > 0 {
		v.Principal = profiles[0]
		compact(v.Principal, 200)
	}
	var todoID *uint64
	if f.TaskID > 0 {
		var task struct {
			ID     uint64
			TodoID *uint64
		}
		if err := db.WithContext(ctx).Table("task").Select("id,todo_id").Where("id = ?", f.TaskID).Take(&task).Error; err != nil {
			return nil, err
		}
		todoID = task.TodoID
	}
	names := []string{"project", "key_matter", "task", "recent_task", "todo"}
	if f.Section != "" {
		switch f.Section {
		case "project", "key_matter", "task", "recent_task", "todo", "person", "group", "resource":
			names = []string{f.Section}
		default:
			return nil, fmt.Errorf("unknown world section %q", f.Section)
		}
	}
	for _, name := range names {
		q := db.WithContext(ctx)
		columns := ""
		title := "name"
		searchText := "summary"
		limit := 10
		switch name {
		case "project":
			q = q.Table("project").Where("status = ?", "active")
			columns = "id,name,status,role,summary,updated_at"
			q = q.Order("priority ASC")
		case "key_matter":
			q = q.Table("key_matter").Where("closed_at IS NULL")
			columns = "id,title,summary,due_at,updated_at"
			title = "title"
		case "task", "recent_task":
			q = q.Table("task")
			title = "title"
			searchText = "title"
			columns = "id,title,source_type,status,todo_id,project_id,updated_at"
			q = q.Where("id <> ?", f.TaskID)
			if name == "task" {
				q = q.Where("status NOT IN ?", []string{"done", "failed", "observing"})
			} else {
				q = q.Where("status IN ? AND updated_at >= ?", []string{"done", "failed", "observing"}, time.Now().UTC().AddDate(0, 0, -7))
				limit = 5
			}
		case "todo":
			q = q.Table("todo").Where("status IN ?", []string{"extracted", "observing"})
			if todoID != nil {
				q = q.Where("id <> ?", *todoID)
			}
			title = "title"
			searchText = "source_quote"
			columns = "id,title,status,source_quote,updated_at,(SELECT id FROM task WHERE task.todo_id = todo.id ORDER BY id DESC LIMIT 1) AS task_id"
		case "person":
			q = q.Table("person").Where("is_active = ?", true)
			columns = "id,name,role,title,summary,updated_at"
		case "group":
			q = q.Table("feishu_group")
			columns = "id,name,chat_id,chat_mode,summary,updated_at"
		case "resource":
			q = q.Table("managed_resource").Where("is_active = ?", true)
			title = "title"
			columns = "id,title,resource_type,summary,updated_at"
		}
		if f.Query != "" {
			term := "%" + f.Query + "%"
			q = q.Where("("+title+" LIKE ? OR "+searchText+" LIKE ?)", term, term)
		}
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return nil, fmt.Errorf("world %s count: %w", name, err)
		}
		if f.Limit > 0 {
			limit = f.Limit
		}
		items := []map[string]any{}
		if err := q.Select(columns).Order("updated_at DESC, id DESC").Offset(f.Offset).Limit(limit).Find(&items).Error; err != nil {
			return nil, fmt.Errorf("world %s: %w", name, err)
		}
		for _, item := range items {
			compact(item, 80)
		}
		v.Sections = append(v.Sections, Section{Name: name, ReadAt: v.ReadAt, Items: items, Total: total, Offset: f.Offset})
	}
	v.refresh()
	for index := 0; ; index++ {
		raw, err := v.JSON()
		if err != nil {
			return nil, err
		}
		if utf8.RuneCount(raw) <= Budget {
			return v, nil
		}
		removed := false
		for j := 0; j < len(v.Sections); j++ {
			s := &v.Sections[(index+j)%len(v.Sections)]
			if len(s.Items) > 1 {
				s.Items = s.Items[:len(s.Items)-1]
				removed = true
				break
			}
		}
		if !removed {
			// A pathologically large last entry must degrade to category metadata
			// rather than make every M3/M5 prompt fail. This pass is reached only
			// after each populated category has already been reduced to one item.
			for j := 0; j < len(v.Sections); j++ {
				s := &v.Sections[(index+j)%len(v.Sections)]
				if len(s.Items) > 0 {
					s.Items = nil
					removed = true
					break
				}
			}
		}
		if !removed {
			return nil, fmt.Errorf("world overview metadata exceeds budget")
		}
		v.refresh()
	}
}
func compact(item map[string]any, n int) {
	// Every value in this projection is display text. Bound all textual columns,
	// not only summaries: producer-owned names and titles are intentionally
	// unrestricted and one pathological value must not disable M3/M5 globally.
	for key, value := range item {
		switch body := value.(type) {
		case string:
			item[key] = Preview(body, n)
		case []byte:
			item[key] = Preview(string(body), n)
		}
	}
}
