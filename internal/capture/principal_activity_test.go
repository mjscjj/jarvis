package capture

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jarvis/internal/datatypes"
)

func TestPrincipalActivityOpensGroupAndCapturesTriggerMessage(t *testing.T) {
	location := mustShanghai(t)
	now := time.Date(2026, 7, 27, 16, 20, 0, 0, location)
	messageTime := time.Date(2026, 7, 27, 16, 15, 0, 0, location)
	db := newCaptureTestDB(t)
	createDiscoveredGroup(t, db, "oc_principal_group", "group", false, now.Add(-24*time.Hour))

	runner := &principalActivityFixture{
		principalOpenID: "ou_principal",
		searchMessages: []SearchedMessage{{
			ChatID:     "oc_principal_group",
			ChatName:   "principal group",
			ChatType:   "group",
			CreateTime: messageTime.Format(cliTimeLayout),
			MessageID:  "om_trigger",
			Sender:     CLISender{ID: "ou_principal", Name: "principal", SenderType: "user"},
		}},
		chatMessages: map[string][]CLIMessage{
			"oc_principal_group": {{
				ChatID:      "oc_principal_group",
				Content:     "我来推进这个修复",
				CreateTime:  messageTime.Format(cliTimeLayout),
				MessageID:   "om_trigger",
				MessageType: "text",
				Sender:      CLISender{ID: "ou_principal", Name: "principal", SenderType: "user"},
			}},
		},
	}
	service := newPrincipalActivityService(t, db, runner, location)
	service.now = func() time.Time { return now }
	observer := &activityRecordingObserver{}
	if err := service.SetScanObserver(observer); err != nil {
		t.Fatalf("SetScanObserver() error = %v", err)
	}

	if err := service.ScanPrincipalActivityAndRelated(context.Background()); err != nil {
		t.Fatalf("ScanPrincipalActivityAndRelated() error = %v", err)
	}

	assertActivityRelated(t, db, "oc_principal_group", true)
	var checkpoint domain.Checkpoint
	if err := db.First(&checkpoint, "chat_id = ?", "oc_principal_group").Error; err != nil {
		t.Fatalf("load chat checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != messageTime.UnixMilli() {
		t.Fatalf("chat high water = %d, want trigger time %d", checkpoint.HighWaterCreateTime, messageTime.UnixMilli())
	}
	if checkpoint.BackfillSince != messageTime.Add(-2*time.Hour).UnixMilli() {
		t.Fatalf(
			"chat backfill_since = %d, want bounded context start %d",
			checkpoint.BackfillSince,
			messageTime.Add(-2*time.Hour).UnixMilli(),
		)
	}
	var stored domain.Message
	if err := db.First(&stored, "message_id = ?", "om_trigger").Error; err != nil {
		t.Fatalf("trigger message was not captured: %v", err)
	}
	if stored.Source != "poll" || stored.SenderOpenID != "ou_principal" {
		t.Fatalf("stored trigger message = %#v", stored)
	}
	var activityCheckpoint domain.PrincipalActivityCheckpoint
	if err := db.First(&activityCheckpoint, "principal_open_id = ?", "ou_principal").Error; err != nil {
		t.Fatalf("load principal activity checkpoint: %v", err)
	}
	if activityCheckpoint.LastSearchAt != now.UnixMilli() {
		t.Fatalf("principal activity checkpoint = %d, want %d", activityCheckpoint.LastSearchAt, now.UnixMilli())
	}
	if len(observer.results) != 1 || observer.results[0].ChatID != "oc_principal_group" {
		t.Fatalf("scan observer results = %#v", observer.results)
	}
	searchArgs := strings.Join(runner.searchArgs, " ")
	for _, required := range []string{
		"+messages-search",
		"--sender ou_principal",
		"--chat-type group",
		"--page-all",
		"--no-reactions",
		"--as user",
	} {
		if !strings.Contains(searchArgs, required) {
			t.Errorf("search args %q do not contain %q", searchArgs, required)
		}
	}
}

func TestPrincipalActivityOpensTopicGroup(t *testing.T) {
	location := mustShanghai(t)
	now := time.Date(2026, 7, 27, 16, 20, 0, 0, location)
	messageTime := time.Date(2026, 7, 27, 16, 15, 0, 0, location)
	db := newCaptureTestDB(t)
	createDiscoveredGroup(t, db, "oc_topic_group", "topic", false, now.Add(-24*time.Hour))

	runner := &principalActivityFixture{
		principalOpenID: "ou_principal",
		searchMessages: []SearchedMessage{{
			ChatID:     "oc_topic_group",
			ChatName:   "topic group",
			ChatType:   "topic",
			CreateTime: messageTime.Format(cliTimeLayout),
			MessageID:  "om_topic_trigger",
			Sender:     CLISender{ID: "ou_principal", Name: "principal", SenderType: "user"},
		}},
		chatMessages: map[string][]CLIMessage{"oc_topic_group": nil},
	}
	service := newPrincipalActivityService(t, db, runner, location)
	service.now = func() time.Time { return now }

	if err := service.SyncPrincipalActivityGroups(context.Background()); err != nil {
		t.Fatalf("SyncPrincipalActivityGroups() error = %v", err)
	}

	assertActivityRelated(t, db, "oc_topic_group", true)
	var activityCheckpoint domain.PrincipalActivityCheckpoint
	if err := db.First(&activityCheckpoint, "principal_open_id = ?", "ou_principal").Error; err != nil {
		t.Fatalf("load principal activity checkpoint: %v", err)
	}
	if activityCheckpoint.LastSearchAt != now.UnixMilli() {
		t.Fatalf("principal activity checkpoint = %d, want %d", activityCheckpoint.LastSearchAt, now.UnixMilli())
	}
}

func TestPrincipalActivityFailureDoesNotStopExistingRelatedScan(t *testing.T) {
	location := mustShanghai(t)
	now := time.Date(2026, 7, 27, 16, 20, 0, 0, location)
	messageTime := now.Add(-time.Minute)
	db := newCaptureTestDB(t)
	createDiscoveredGroup(t, db, "oc_existing", "group", true, now.Add(-24*time.Hour))

	runner := &principalActivityFixture{
		principalOpenID: "ou_principal",
		searchErr:       errors.New("search unavailable"),
		chatMessages: map[string][]CLIMessage{
			"oc_existing": {{
				ChatID:      "oc_existing",
				Content:     "existing related evidence",
				CreateTime:  messageTime.Format(cliTimeLayout),
				MessageID:   "om_existing",
				MessageType: "text",
				Sender:      CLISender{ID: "ou_colleague", Name: "colleague", SenderType: "user"},
			}},
		},
	}
	service := newPrincipalActivityService(t, db, runner, location)
	service.now = func() time.Time { return now }

	err := service.ScanPrincipalActivityAndRelated(context.Background())
	if err == nil || !strings.Contains(err.Error(), "search unavailable") {
		t.Fatalf("ScanPrincipalActivityAndRelated() error = %v, want search failure", err)
	}
	var stored domain.Message
	if err := db.First(&stored, "message_id = ?", "om_existing").Error; err != nil {
		t.Fatalf("existing related chat was not scanned after activity failure: %v", err)
	}
	var checkpointCount int64
	if err := db.Model(&domain.PrincipalActivityCheckpoint{}).Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count principal checkpoints: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("principal checkpoint count = %d, want 0 after failed search", checkpointCount)
	}
}

func TestPrincipalActivityRejectsUnexpectedSenderWithoutAdvancingCursor(t *testing.T) {
	location := mustShanghai(t)
	now := time.Date(2026, 7, 27, 16, 20, 0, 0, location)
	db := newCaptureTestDB(t)
	createDiscoveredGroup(t, db, "oc_wrong_sender", "group", false, now.Add(-24*time.Hour))

	runner := &principalActivityFixture{
		principalOpenID: "ou_principal",
		searchMessages: []SearchedMessage{{
			ChatID:     "oc_wrong_sender",
			ChatType:   "group",
			CreateTime: now.Add(-time.Minute).Format(cliTimeLayout),
			MessageID:  "om_wrong_sender",
			Sender:     CLISender{ID: "ou_other", Name: "other", SenderType: "user"},
		}},
	}
	service := newPrincipalActivityService(t, db, runner, location)
	service.now = func() time.Time { return now }

	err := service.SyncPrincipalActivityGroups(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not match principal") {
		t.Fatalf("SyncPrincipalActivityGroups() error = %v, want sender mismatch", err)
	}
	assertActivityRelated(t, db, "oc_wrong_sender", false)
	var count int64
	if err := db.Model(&domain.PrincipalActivityCheckpoint{}).Count(&count).Error; err != nil {
		t.Fatalf("count principal checkpoints: %v", err)
	}
	if count != 0 {
		t.Fatalf("principal checkpoint count = %d, want 0", count)
	}
}

func newPrincipalActivityService(
	t *testing.T,
	db *gorm.DB,
	runner runner,
	location *time.Location,
) *Service {
	t.Helper()
	service, err := NewService(db, runner, Options{
		PageSize:           50,
		ScanWorkers:        1,
		HotAge:             6 * time.Hour,
		WarmAge:            7 * 24 * time.Hour,
		Location:           location,
		PrincipalOpenID:    "ou_principal",
		SearchOverlap:      10 * time.Minute,
		ActivationContext:  2 * time.Hour,
		AutoRelatedP2PTopN: 30,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func newCaptureTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s_%d?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"),
		activityTestDBSerial.Add(1),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	models := []any{
		&activityTestGroup{},
		&activityTestMessage{},
		&activityTestCheckpoint{},
		&activityTestPrincipalCheckpoint{},
		&activityTestScanRecord{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func createDiscoveredGroup(
	t *testing.T,
	db *gorm.DB,
	chatID string,
	chatMode string,
	related bool,
	discoveredAt time.Time,
) {
	t.Helper()
	group := activityTestGroup{
		ChatID: chatID, ChatMode: chatMode, RelatedGroup: related, Tier: "cold",
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group %s: %v", chatID, err)
	}
	checkpoint := domain.Checkpoint{
		ChatID:              chatID,
		HighWaterCreateTime: discoveredAt.UnixMilli(),
		BackfillDone:        true,
		BackfillSince:       discoveredAt.UnixMilli(),
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatalf("create checkpoint %s: %v", chatID, err)
	}
}

type activityTestGroup struct {
	ID           uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ChatID       string    `gorm:"column:chat_id;uniqueIndex"`
	ChatMode     string    `gorm:"column:chat_mode"`
	External     bool      `gorm:"column:external"`
	RelatedGroup bool      `gorm:"column:related_group"`
	Tier         string    `gorm:"column:tier"`
	Pinned       bool      `gorm:"column:pinned"`
	LastActiveAt *int64    `gorm:"column:last_active_at"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (activityTestGroup) TableName() string { return "feishu_group" }

type activityTestMessage struct {
	ID           uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	MessageID    string         `gorm:"column:message_id;uniqueIndex"`
	ChatID       string         `gorm:"column:chat_id"`
	GroupID      *uint64        `gorm:"column:group_id"`
	ChatMode     string         `gorm:"column:chat_mode"`
	SenderOpenID string         `gorm:"column:sender_open_id"`
	SenderName   string         `gorm:"column:sender_name"`
	SenderType   string         `gorm:"column:sender_type"`
	MessageType  string         `gorm:"column:message_type"`
	Content      string         `gorm:"column:content"`
	ContentRaw   *string        `gorm:"column:content_raw"`
	MentionsJSON datatypes.JSON `gorm:"column:mentions_json"`
	ReplyTo      *string        `gorm:"column:reply_to"`
	RootID       *string        `gorm:"column:root_id"`
	ThreadID     *string        `gorm:"column:thread_id"`
	CreateTime   int64          `gorm:"column:create_time"`
	UpdateTime   *int64         `gorm:"column:update_time"`
	Source       string         `gorm:"column:source"`
	RenderOK     bool           `gorm:"column:render_ok"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
}

func (activityTestMessage) TableName() string { return "message" }

type activityTestCheckpoint struct {
	ChatID              string     `gorm:"column:chat_id;primaryKey"`
	HighWaterCreateTime int64      `gorm:"column:high_water_create_time"`
	LastMessageID       *string    `gorm:"column:last_message_id"`
	BackfillDone        bool       `gorm:"column:backfill_done"`
	BackfillSince       int64      `gorm:"column:backfill_since"`
	LastScanAt          *time.Time `gorm:"column:last_scan_at"`
	LastScanStatus      *string    `gorm:"column:last_scan_status"`
	LastError           *string    `gorm:"column:last_error"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (activityTestCheckpoint) TableName() string { return "chat_checkpoint" }

type activityTestPrincipalCheckpoint struct {
	PrincipalOpenID string    `gorm:"column:principal_open_id;primaryKey"`
	LastSearchAt    int64     `gorm:"column:last_search_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (activityTestPrincipalCheckpoint) TableName() string {
	return "principal_activity_checkpoint"
}

type activityTestScanRecord struct {
	ID              uint64     `gorm:"column:id;type:integer;primaryKey;autoIncrement"`
	ScanType        string     `gorm:"column:scan_type"`
	GroupID         *uint64    `gorm:"column:group_id"`
	ChatID          *string    `gorm:"column:chat_id"`
	WindowStart     *int64     `gorm:"column:window_start"`
	WindowEnd       *int64     `gorm:"column:window_end"`
	FetchedCount    int32      `gorm:"column:fetched_count"`
	InsertedCount   int32      `gorm:"column:inserted_count"`
	PageCount       int32      `gorm:"column:page_count"`
	Status          string     `gorm:"column:status"`
	ErrorType       *string    `gorm:"column:error_type"`
	ErrorMessage    *string    `gorm:"column:error_message"`
	HighWaterBefore *int64     `gorm:"column:high_water_before"`
	HighWaterAfter  *int64     `gorm:"column:high_water_after"`
	StartedAt       time.Time  `gorm:"column:started_at"`
	FinishedAt      *time.Time `gorm:"column:finished_at"`
	DurationMS      *int32     `gorm:"column:duration_ms"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
}

func (activityTestScanRecord) TableName() string { return "scan_record" }

type activityRecordingObserver struct {
	results []ChatScanResult
}

func (o *activityRecordingObserver) ChatScanned(_ context.Context, result ChatScanResult) error {
	o.results = append(o.results, result)
	return nil
}

func assertActivityRelated(t *testing.T, db *gorm.DB, chatID string, want bool) {
	t.Helper()
	var group domain.Group
	if err := db.Select("related_group").Where("chat_id = ?", chatID).First(&group).Error; err != nil {
		t.Fatalf("load group %s: %v", chatID, err)
	}
	if group.RelatedGroup != want {
		t.Fatalf("group %s related_group = %t, want %t", chatID, group.RelatedGroup, want)
	}
}

type principalActivityFixture struct {
	principalOpenID string
	searchMessages  []SearchedMessage
	searchErr       error
	chatMessages    map[string][]CLIMessage
	searchArgs      []string
}

func (f *principalActivityFixture) Run(_ context.Context, out any, args ...string) error {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "+messages-search"):
		switch response := out.(type) {
		case *MessageSearchResponse:
			f.searchArgs = append([]string(nil), args...)
			if f.searchErr != nil {
				return f.searchErr
			}
			response.OK = true
			response.Data.Messages = append([]SearchedMessage(nil), f.searchMessages...)
			response.Data.Total = len(f.searchMessages)
			return nil
		case *MessageSearchListResponse:
			response.OK = true
			chatID := argValue(args, "--chat-id")
			response.Data.Messages = append([]CLIMessage(nil), f.chatMessages[chatID]...)
			response.Data.Total = len(response.Data.Messages)
			return nil
		default:
			return fmt.Errorf("unexpected messages-search response type %T", out)
		}
	case strings.Contains(joined, "+chat-messages-list"):
		response := out.(*MessageListResponse)
		response.OK = true
		chatID := argValue(args, "--chat-id")
		response.Data.Messages = append([]CLIMessage(nil), f.chatMessages[chatID]...)
		return nil
	default:
		return fmt.Errorf("unexpected lark-cli args: %s", joined)
	}
}

func argValue(args []string, name string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			return args[index+1]
		}
	}
	return ""
}

func mustShanghai(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	return location
}

var activityTestDBSerial atomic.Uint64
