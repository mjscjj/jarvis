package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAppendMessageEventPersistsAndNotifiesRelatedChat(t *testing.T) {
	db := openMessageEventTestDB(t)
	lastActive := int64(900)
	group := domain.Group{
		ChatID: "oc_related", ChatMode: "topic", RelatedGroup: true,
		Tier: "hot", LastActiveAt: &lastActive,
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	checkpoint := domain.Checkpoint{
		ChatID: "oc_related", HighWaterCreateTime: 1000,
		BackfillDone: true, BackfillSince: 1000,
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	service := &Service{db: db}
	observer := &messageEventObserver{}
	if err := service.SetScanObserver(observer); err != nil {
		t.Fatalf("SetScanObserver() error = %v", err)
	}
	raw := json.RawMessage(`{
		"type":"im.message.receive_v1","event_id":"evt_1",
		"message_id":"om_event_1","chat_id":"oc_related","chat_type":"group",
		"sender_id":"ou_sender","sender_type":"user","message_type":"text",
		"content":"请推进 agency 方案","create_time":"2000",
		"reply_to":"om_parent","root_id":"om_root","thread_id":"omt_thread",
		"mentions":[{"id":"ou_owner","key":"@_user_1","name":"储节节"}]
	}`)

	result, err := service.AppendMessageEvent(context.Background(), raw)
	if err != nil {
		t.Fatalf("AppendMessageEvent() error = %v", err)
	}
	if !result.Inserted || !result.Related || result.MessageID != "om_event_1" {
		t.Fatalf("AppendMessageEvent() result = %#v", result)
	}
	if len(observer.results) != 1 || observer.results[0].HighWater != 2000 {
		t.Fatalf("observer results = %#v", observer.results)
	}

	var message domain.Message
	if err := db.First(&message, "message_id = ?", "om_event_1").Error; err != nil {
		t.Fatalf("load event message: %v", err)
	}
	if message.Source != "event" || message.ChatMode != "topic" || message.Content != "请推进 agency 方案" {
		t.Fatalf("event message = %#v", message)
	}
	if message.ContentRaw == nil || *message.ContentRaw != string(raw) {
		t.Fatalf("content_raw = %v, want exact event payload", message.ContentRaw)
	}
	if message.ReplyTo == nil || *message.ReplyTo != "om_parent" ||
		message.RootID == nil || *message.RootID != "om_root" ||
		message.ThreadID == nil || *message.ThreadID != "omt_thread" {
		t.Fatalf("reply linkage lost: %#v", message)
	}

	var after domain.Checkpoint
	if err := db.First(&after, "chat_id = ?", "oc_related").Error; err != nil {
		t.Fatalf("reload checkpoint: %v", err)
	}
	if after.HighWaterCreateTime != 1000 {
		t.Fatalf("event advanced polling checkpoint to %d, want 1000", after.HighWaterCreateTime)
	}

	redelivery, err := service.AppendMessageEvent(context.Background(), raw)
	if err != nil {
		t.Fatalf("AppendMessageEvent() redelivery error = %v", err)
	}
	if redelivery.Inserted || len(observer.results) != 1 {
		t.Fatalf("redelivery = %#v, observer results = %#v", redelivery, observer.results)
	}
}

func TestAppendMessageEventCreatesUnrelatedDiscoverySkeleton(t *testing.T) {
	db := openMessageEventTestDB(t)
	service := &Service{db: db}
	observer := &messageEventObserver{}
	if err := service.SetScanObserver(observer); err != nil {
		t.Fatalf("SetScanObserver() error = %v", err)
	}
	raw := json.RawMessage(`{
		"type":"im.message.receive_v1","event_id":"evt_new",
		"message_id":"om_new","chat_id":"oc_new","chat_type":"p2p",
		"sender_id":"ou_new","sender_type":"user","message_type":"text",
		"content":"第一条消息","create_time":"3000"
	}`)
	result, err := service.AppendMessageEvent(context.Background(), raw)
	if err != nil {
		t.Fatalf("AppendMessageEvent() error = %v", err)
	}
	if !result.Inserted || result.Related {
		t.Fatalf("AppendMessageEvent() result = %#v", result)
	}
	if len(observer.results) != 0 {
		t.Fatalf("unrelated new chat woke M3: %#v", observer.results)
	}
	var group domain.Group
	if err := db.First(&group, "chat_id = ?", "oc_new").Error; err != nil {
		t.Fatalf("load discovery skeleton: %v", err)
	}
	if group.ChatMode != "p2p" || group.RelatedGroup || group.LastActiveAt == nil || *group.LastActiveAt != 3000 {
		t.Fatalf("discovery skeleton = %#v", group)
	}
	var checkpoint domain.Checkpoint
	if err := db.First(&checkpoint, "chat_id = ?", "oc_new").Error; err != nil {
		t.Fatalf("load skeleton checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != 3000 || checkpoint.BackfillSince != 3000 {
		t.Fatalf("skeleton checkpoint = %#v", checkpoint)
	}
}

func TestValidateMessageEventRejectsMalformedControlFields(t *testing.T) {
	valid := MessageEvent{
		Type: messageEventType, MessageID: "om_1", ChatID: "oc_1", ChatType: "group",
		SenderID: "ou_1", SenderType: "user", MessageType: "text", CreateTime: "1000",
	}
	tests := []struct {
		name   string
		mutate func(*MessageEvent)
	}{
		{name: "wrong type", mutate: func(event *MessageEvent) { event.Type = "other" }},
		{name: "blank message id", mutate: func(event *MessageEvent) { event.MessageID = "" }},
		{name: "unknown chat type", mutate: func(event *MessageEvent) { event.ChatType = "topic" }},
		{name: "invalid create time", mutate: func(event *MessageEvent) { event.CreateTime = "now" }},
		{name: "invalid update time", mutate: func(event *MessageEvent) { event.UpdateTime = "0" }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			event := valid
			testCase.mutate(&event)
			if err := validateMessageEvent(event); err == nil {
				t.Fatalf("validateMessageEvent(%#v) unexpectedly succeeded", event)
			}
		})
	}
}

func openMessageEventTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		TranslateError:                           true,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&eventTestGroup{}, &eventTestMessage{}, &eventTestCheckpoint{}, &eventTestResource{}); err != nil {
		t.Fatalf("migrate event test tables: %v", err)
	}
	return db
}

type messageEventObserver struct {
	results []ChatScanResult
}

func (o *messageEventObserver) ChatScanned(_ context.Context, result ChatScanResult) error {
	o.results = append(o.results, result)
	return nil
}

// SQLite cannot parse the MySQL enum declarations on the production models.
// These table-only mirrors keep the same columns and indexes without carrying
// associations or dialect-specific DDL into this focused persistence test.
type eventTestGroup struct {
	ID              uint64 `gorm:"primaryKey;autoIncrement"`
	ChatID          string `gorm:"uniqueIndex"`
	ChatMode        string
	Name            *string
	Description     *string
	BackgroundNote  *string
	OwnerOpenID     *string
	External        bool
	TenantKey       *string
	P2PTargetType   *string `gorm:"column:p2p_target_type"`
	ProjectID       *uint64
	RelatedGroup    bool
	Tier            string
	Pinned          bool
	IncludeInMemory bool
	IsKeyGroup      bool
	LastActiveAt    *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (eventTestGroup) TableName() string { return "feishu_group" }

type eventTestMessage struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	MessageID    string `gorm:"uniqueIndex"`
	ChatID       string
	GroupID      *uint64
	ChatMode     string
	SenderOpenID string
	SenderName   string
	SenderType   string
	MessageType  string
	Content      string
	ContentRaw   *string
	MentionsJSON datatypes.JSON
	ReplyTo      *string
	RootID       *string
	ThreadID     *string
	CreateTime   int64
	UpdateTime   *int64
	Source       string
	RenderOK     bool
	CreatedAt    time.Time
}

func (eventTestMessage) TableName() string { return "message" }

type eventTestCheckpoint struct {
	ChatID              string `gorm:"primaryKey"`
	HighWaterCreateTime int64
	LastMessageID       *string
	BackfillDone        bool
	BackfillSince       int64
	LastScanAt          *time.Time
	LastScanStatus      *string
	LastError           *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (eventTestCheckpoint) TableName() string { return "chat_checkpoint" }

type eventTestResource struct {
	ID              uint64 `gorm:"primaryKey;autoIncrement"`
	ResourceType    string
	FileKey         *string `gorm:"uniqueIndex:uk_resource_msg_key,priority:2"`
	MinuteToken     *string
	DocToken        *string
	URL             *string
	Name            *string
	MIMEType        *string
	SizeBytes       *int64
	SourceMessageID *string `gorm:"uniqueIndex:uk_resource_msg_key,priority:1"`
	GroupID         *uint64
	LocalPath       *string
	Downloaded      bool
	ContentHash     *string
	ExtractedText   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (eventTestResource) TableName() string { return "resource" }
