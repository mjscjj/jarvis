package factengine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SourceMessage names the message source in fact.source_kind and in the cursor
// table. Sources are strings rather than an enum: a new one is a row, not a
// schema change.
const SourceMessage = "message"

// WindowOptions cuts a chat's messages into conversation windows. One window is
// one extraction call, so these bound both the prompt size and the cost of a
// round.
type WindowOptions struct {
	Gap         time.Duration
	MaxMessages int
	Location    *time.Location
}

func (o WindowOptions) validate() error {
	if o.Gap <= 0 {
		return fmt.Errorf("fact engine window gap must be positive")
	}
	if o.MaxMessages <= 0 {
		return fmt.Errorf("fact engine window max messages must be positive")
	}
	if o.Location == nil {
		return fmt.Errorf("fact engine window location is nil")
	}
	return nil
}

// GORMStore reads material out of the same MySQL source of truth the pipeline
// writes, and keeps each source's watermark there too.
type GORMStore struct {
	db *gorm.DB
}

func NewGORMStore(db *gorm.DB) (*GORMStore, error) {
	if db == nil {
		return nil, fmt.Errorf("fact engine store db is nil")
	}
	return &GORMStore{db: db}, nil
}

// Cursor returns the source's watermark. The second result is false when the
// source has never run, which the caller must not treat as "start from zero" —
// see Worker.ExtractOnce.
func (s *GORMStore) Cursor(ctx context.Context, source string) (uint64, bool, error) {
	var row domain.FactSourceCursor
	err := s.db.WithContext(ctx).Where("source = ?", source).Take(&row).Error
	if err == nil {
		return row.LastID, true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return 0, false, nil
	}
	return 0, false, fmt.Errorf("load fact source cursor source=%s: %w", source, err)
}

// MaxMessageID is where a first run starts: at the present, not at the oldest
// message ever captured.
func (s *GORMStore) MaxMessageID(ctx context.Context) (uint64, error) {
	var maxID *uint64
	if err := s.db.WithContext(ctx).Model(&domain.Message{}).
		Select("MAX(id)").Scan(&maxID).Error; err != nil {
		return 0, fmt.Errorf("load max message id for fact extraction: %w", err)
	}
	if maxID == nil {
		return 0, nil
	}
	return *maxID, nil
}

// AdvanceCursor moves the watermark forward. It refuses to move backwards: a
// stale round must not re-open material a later round already consumed.
func (s *GORMStore) AdvanceCursor(ctx context.Context, source string, lastID uint64, occurredAt time.Time) error {
	if lastID == 0 {
		return fmt.Errorf("advance fact source cursor source=%s: last_id must be positive", source)
	}
	row := domain.FactSourceCursor{Source: source, LastID: lastID}
	if !occurredAt.IsZero() {
		at := occurredAt.UTC()
		row.LastOccurredAt = &at
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_id":          gorm.Expr("GREATEST(`last_id`, VALUES(`last_id`))"),
			"last_occurred_at": gorm.Expr("VALUES(`last_occurred_at`)"),
		}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("advance fact source cursor source=%s last_id=%d: %w", source, lastID, err)
	}
	return nil
}

// messageRow is the joined projection one message contributes: its own content
// plus the subjects it could produce a fact about.
type messageRow struct {
	ID           uint64
	MessageID    string
	ChatID       string
	ChatName     string
	ChatMode     string
	GroupID      uint64
	ProjectID    *uint64
	ProjectName  *string
	SenderOpenID string
	SenderName   string
	SenderType   string
	MessageType  string
	Content      string
	ReplyTo      *string
	RootID       *string
	ThreadID     *string
	CreateTime   int64
	RenderOK     bool
}

// MessageUnits reads messages above the watermark and cuts them into windows.
// The returned maxID is the highest id consumed, which the caller commits as the
// new watermark once every window's facts are stored — see Worker.ExtractOnce
// for why the cursor moves once per round rather than once per window.
func (s *GORMStore) MessageUnits(ctx context.Context, cursor uint64, limit int, opts WindowOptions) ([]SourceUnit, uint64, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("fact engine message limit must be positive")
	}
	if err := opts.validate(); err != nil {
		return nil, 0, err
	}
	var rows []messageRow
	err := s.db.WithContext(ctx).
		Table("message AS m").
		Select(`m.id, m.message_id, m.chat_id, COALESCE(g.name, '') AS chat_name,
			m.chat_mode, g.id AS group_id,
			g.project_id, p.name AS project_name, m.sender_open_id, m.sender_name,
			m.sender_type, m.message_type, m.content, m.reply_to, m.root_id,
			m.thread_id, m.create_time, m.render_ok`).
		Joins("JOIN feishu_group AS g ON g.chat_id = m.chat_id").
		Joins("LEFT JOIN project AS p ON p.id = g.project_id").
		Where("m.id > ? AND g.related_group = ? AND g.include_in_memory = ?", cursor, true, true).
		Order("m.id ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list messages for fact extraction cursor=%d: %w", cursor, err)
	}
	if len(rows) == 0 {
		return nil, 0, nil
	}
	maxID := rows[0].ID
	for _, row := range rows {
		if row.ID > maxID {
			maxID = row.ID
		}
	}
	persons, err := s.personSubjects(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	units := make([]SourceUnit, 0)
	for _, chat := range groupByChat(rows) {
		for _, window := range splitWindows(chat, opts.Gap, opts.MaxMessages) {
			// Every captured row reaches the agent. Bot/system messages, reactions,
			// cards and imperfect renderings are evidence too; deciding that they do
			// not contain a durable fact is model work, not a Go filter.
			units = append(units, buildUnit(window, persons, opts.Location))
		}
	}
	return units, maxID, nil
}

// personSubjects resolves the senders that are tracked people, so a fact about
// someone binds to their real person row. Senders with no person row simply do
// not become subjects; their words still reach the model in the transcript.
func (s *GORMStore) personSubjects(ctx context.Context, rows []messageRow) (map[string]Subject, error) {
	openIDs := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.SenderOpenID == "" {
			continue
		}
		if _, ok := seen[row.SenderOpenID]; ok {
			continue
		}
		seen[row.SenderOpenID] = struct{}{}
		openIDs = append(openIDs, row.SenderOpenID)
	}
	if len(openIDs) == 0 {
		return map[string]Subject{}, nil
	}
	var people []domain.Person
	if err := s.db.WithContext(ctx).Where("open_id IN ?", openIDs).Find(&people).Error; err != nil {
		return nil, fmt.Errorf("resolve person subjects for fact extraction: %w", err)
	}
	subjects := make(map[string]Subject, len(people))
	for _, person := range people {
		subjects[person.OpenID] = Subject{Type: "person", ID: person.ID, Name: person.Name}
	}
	return subjects, nil
}

func buildUnit(window []messageRow, persons map[string]Subject, location *time.Location) SourceUnit {
	first := window[0]
	last := window[len(window)-1]
	subjects := make([]Subject, 0, 3)
	subjects = append(subjects, Subject{Type: "group", ID: first.GroupID, Name: first.ChatName})
	seen := make(map[uint64]struct{}, len(window))
	for _, row := range window {
		person, ok := persons[row.SenderOpenID]
		if !ok {
			continue
		}
		if _, dup := seen[person.ID]; dup {
			continue
		}
		seen[person.ID] = struct{}{}
		subjects = append(subjects, person)
	}
	if first.ProjectID != nil {
		name := ""
		if first.ProjectName != nil {
			name = *first.ProjectName
		}
		// A project binding is one piece of context, not the organizing axis of
		// the extraction protocol.
		subjects = append(subjects, Subject{Type: "project", ID: *first.ProjectID, Name: name})
	}
	return SourceUnit{
		Source:     SourceMessage,
		Key:        fmt.Sprintf("%s:%d-%d", first.ChatID, first.ID, last.ID),
		LastID:     last.ID,
		OccurredAt: time.UnixMilli(last.CreateTime),
		Context:    renderMessageContext(window, persons, location),
		Body:       renderMessages(window, location),
		Subjects:   subjects,
	}
}

// groupByChat splits an id-ordered scan into per-chat runs, then orders each run
// by conversation time so windowing sees the conversation as it was held. The
// scan is ordered by id (insert order) because the watermark is an id.
func groupByChat(rows []messageRow) [][]messageRow {
	byChat := make(map[string][]messageRow)
	order := make([]string, 0)
	for _, row := range rows {
		if _, ok := byChat[row.ChatID]; !ok {
			order = append(order, row.ChatID)
		}
		byChat[row.ChatID] = append(byChat[row.ChatID], row)
	}
	groups := make([][]messageRow, 0, len(order))
	for _, chatID := range order {
		chat := byChat[chatID]
		sort.SliceStable(chat, func(i, j int) bool {
			if chat[i].CreateTime != chat[j].CreateTime {
				return chat[i].CreateTime < chat[j].CreateTime
			}
			return chat[i].ID < chat[j].ID
		})
		groups = append(groups, chat)
	}
	return groups
}

func splitWindows(rows []messageRow, gap time.Duration, maxMessages int) [][]messageRow {
	if len(rows) == 0 {
		return nil
	}
	windows := make([][]messageRow, 0, 1)
	start := 0
	for i := 1; i < len(rows); i++ {
		timeGap := time.Duration(rows[i].CreateTime-rows[i-1].CreateTime) * time.Millisecond
		if i-start >= maxMessages || timeGap > gap {
			windows = append(windows, rows[start:i])
			start = i
		}
	}
	return append(windows, rows[start:])
}

func renderMessageContext(rows []messageRow, persons map[string]Subject, location *time.Location) string {
	first := rows[0]
	last := rows[len(rows)-1]
	lines := []string{
		fmt.Sprintf("conversation: chat_id=%s name=%q mode=%s", first.ChatID, first.ChatName, first.ChatMode),
		fmt.Sprintf("window: %s .. %s", time.UnixMilli(first.CreateTime).In(location).Format(time.RFC3339), time.UnixMilli(last.CreateTime).In(location).Format(time.RFC3339)),
	}
	if first.ProjectID != nil {
		name := ""
		if first.ProjectName != nil {
			name = *first.ProjectName
		}
		lines = append(lines, fmt.Sprintf("known_association: project/%d name=%q", *first.ProjectID, name))
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.SenderOpenID]; ok {
			continue
		}
		seen[row.SenderOpenID] = struct{}{}
		personID := "unresolved"
		if person, ok := persons[row.SenderOpenID]; ok {
			personID = fmt.Sprintf("%d", person.ID)
		}
		lines = append(lines, fmt.Sprintf("participant: open_id=%s person_id=%s name=%q sender_type=%s", row.SenderOpenID, personID, row.SenderName, row.SenderType))
	}
	return strings.Join(lines, "\n")
}

func renderMessages(rows []messageRow, location *time.Location) string {
	blocks := make([]string, 0, len(rows))
	for _, row := range rows {
		content := strings.ReplaceAll(strings.TrimSpace(row.Content), "\r\n", "\n")
		content = strings.ReplaceAll(content, "\n", "\n    ")
		at := time.UnixMilli(row.CreateTime).In(location).Format(time.RFC3339)
		meta := fmt.Sprintf("message_id=%s time=%s sender_open_id=%s sender_name=%q sender_type=%s message_type=%s render_ok=%t",
			row.MessageID, at, row.SenderOpenID, row.SenderName, row.SenderType, row.MessageType, row.RenderOK)
		for _, ref := range []struct {
			name  string
			value *string
		}{{"reply_to", row.ReplyTo}, {"root_id", row.RootID}, {"thread_id", row.ThreadID}} {
			if ref.value != nil && strings.TrimSpace(*ref.value) != "" {
				meta += " " + ref.name + "=" + *ref.value
			}
		}
		blocks = append(blocks, meta+"\n    "+content)
	}
	return strings.Join(blocks, "\n\n")
}
