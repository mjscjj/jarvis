// Package meetingcapture imports ended Feishu meeting Minutes artifacts into
// Jarvis's existing message-backed M3 evidence pipeline.
package meetingcapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"jarvis/internal/capture"
	"jarvis/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	meetingChatMode     = "meeting"
	meetingSenderOpenID = "__meeting_minutes__"
	meetingSenderName   = "会议妙记"
	meetingMessageType  = "meeting_minutes"

	statusDiscovered        = "discovered"
	statusWaitingMinutes    = "waiting_minutes"
	statusWaitingArtifacts  = "waiting_artifacts"
	statusPermissionDenied  = "permission_denied"
	statusFailed            = "failed"
	statusImported          = "imported"
	waitingRetryDelay       = 10 * time.Minute
	emptyArtifactRetryDelay = 30 * time.Minute
	failedRetryDelay        = 30 * time.Minute
	permissionRetryDelay    = 6 * time.Hour
)

type runner interface {
	Run(context.Context, any, ...string) error
}

type Options struct {
	PrincipalOpenID string
	LookbackDays    int
	ArtifactDir     string
	MaxContentChars int
	Location        *time.Location
}

type Stats struct {
	Discovered       int
	Attempted        int
	Imported         int
	CreatedMessages  int
	Waiting          int
	PermissionDenied int
	Skipped          int
}

type Service struct {
	db       *gorm.DB
	lark     runner
	opts     Options
	observer capture.ScanObserver
	now      func() time.Time
	readFile func(string) ([]byte, error)
	mkdirAll func(string, os.FileMode) error
}

func NewService(db *gorm.DB, lark runner, opts Options) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("meeting capture db is nil")
	}
	if lark == nil {
		return nil, fmt.Errorf("meeting capture lark-cli runner is nil")
	}
	if strings.TrimSpace(opts.PrincipalOpenID) == "" {
		return nil, fmt.Errorf("meeting capture principal open_id is empty")
	}
	if opts.LookbackDays <= 0 || opts.LookbackDays > 30 {
		return nil, fmt.Errorf("meeting capture lookback days must be between 1 and 30")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("meeting capture location is nil")
	}
	cleanArtifactDir := filepath.Clean(strings.TrimSpace(opts.ArtifactDir))
	if cleanArtifactDir == "." || filepath.IsAbs(cleanArtifactDir) || cleanArtifactDir == ".." ||
		strings.HasPrefix(cleanArtifactDir, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("meeting capture artifact dir must be a non-empty relative path")
	}
	if opts.MaxContentChars <= 0 {
		return nil, fmt.Errorf("meeting capture max content chars must be positive")
	}
	opts.ArtifactDir = cleanArtifactDir
	return &Service{
		db: db, lark: lark, opts: opts,
		now: time.Now, readFile: os.ReadFile, mkdirAll: os.MkdirAll,
	}, nil
}

func (s *Service) SetScanObserver(observer capture.ScanObserver) error {
	if observer == nil {
		return fmt.Errorf("meeting capture scan observer is nil")
	}
	if s.observer != nil {
		return fmt.Errorf("meeting capture scan observer is already set")
	}
	s.observer = observer
	return nil
}

// ScanOnce searches ended meetings involving the principal, retrieves any
// available Minutes Todo/transcript artifacts, and emits one stable synthetic
// evidence message per meeting. Individual meeting failures are persisted and
// joined after the rest of the batch has been attempted.
func (s *Service) ScanOnce(ctx context.Context) (Stats, error) {
	if err := s.mkdirAll(s.opts.ArtifactDir, 0o755); err != nil {
		return Stats{}, fmt.Errorf("create meeting artifact directory %q: %w", s.opts.ArtifactDir, err)
	}
	now := s.now().In(s.opts.Location)
	items, err := s.searchMeetings(ctx, now)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Discovered: len(items)}
	failures := make([]error, 0)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		state, eligible, err := s.loadState(item, now)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !eligible {
			stats.Skipped++
			continue
		}
		stats.Attempted++
		if err := s.markAttempt(&state, now); err != nil {
			failures = append(failures, err)
			continue
		}
		outcome, err := s.processMeeting(ctx, &state, item, now)
		switch outcome {
		case statusImported:
			stats.Imported++
			if state.AttemptCount == 1 {
				stats.CreatedMessages++
			}
		case statusWaitingMinutes, statusWaitingArtifacts:
			stats.Waiting++
		case statusPermissionDenied:
			stats.PermissionDenied++
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return stats, errors.Join(failures...)
}

type searchResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Items     []searchItem `json:"items"`
		HasMore   bool         `json:"has_more"`
		PageToken string       `json:"page_token"`
	} `json:"data"`
}

type searchItem struct {
	ID       string `json:"id"`
	Metadata struct {
		AppLink string `json:"app_link"`
	} `json:"meta_data"`
}

func (s *Service) searchMeetings(ctx context.Context, now time.Time) ([]searchItem, error) {
	start := now.AddDate(0, 0, -(s.opts.LookbackDays - 1)).Format(time.DateOnly)
	end := now.Format(time.DateOnly)
	var (
		pageToken string
		items     []searchItem
		seenIDs   = make(map[string]struct{})
		seenPages = make(map[string]struct{})
	)
	for {
		args := []string{
			"vc", "+search", "--participant-ids", s.opts.PrincipalOpenID,
			"--start", start, "--end", end, "--page-size", "30", "--as", "user",
		}
		if pageToken != "" {
			args = append(args, "--page-token", pageToken)
		}
		var response searchResponse
		if err := s.lark.Run(ctx, &response, args...); err != nil {
			return nil, fmt.Errorf("search ended meetings page_token=%q: %w", pageToken, err)
		}
		for _, item := range response.Data.Items {
			item.ID = strings.TrimSpace(item.ID)
			if item.ID == "" {
				return nil, fmt.Errorf("search ended meetings returned blank meeting_id")
			}
			if _, exists := seenIDs[item.ID]; exists {
				continue
			}
			seenIDs[item.ID] = struct{}{}
			items = append(items, item)
		}
		if !response.Data.HasMore {
			return items, nil
		}
		next := strings.TrimSpace(response.Data.PageToken)
		if next == "" {
			return nil, fmt.Errorf("search ended meetings has_more=true with empty page_token")
		}
		if _, repeated := seenPages[next]; repeated {
			return nil, fmt.Errorf("search ended meetings repeated page_token=%q", next)
		}
		seenPages[next] = struct{}{}
		pageToken = next
	}
}

func (s *Service) loadState(item searchItem, now time.Time) (domain.MeetingIngest, bool, error) {
	var state domain.MeetingIngest
	result := s.db.Where("meeting_id = ?", item.ID).Limit(1).Find(&state)
	if result.Error != nil {
		return state, false, fmt.Errorf("load meeting ingest meeting_id=%s: %w", item.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		appLink := stringPointer(item.Metadata.AppLink)
		state = domain.MeetingIngest{
			MeetingID: item.ID, AppLink: appLink, Status: statusDiscovered,
		}
		if err := s.db.Create(&state).Error; err != nil {
			return state, false, fmt.Errorf("create meeting ingest meeting_id=%s: %w", item.ID, err)
		}
		return state, true, nil
	}
	if state.Status == statusImported {
		return state, false, nil
	}
	if state.NextRetryAt != nil && state.NextRetryAt.After(now) {
		return state, false, nil
	}
	if state.AppLink == nil && strings.TrimSpace(item.Metadata.AppLink) != "" {
		state.AppLink = stringPointer(item.Metadata.AppLink)
		if err := s.db.Model(&state).Update("app_link", state.AppLink).Error; err != nil {
			return state, false, fmt.Errorf("update meeting app link meeting_id=%s: %w", item.ID, err)
		}
	}
	return state, true, nil
}

func (s *Service) markAttempt(state *domain.MeetingIngest, now time.Time) error {
	state.AttemptCount++
	state.LastAttemptAt = timePointer(now)
	if err := s.db.Model(state).Updates(map[string]any{
		"attempt_count":   state.AttemptCount,
		"last_attempt_at": now,
	}).Error; err != nil {
		return fmt.Errorf("mark meeting attempt meeting_id=%s: %w", state.MeetingID, err)
	}
	return nil
}

type detailResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Meetings []meetingDetail `json:"meetings"`
	} `json:"data"`
}

type meetingDetail struct {
	MeetingID   string `json:"meeting_id"`
	MeetingNo   string `json:"meeting_no"`
	Topic       string `json:"topic"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	MinuteToken string `json:"minute_token"`
	Error       string `json:"error"`
}

type minutesResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Minutes []minuteDetail `json:"minutes"`
	} `json:"data"`
}

type minuteDetail struct {
	MinuteToken string `json:"minute_token"`
	Title       string `json:"title"`
	NoteID      string `json:"note_id"`
	Error       string `json:"error"`
	Artifacts   struct {
		Todos          json.RawMessage `json:"todos"`
		TranscriptFile string          `json:"transcript_file"`
	} `json:"artifacts"`
}

func (s *Service) processMeeting(ctx context.Context, state *domain.MeetingIngest, item searchItem, now time.Time) (string, error) {
	var details detailResponse
	if err := s.lark.Run(ctx, &details, "vc", "+detail", "--meeting-ids", state.MeetingID, "--as", "user"); err != nil {
		return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
	}
	if len(details.Data.Meetings) != 1 {
		err := fmt.Errorf("meeting detail meeting_id=%s returned %d items, want 1", state.MeetingID, len(details.Data.Meetings))
		return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
	}
	detail := details.Data.Meetings[0]
	if strings.TrimSpace(detail.Error) != "" {
		err := errors.New(detail.Error)
		return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
	}
	startedAt, err := parseMeetingTime(detail.StartTime, s.opts.Location)
	if err != nil {
		return statusFailed, s.fail(state, statusFailed, fmt.Errorf("parse meeting start: %w", err), now.Add(failedRetryDelay))
	}
	endedAt, err := parseMeetingTime(detail.EndTime, s.opts.Location)
	if err != nil {
		return statusFailed, s.fail(state, statusFailed, fmt.Errorf("parse meeting end: %w", err), now.Add(failedRetryDelay))
	}
	state.MeetingNo = stringPointer(detail.MeetingNo)
	state.Topic = stringPointer(detail.Topic)
	state.StartedAt = timePointer(startedAt)
	state.EndedAt = timePointer(endedAt)
	state.MinuteToken = stringPointer(detail.MinuteToken)
	if err := s.db.Model(state).Updates(map[string]any{
		"meeting_no": detail.MeetingNo, "topic": detail.Topic,
		"started_at": startedAt, "ended_at": endedAt, "minute_token": nullableString(detail.MinuteToken),
	}).Error; err != nil {
		return statusFailed, fmt.Errorf("persist meeting detail meeting_id=%s: %w", state.MeetingID, err)
	}
	if strings.TrimSpace(detail.MinuteToken) == "" {
		if err := s.wait(state, statusWaitingMinutes, "meeting has no minute_token yet", now.Add(waitingRetryDelay)); err != nil {
			return statusWaitingMinutes, err
		}
		return statusWaitingMinutes, nil
	}

	var todoResponse minutesResponse
	runErr := s.lark.Run(ctx, &todoResponse,
		"minutes", "+detail", "--minute-tokens", detail.MinuteToken, "--todo", "--as", "user",
	)
	minute, err := minutesItem(todoResponse, runErr, detail.MinuteToken)
	if err != nil {
		if isPermissionError(err) {
			return statusPermissionDenied, s.fail(state, statusPermissionDenied, err, now.Add(permissionRetryDelay))
		}
		return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
	}

	transcript := ""
	if !meaningfulJSON(minute.Artifacts.Todos) {
		var transcriptResponse minutesResponse
		runErr = s.lark.Run(ctx, &transcriptResponse,
			"minutes", "+detail", "--minute-tokens", detail.MinuteToken,
			"--transcript", "--overwrite", "--output-dir", s.opts.ArtifactDir, "--as", "user",
		)
		transcriptMinute, transcriptErr := minutesItem(transcriptResponse, runErr, detail.MinuteToken)
		if transcriptErr != nil {
			if isPermissionError(transcriptErr) {
				return statusPermissionDenied, s.fail(state, statusPermissionDenied, transcriptErr, now.Add(permissionRetryDelay))
			}
			return statusFailed, s.fail(state, statusFailed, transcriptErr, now.Add(failedRetryDelay))
		}
		minute.Artifacts.TranscriptFile = transcriptMinute.Artifacts.TranscriptFile
		transcript, err = s.loadTranscript(minute.Artifacts.TranscriptFile)
		if err != nil {
			return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
		}
	}
	if len(strings.TrimSpace(transcript)) == 0 && !meaningfulJSON(minute.Artifacts.Todos) {
		if err := s.wait(state, statusWaitingArtifacts, "minutes artifacts are empty", now.Add(emptyArtifactRetryDelay)); err != nil {
			return statusWaitingArtifacts, err
		}
		return statusWaitingArtifacts, nil
	}

	content, err := buildEvidence(detail, item.Metadata.AppLink, minute, transcript, s.opts.MaxContentChars)
	if err != nil {
		return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
	}
	group, err := s.ensureMeetingGroup(now)
	if err != nil {
		return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
	}
	messageID := meetingMessageID(state.MeetingID)
	message := domain.Message{
		MessageID: messageID, ChatID: group.ChatID, GroupID: &group.ID,
		ChatMode: meetingChatMode, SenderOpenID: meetingSenderOpenID, SenderName: meetingSenderName,
		SenderType: "system", MessageType: meetingMessageType, Content: content,
		CreateTime: now.UnixMilli(), Source: "meeting", RenderOK: true,
	}
	create := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "message_id"}}, DoNothing: true}).Create(&message)
	if create.Error != nil {
		return statusFailed, s.fail(state, statusFailed, fmt.Errorf("persist meeting evidence message_id=%s: %w", messageID, create.Error), now.Add(failedRetryDelay))
	}
	if create.RowsAffected == 0 {
		if err := s.db.Where("message_id = ?", messageID).First(&message).Error; err != nil {
			return statusFailed, s.fail(state, statusFailed, fmt.Errorf("reload meeting evidence message_id=%s: %w", messageID, err), now.Add(failedRetryDelay))
		}
	}
	if err := s.ensureMinutesResource(&message, &group, detail, minute, transcript); err != nil {
		return statusFailed, s.fail(state, statusFailed, err, now.Add(failedRetryDelay))
	}
	state.Status = statusImported
	state.NextRetryAt = nil
	state.LastError = nil
	state.SourceMessageID = &messageID
	if err := s.db.Model(state).Updates(map[string]any{
		"status": statusImported, "next_retry_at": nil, "last_error": nil, "source_message_id": messageID,
	}).Error; err != nil {
		return statusFailed, fmt.Errorf("mark meeting imported meeting_id=%s: %w", state.MeetingID, err)
	}
	if s.observer != nil {
		if err := s.observer.ChatScanned(ctx, capture.ChatScanResult{
			ChatID: group.ChatID, InsertedCount: 1, MessageIDs: []string{messageID},
			HighWater: message.CreateTime, LastMessageID: &messageID,
		}); err != nil {
			return statusImported, fmt.Errorf("notify M3 for meeting_id=%s: %w", state.MeetingID, err)
		}
	}
	return statusImported, nil
}

func minutesItem(response minutesResponse, runErr error, minuteToken string) (minuteDetail, error) {
	if len(response.Data.Minutes) != 1 {
		if runErr != nil {
			return minuteDetail{}, runErr
		}
		return minuteDetail{}, fmt.Errorf("minutes detail token=%s returned %d items, want 1", minuteToken, len(response.Data.Minutes))
	}
	item := response.Data.Minutes[0]
	if strings.TrimSpace(item.Error) != "" {
		return item, errors.New(item.Error)
	}
	if runErr != nil {
		return item, runErr
	}
	return item, nil
}

func (s *Service) ensureMeetingGroup(now time.Time) (domain.Group, error) {
	chatID := meetingChatID(s.opts.PrincipalOpenID)
	var group domain.Group
	result := s.db.Where("chat_id = ?", chatID).Limit(1).Find(&group)
	if result.Error != nil {
		return group, fmt.Errorf("load meeting evidence group: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		name := "我的会议妙记"
		group = domain.Group{
			ChatID: chatID, ChatMode: meetingChatMode, Name: &name,
			RelatedGroup: true, Tier: "hot", IncludeInMemory: true, LastActiveAt: int64Pointer(now.UnixMilli()),
		}
		if err := s.db.Create(&group).Error; err != nil {
			return group, fmt.Errorf("create meeting evidence group: %w", err)
		}
		return group, nil
	}
	if group.ChatMode != meetingChatMode {
		return group, fmt.Errorf("meeting evidence chat_id=%s already uses chat_mode=%q", chatID, group.ChatMode)
	}
	if err := s.db.Model(&group).Updates(map[string]any{
		"related_group": true, "tier": "hot", "last_active_at": now.UnixMilli(),
	}).Error; err != nil {
		return group, fmt.Errorf("refresh meeting evidence group: %w", err)
	}
	return group, nil
}

func (s *Service) ensureMinutesResource(message *domain.Message, group *domain.Group, detail meetingDetail, minute minuteDetail, transcript string) error {
	fileKey := "minutes:" + detail.MinuteToken
	name := strings.TrimSpace(detail.Topic)
	if name == "" {
		name = strings.TrimSpace(minute.Title)
	}
	sourceMessageID := message.MessageID
	groupID := group.ID
	minuteToken := detail.MinuteToken
	var localPath *string
	if strings.TrimSpace(minute.Artifacts.TranscriptFile) != "" {
		localPath = stringPointer(minute.Artifacts.TranscriptFile)
	}
	hashInput := transcript
	if hashInput == "" {
		hashInput = string(minute.Artifacts.Todos)
	}
	sum := sha256.Sum256([]byte(hashInput))
	contentHash := hex.EncodeToString(sum[:])
	resource := domain.Resource{
		ResourceType: "minutes", FileKey: &fileKey, MinuteToken: &minuteToken,
		Name: stringPointer(name), SourceMessageID: &sourceMessageID, GroupID: &groupID,
		LocalPath: localPath, Downloaded: localPath != nil, ContentHash: &contentHash,
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_message_id"}, {Name: "file_key"}},
		DoNothing: true,
	}).Create(&resource).Error; err != nil {
		return fmt.Errorf("persist meeting Minutes resource meeting_id=%s: %w", detail.MeetingID, err)
	}
	return nil
}

func (s *Service) loadTranscript(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	raw, err := s.readFile(path)
	if err != nil {
		return "", fmt.Errorf("read Minutes transcript %q: %w", path, err)
	}
	return string(raw), nil
}

func (s *Service) wait(state *domain.MeetingIngest, status, reason string, retryAt time.Time) error {
	state.Status = status
	state.NextRetryAt = &retryAt
	state.LastError = &reason
	if err := s.db.Model(state).Updates(map[string]any{
		"status": status, "next_retry_at": retryAt, "last_error": reason,
	}).Error; err != nil {
		return fmt.Errorf("persist meeting wait state meeting_id=%s: %w", state.MeetingID, err)
	}
	return nil
}

func (s *Service) fail(state *domain.MeetingIngest, status string, cause error, retryAt time.Time) error {
	if cause == nil {
		cause = errors.New("unknown meeting capture error")
	}
	message := cause.Error()
	state.Status = status
	state.NextRetryAt = &retryAt
	state.LastError = &message
	if err := s.db.Model(state).Updates(map[string]any{
		"status": status, "next_retry_at": retryAt, "last_error": message,
	}).Error; err != nil {
		return errors.Join(cause, fmt.Errorf("persist meeting failure meeting_id=%s: %w", state.MeetingID, err))
	}
	return fmt.Errorf("meeting_id=%s: %w", state.MeetingID, cause)
}

func buildEvidence(detail meetingDetail, appLink string, minute minuteDetail, transcript string, maxChars int) (string, error) {
	if maxChars <= 0 {
		return "", fmt.Errorf("meeting evidence max chars must be positive")
	}
	todos := "(无)"
	if meaningfulJSON(minute.Artifacts.Todos) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, minute.Artifacts.Todos, "", "  "); err != nil {
			return "", fmt.Errorf("format Minutes todos: %w", err)
		}
		todos = pretty.String()
	}
	header := strings.Join([]string{
		"[会议结束妙记系统证据]",
		"会议主题：" + strings.TrimSpace(detail.Topic),
		"会议 ID：" + strings.TrimSpace(detail.MeetingID),
		"会议时间：" + strings.TrimSpace(detail.StartTime) + " ~ " + strings.TrimSpace(detail.EndTime),
		"会议链接：" + strings.TrimSpace(appLink),
		"妙记 Token：" + strings.TrimSpace(detail.MinuteToken),
		"",
		"妙记 AI 待办（优先作为行动线索，但仍需核对负责人是否是 principal）：",
		truncateMiddle(todos, min(6000, maxChars/3)),
		"",
		"妙记逐字稿（用于发现 AI 待办遗漏的明确交办、负责人和本人承诺）：",
	}, "\n")
	remaining := maxChars - utf8.RuneCountInString(header) - 1
	if remaining <= 0 {
		return truncateMiddle(header, maxChars), nil
	}
	return header + "\n" + truncateMiddle(strings.TrimSpace(transcript), remaining), nil
}

func truncateMiddle(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	const marker = "\n...[中间内容因 M3 上下文上限截断]...\n"
	markerRunes := []rune(marker)
	if maxRunes <= len(markerRunes)+2 {
		return string(runes[:maxRunes])
	}
	available := maxRunes - len(markerRunes)
	head := available / 2
	tail := available - head
	return string(runes[:head]) + marker + string(runes[len(runes)-tail:])
}

func meaningfulJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "[]"
}

func parseMeetingTime(value string, location *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp is empty")
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %q: %w", value, err)
	}
	return parsed, nil
}

func meetingChatID(principalOpenID string) string {
	return "meeting:" + strings.TrimSpace(principalOpenID)
}

func meetingMessageID(meetingID string) string {
	return "vc_meeting_" + strings.TrimSpace(meetingID)
}

func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "permission") || strings.Contains(text, "权限")
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func timePointer(value time.Time) *time.Time { return &value }
func int64Pointer(value int64) *int64        { return &value }
