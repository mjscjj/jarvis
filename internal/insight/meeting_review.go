package insight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

const meetingClueChatID = "clue:feishu_meeting"

var ErrInvalidReviewDate = errors.New("invalid review date")

// MeetingReviewService projects the existing meeting clue -> Todo -> Task ->
// ExecutionRun chain for the Review page. It owns no meeting state and never
// fetches Feishu or generates a summary.
type MeetingReviewService struct {
	db       *gorm.DB
	location *time.Location
}

type MeetingReviewList struct {
	Date  string              `json:"date"`
	Items []MeetingReviewItem `json:"items"`
}

type MeetingReviewItem struct {
	MeetingID          string           `json:"meeting_id"`
	Title              string           `json:"title"`
	OccurredAt         string           `json:"occurred_at"`
	StartAt            string           `json:"start_at"`
	EndAt              string           `json:"end_at"`
	Host               string           `json:"host"`
	Participants       string           `json:"participants"`
	MeetingURL         string           `json:"meeting_url"`
	TaskID             *uint64          `json:"task_id"`
	TaskStatus         string           `json:"task_status"`
	Summary            string           `json:"summary"`
	SummaryGeneratedAt *time.Time       `json:"summary_generated_at"`
	Effects            []map[string]any `json:"effects"`
	messageID          string
}

type meetingSummaryEnrichment struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Content string `json:"content"`
}

func NewMeetingReviewService(db *gorm.DB, location *time.Location) (*MeetingReviewService, error) {
	if db == nil {
		return nil, fmt.Errorf("meeting review service db is nil")
	}
	if location == nil {
		return nil, fmt.Errorf("meeting review service location is nil")
	}
	return &MeetingReviewService{db: db, location: location}, nil
}

func (s *MeetingReviewService) Load(ctx context.Context, date string) (*MeetingReviewList, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		date = time.Now().In(s.location).Format("2006-01-02")
	}
	day, err := time.ParseInLocation("2006-01-02", date, s.location)
	if err != nil || day.Format("2006-01-02") != date {
		return nil, fmt.Errorf("%w: date %q must be YYYY-MM-DD", ErrInvalidReviewDate, date)
	}

	var messages []domain.Message
	like := "%线索发生时间：" + date + "%"
	if err := s.db.WithContext(ctx).
		Where("chat_id = ? AND content LIKE ?", meetingClueChatID, like).
		Order("create_time ASC, id ASC").
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("load meeting clues for %s: %w", date, err)
	}

	items := make([]MeetingReviewItem, 0, len(messages))
	messageIDs := make(map[string]struct{}, len(messages))
	groupIDs := make(map[uint64]struct{})
	for i := range messages {
		item, occurred, err := parseMeetingClue(&messages[i])
		if err != nil {
			return nil, err
		}
		if occurred.In(s.location).Format("2006-01-02") != date {
			continue
		}
		items = append(items, item)
		messageIDs[item.messageID] = struct{}{}
		if messages[i].GroupID != nil {
			groupIDs[*messages[i].GroupID] = struct{}{}
		}
	}
	if len(items) == 0 {
		return &MeetingReviewList{Date: date, Items: items}, nil
	}

	tasksByMessage, err := s.loadMeetingTasks(ctx, groupIDs, messageIDs)
	if err != nil {
		return nil, err
	}
	taskIDs := make([]uint64, 0, len(tasksByMessage))
	seenTaskIDs := make(map[uint64]struct{}, len(tasksByMessage))
	for _, task := range tasksByMessage {
		if _, ok := seenTaskIDs[task.ID]; !ok {
			seenTaskIDs[task.ID] = struct{}{}
			taskIDs = append(taskIDs, task.ID)
		}
	}
	summariesByTask, err := s.loadMeetingSummaries(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	for i := range items {
		task, ok := tasksByMessage[items[i].messageID]
		if !ok {
			continue
		}
		items[i].TaskID = &task.ID
		items[i].TaskStatus = task.Status
		if summary, ok := summariesByTask[task.ID]; ok {
			items[i].Summary = summary.Content
			items[i].SummaryGeneratedAt = summary.GeneratedAt
			items[i].Effects = summary.Effects
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := meetingSortTime(items[i], s.location)
		right := meetingSortTime(items[j], s.location)
		if left.Equal(right) {
			return items[i].Title < items[j].Title
		}
		return left.Before(right)
	})
	return &MeetingReviewList{Date: date, Items: items}, nil
}

func parseMeetingClue(message *domain.Message) (MeetingReviewItem, time.Time, error) {
	if message == nil || strings.TrimSpace(message.MessageID) == "" {
		return MeetingReviewItem{}, time.Time{}, fmt.Errorf("parse meeting clue: message is invalid")
	}
	lines := strings.Split(message.Content, "\n")
	if len(lines) < 2 {
		return MeetingReviewItem{}, time.Time{}, fmt.Errorf("parse meeting clue %s: content is incomplete", message.MessageID)
	}
	fields := make(map[string]string, len(lines))
	for _, line := range lines[1:] {
		key, value, found := strings.Cut(line, "：")
		if found {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	meetingID := strings.TrimSpace(fields["会议 ID"])
	if meetingID == "" {
		meetingID = strings.TrimPrefix(message.MessageID, meetingClueChatID+":")
	}
	if meetingID == "" || meetingID == message.MessageID {
		return MeetingReviewItem{}, time.Time{}, fmt.Errorf("parse meeting clue %s: meeting id is missing", message.MessageID)
	}
	title := strings.TrimSpace(fields["会议主题"])
	if title == "" {
		title = strings.TrimSpace(strings.TrimPrefix(lines[0], "会议结束："))
	}
	if title == "" {
		return MeetingReviewItem{}, time.Time{}, fmt.Errorf("parse meeting clue %s: title is missing", message.MessageID)
	}
	occurredAt, err := time.Parse(time.RFC3339, fields["线索发生时间"])
	if err != nil {
		return MeetingReviewItem{}, time.Time{}, fmt.Errorf("parse meeting clue %s occurred_at: %w", message.MessageID, err)
	}
	return MeetingReviewItem{
		MeetingID: meetingID, Title: title, OccurredAt: occurredAt.Format(time.RFC3339),
		StartAt: fields["开始时间"], EndAt: fields["结束时间"], Host: fields["主持人"],
		Participants: fields["参会人"], MeetingURL: fields["会议链接"],
		Effects: make([]map[string]any, 0), messageID: message.MessageID,
	}, occurredAt, nil
}

func (s *MeetingReviewService) loadMeetingTasks(
	ctx context.Context,
	groupIDs map[uint64]struct{},
	messageIDs map[string]struct{},
) (map[string]domain.Task, error) {
	result := make(map[string]domain.Task)
	if len(groupIDs) == 0 {
		return result, nil
	}
	ids := make([]uint64, 0, len(groupIDs))
	for id := range groupIDs {
		ids = append(ids, id)
	}
	var todos []domain.Todo
	if err := s.db.WithContext(ctx).Where("group_id IN ?", ids).Find(&todos).Error; err != nil {
		return nil, fmt.Errorf("load meeting Todos: %w", err)
	}
	todoMessageIDs := make(map[uint64][]string)
	todoIDs := make([]uint64, 0, len(todos))
	for i := range todos {
		var sourceIDs []string
		if err := json.Unmarshal(todos[i].SourceMessageIDs, &sourceIDs); err != nil {
			return nil, fmt.Errorf("decode meeting Todo %d source_message_ids: %w", todos[i].ID, err)
		}
		for _, sourceID := range sourceIDs {
			if _, ok := messageIDs[sourceID]; ok {
				todoMessageIDs[todos[i].ID] = append(todoMessageIDs[todos[i].ID], sourceID)
			}
		}
		if len(todoMessageIDs[todos[i].ID]) > 0 {
			todoIDs = append(todoIDs, todos[i].ID)
		}
	}
	if len(todoIDs) == 0 {
		return result, nil
	}
	var tasks []domain.Task
	if err := s.db.WithContext(ctx).Where("todo_id IN ?", todoIDs).Order("id ASC").Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("load meeting Tasks: %w", err)
	}
	for i := range tasks {
		if tasks[i].TodoID == nil {
			continue
		}
		for _, messageID := range todoMessageIDs[*tasks[i].TodoID] {
			result[messageID] = tasks[i]
		}
	}
	return result, nil
}

type projectedMeetingSummary struct {
	Content     string
	GeneratedAt *time.Time
	Effects     []map[string]any
}

func (s *MeetingReviewService) loadMeetingSummaries(ctx context.Context, taskIDs []uint64) (map[uint64]projectedMeetingSummary, error) {
	result := make(map[uint64]projectedMeetingSummary)
	if len(taskIDs) == 0 {
		return result, nil
	}
	var runs []domain.ExecutionRun
	if err := s.db.WithContext(ctx).
		Where("task_id IN ?", taskIDs).
		Order("started_at DESC, id DESC").
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("load meeting execution runs: %w", err)
	}
	for i := range runs {
		if _, exists := result[runs[i].TaskID]; exists || len(runs[i].Output) == 0 {
			continue
		}
		var output struct {
			Enrichments []meetingSummaryEnrichment `json:"enrichments"`
		}
		if err := json.Unmarshal(runs[i].Output, &output); err != nil {
			return nil, fmt.Errorf("decode meeting execution run %d output: %w", runs[i].ID, err)
		}
		for _, enrichment := range output.Enrichments {
			if enrichment.Kind != "meeting_summary" || strings.TrimSpace(enrichment.Content) == "" {
				continue
			}
			effects := make([]map[string]any, 0)
			if len(runs[i].Effects) > 0 {
				if err := json.Unmarshal(runs[i].Effects, &effects); err != nil {
					return nil, fmt.Errorf("decode meeting execution run %d effects: %w", runs[i].ID, err)
				}
			}
			generatedAt := runs[i].FinishedAt
			if generatedAt == nil {
				startedAt := runs[i].StartedAt
				generatedAt = &startedAt
			}
			result[runs[i].TaskID] = projectedMeetingSummary{
				Content: strings.TrimSpace(enrichment.Content), GeneratedAt: generatedAt, Effects: effects,
			}
			break
		}
	}
	return result, nil
}

func meetingSortTime(item MeetingReviewItem, location *time.Location) time.Time {
	for _, value := range []string{item.StartAt, item.OccurredAt} {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.In(location)
		}
	}
	return time.Date(9999, time.December, 31, 23, 59, 59, 0, location)
}
