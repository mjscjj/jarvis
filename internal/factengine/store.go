package factengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Source names are rows in fact_source_cursor, not schema enums. Todo and Task
// advance on their append-only event streams so later state changes are seen;
// their material contains result fields, not the background already preserved
// on the source rows.
const (
	SourceMessage = "message"
	SourceTodo    = "todo"
	SourceTask    = "task"

	// Result payloads can still be much larger than chat messages. Split before
	// a combined unit grows beyond a reliably small agent prompt. A single
	// oversized result is still passed through whole.
	maxStructuredMaterialChars = 50_000
)

// MaterialSource is one mechanical projection into the shared SourceUnit
// protocol. The composition root registers projections explicitly; the worker,
// prompt, extractor and fact persistence path remain unchanged.
type MaterialSource struct {
	Name           string
	StartAtPresent bool
	MaxID          func(context.Context) (uint64, error)
	Units          func(context.Context, uint64, int, WindowOptions) ([]SourceUnit, uint64, error)
}

// WindowOptions cuts a chat's messages into complete conversation windows.
// Windows are material boundaries inside one combined world-maintenance batch.
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

// GORMStore reads material out of the same SQLite source of truth the pipeline
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
	return s.maxID(ctx, &domain.Message{}, SourceMessage)
}

func (s *GORMStore) MaxTodoEventID(ctx context.Context) (uint64, error) {
	return s.maxID(ctx, &domain.TodoEvent{}, SourceTodo)
}

func (s *GORMStore) MaxTaskEventID(ctx context.Context) (uint64, error) {
	return s.maxID(ctx, &domain.TaskEvent{}, SourceTask)
}

func (s *GORMStore) maxID(ctx context.Context, model any, source string) (uint64, error) {
	var maxID *uint64
	if err := s.db.WithContext(ctx).Model(model).Select("MAX(id)").Scan(&maxID).Error; err != nil {
		return 0, fmt.Errorf("load max %s id for fact extraction: %w", source, err)
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
			"last_id":          gorm.Expr("MAX(`last_id`, excluded.`last_id`)"),
			"last_occurred_at": gorm.Expr("excluded.`last_occurred_at`"),
			"updated_at":       gorm.Expr("CURRENT_TIMESTAMP"),
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

// TodoUnits follows todo_event rather than todo.id. Re-extraction mutates a Todo
// in place, so scanning only the main table once would miss every later change.
// The Agent receives lifecycle identity plus the current extraction/resolution
// result. Source messages, context and snapshots remain stored but are not
// copied into world-maintenance material.
func (s *GORMStore) TodoUnits(ctx context.Context, cursor uint64, limit int, opts WindowOptions) ([]SourceUnit, uint64, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("fact engine todo limit must be positive")
	}
	if err := opts.validate(); err != nil {
		return nil, 0, err
	}
	var rows []domain.TodoEvent
	if err := s.db.WithContext(ctx).Preload("Todo").Where("id > ?", cursor).
		Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list todo events for fact extraction cursor=%d: %w", cursor, err)
	}
	materials := make([]todoMaterial, 0, len(rows))
	for i := range rows {
		row := rows[i]
		if row.Todo == nil {
			return nil, 0, fmt.Errorf("todo event id=%d references missing todo id=%d", row.ID, row.TodoID)
		}
		material := todoMaterial{
			Event:    projectTodoEvent(row),
			Result:   projectTodoResult(row.Todo),
			subjects: todoSubjects(row.Todo),
		}
		size, err := jsonMaterialSize(material)
		if err != nil {
			return nil, 0, fmt.Errorf("measure todo event id=%d: %w", row.ID, err)
		}
		material.encodedSize = size
		materials = append(materials, material)
	}
	units := make([]SourceUnit, 0, len(materials))
	for start := 0; start < len(materials); {
		end := structuredMaterialWindowEnd(start, len(materials), opts.MaxMessages, opts.Location,
			func(i int) time.Time { return materials[i].Event.CreatedAt },
			func(i int) int { return materials[i].encodedSize })
		window := materials[start:end]
		body, err := renderJSONMaterial(window)
		if err != nil {
			return nil, 0, fmt.Errorf("render todo events id=%d-%d: %w", window[0].Event.ID, window[len(window)-1].Event.ID, err)
		}
		units = append(units, SourceUnit{
			Source: SourceTodo, Key: fmt.Sprintf("todo_events:%d-%d", window[0].Event.ID, window[len(window)-1].Event.ID),
			LastID: window[len(window)-1].Event.ID, OccurredAt: window[len(window)-1].Event.CreatedAt,
			Context: fmt.Sprintf("material_kind=todo_lifecycle_events\ncount=%d\nwindow=%s .. %s", len(window), window[0].Event.CreatedAt.In(opts.Location).Format(time.RFC3339), window[len(window)-1].Event.CreatedAt.In(opts.Location).Format(time.RFC3339)),
			Body:    body, Subjects: todoMaterialSubjects(window),
		})
		start = end
	}
	return units, lastTodoEventID(rows), nil
}

// TaskUnits does the same for task_event and includes only the linked run's
// final result when present. Frozen background, source payload, plan and run
// prompt remain stored but are not copied into world-maintenance material.
func (s *GORMStore) TaskUnits(ctx context.Context, cursor uint64, limit int, opts WindowOptions) ([]SourceUnit, uint64, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("fact engine task limit must be positive")
	}
	if err := opts.validate(); err != nil {
		return nil, 0, err
	}
	var rows []domain.TaskEvent
	if err := s.db.WithContext(ctx).Preload("Task").Preload("Run").Where("id > ?", cursor).
		Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list task events for fact extraction cursor=%d: %w", cursor, err)
	}
	materials := make([]taskMaterial, 0, len(rows))
	for i := range rows {
		row := rows[i]
		if row.Task == nil {
			return nil, 0, fmt.Errorf("task event id=%d references missing task id=%d", row.ID, row.TaskID)
		}
		if row.RunID != nil && row.Run == nil {
			return nil, 0, fmt.Errorf("task event id=%d references missing execution run id=%d", row.ID, *row.RunID)
		}
		material := taskMaterial{
			Event:     projectTaskEvent(row),
			Result:    projectTaskResult(row.Task),
			RunResult: projectExecutionRunResult(row.Run),
			subjects:  taskSubjects(row.Task),
		}
		size, err := jsonMaterialSize(material)
		if err != nil {
			return nil, 0, fmt.Errorf("measure task event id=%d: %w", row.ID, err)
		}
		material.encodedSize = size
		materials = append(materials, material)
	}
	units := make([]SourceUnit, 0, len(materials))
	for start := 0; start < len(materials); {
		end := structuredMaterialWindowEnd(start, len(materials), opts.MaxMessages, opts.Location,
			func(i int) time.Time { return materials[i].Event.OccurredAt },
			func(i int) int { return materials[i].encodedSize })
		window := materials[start:end]
		body, err := renderJSONMaterial(window)
		if err != nil {
			return nil, 0, fmt.Errorf("render task events id=%d-%d: %w", window[0].Event.ID, window[len(window)-1].Event.ID, err)
		}
		units = append(units, SourceUnit{
			Source: SourceTask, Key: fmt.Sprintf("task_events:%d-%d", window[0].Event.ID, window[len(window)-1].Event.ID),
			LastID: window[len(window)-1].Event.ID, OccurredAt: window[len(window)-1].Event.OccurredAt,
			Context: fmt.Sprintf("material_kind=task_lifecycle_events\ncount=%d\nwindow=%s .. %s", len(window), window[0].Event.OccurredAt.In(opts.Location).Format(time.RFC3339), window[len(window)-1].Event.OccurredAt.In(opts.Location).Format(time.RFC3339)),
			Body:    body, Subjects: taskMaterialSubjects(window),
		})
		start = end
	}
	return units, lastTaskEventID(rows), nil
}

type todoMaterial struct {
	Event       todoEventMaterial  `json:"event"`
	Result      todoResultMaterial `json:"todo_result"`
	subjects    []Subject
	encodedSize int
}

type todoEventMaterial struct {
	ID         uint64    `json:"id"`
	TodoID     uint64    `json:"todo_id"`
	FromStatus *string   `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	CreatedAt  time.Time `json:"created_at"`
}

type todoResultMaterial struct {
	ID               uint64          `json:"id"`
	Title            string          `json:"title"`
	Status           string          `json:"status"`
	ExtractionResult json.RawMessage `json:"extraction_result,omitempty"`
	Resolution       json.RawMessage `json:"resolution,omitempty"`
	ProjectID        *uint64         `json:"project_id,omitempty"`
	GroupID          *uint64         `json:"group_id,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type taskMaterial struct {
	Event       taskEventMaterial           `json:"event"`
	Result      taskResultMaterial          `json:"task_result"`
	RunResult   *executionRunResultMaterial `json:"run_result,omitempty"`
	subjects    []Subject
	encodedSize int
}

type taskEventMaterial struct {
	ID         uint64    `json:"id"`
	TaskID     uint64    `json:"task_id"`
	EventType  string    `json:"event_type"`
	FromStatus *string   `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	RunID      *uint64   `json:"run_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type taskResultMaterial struct {
	ID              uint64          `json:"id"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	ExecutionResult json.RawMessage `json:"execution_result,omitempty"`
	Summary         *string         `json:"summary,omitempty"`
	ProjectID       *uint64         `json:"project_id,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type executionRunResultMaterial struct {
	ID          uint64          `json:"id"`
	TaskID      uint64          `json:"task_id"`
	Status      string          `json:"status"`
	Summary     *string         `json:"summary,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
	Effects     json.RawMessage `json:"effects,omitempty"`
	ErrorDetail *string         `json:"error_detail,omitempty"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
}

func projectTodoEvent(event domain.TodoEvent) todoEventMaterial {
	return todoEventMaterial{
		ID: event.ID, TodoID: event.TodoID, FromStatus: event.FromStatus,
		ToStatus: event.ToStatus, CreatedAt: event.CreatedAt,
	}
}

func projectTodoResult(todo *domain.Todo) todoResultMaterial {
	return todoResultMaterial{
		ID: todo.ID, Title: todo.Title, Status: todo.Status,
		ExtractionResult: json.RawMessage(todo.ExtractionResult),
		Resolution:       json.RawMessage(todo.Resolution),
		ProjectID:        todo.ProjectID, GroupID: todo.GroupID, UpdatedAt: todo.UpdatedAt,
	}
}

func projectTaskEvent(event domain.TaskEvent) taskEventMaterial {
	return taskEventMaterial{
		ID: event.ID, TaskID: event.TaskID, EventType: event.EventType,
		FromStatus: event.FromStatus, ToStatus: event.ToStatus, RunID: event.RunID,
		OccurredAt: event.OccurredAt,
	}
}

func projectTaskResult(task *domain.Task) taskResultMaterial {
	return taskResultMaterial{
		ID: task.ID, Title: task.Title, Status: task.Status,
		ExecutionResult: json.RawMessage(task.ExecutionResult), Summary: task.Summary,
		ProjectID: task.ProjectID, UpdatedAt: task.UpdatedAt,
	}
}

func projectExecutionRunResult(run *domain.ExecutionRun) *executionRunResultMaterial {
	if run == nil {
		return nil
	}
	return &executionRunResultMaterial{
		ID: run.ID, TaskID: run.TaskID, Status: run.Status, Summary: run.Summary,
		Output: json.RawMessage(run.Output), Effects: json.RawMessage(run.Effects),
		ErrorDetail: run.ErrorDetail, FinishedAt: run.FinishedAt,
	}
}

func renderJSONMaterial(value any) (string, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func jsonMaterialSize(value any) (int, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return len(encoded), nil
}

func todoSubjects(todo *domain.Todo) []Subject {
	// Todo is input material, but facts are not currently read back by Todo id.
	// Offer only subjects the rest of the system can actually retrieve; the full
	// Todo id and body remain in CONTEXT/MATERIAL for the agent.
	var subjects []Subject
	if todo.GroupID != nil {
		subjects = append(subjects, Subject{Type: "group", ID: *todo.GroupID})
	}
	if todo.ProjectID != nil {
		subjects = append(subjects, Subject{Type: "project", ID: *todo.ProjectID})
	}
	return subjects
}

func todoMaterialSubjects(materials []todoMaterial) []Subject {
	var subjects []Subject
	seen := make(map[string]struct{})
	for _, material := range materials {
		subjects = appendUniqueSubjects(subjects, seen, material.subjects...)
	}
	return subjects
}

func taskSubjects(task *domain.Task) []Subject {
	subjects := []Subject{{Type: SourceTask, ID: task.ID, Name: task.Title}}
	if task.ProjectID != nil {
		subjects = append(subjects, Subject{Type: "project", ID: *task.ProjectID})
	}
	return subjects
}

func taskMaterialSubjects(materials []taskMaterial) []Subject {
	var subjects []Subject
	seen := make(map[string]struct{})
	for _, material := range materials {
		subjects = appendUniqueSubjects(subjects, seen, material.subjects...)
	}
	return subjects
}

func appendUniqueSubjects(dst []Subject, seen map[string]struct{}, candidates ...Subject) []Subject {
	for _, candidate := range candidates {
		key := fmt.Sprintf("%s/%d", candidate.Type, candidate.ID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		dst = append(dst, candidate)
	}
	return dst
}

// materialWindowEnd batches raw lifecycle events without interpreting them.
// A unit never crosses a local natural day, so facts still land on the day their
// material happened; MaxMessages keeps one prompt bounded.
func materialWindowEnd(start, total, maxItems int, location *time.Location, occurredAt func(int) time.Time) int {
	day := occurredAt(start).In(location).Format("2006-01-02")
	end := start + 1
	for end < total && end-start < maxItems && occurredAt(end).In(location).Format("2006-01-02") == day {
		end++
	}
	return end
}

func structuredMaterialWindowEnd(start, total, maxItems int, location *time.Location, occurredAt func(int) time.Time, encodedSize func(int) int) int {
	day := occurredAt(start).In(location).Format("2006-01-02")
	end := start
	size := 2 // JSON array brackets.
	for end < total && end-start < maxItems && occurredAt(end).In(location).Format("2006-01-02") == day {
		nextSize := encodedSize(end)
		if end > start && size+1+nextSize > maxStructuredMaterialChars {
			break
		}
		size += nextSize + 1
		end++
	}
	return end
}

func lastTodoEventID(rows []domain.TodoEvent) uint64 {
	if len(rows) == 0 {
		return 0
	}
	return rows[len(rows)-1].ID
}

func lastTaskEventID(rows []domain.TaskEvent) uint64 {
	if len(rows) == 0 {
		return 0
	}
	return rows[len(rows)-1].ID
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
