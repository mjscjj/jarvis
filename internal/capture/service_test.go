package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/larkcli"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDiscoverChatsRotatesCurrentActiveP2PTopN(t *testing.T) {
	db := newDiscoverTestDB(t)
	fixture := &discoverRotationFixture{}
	service := newDiscoverTestService(t, db, fixture, 2)
	first := time.Date(2026, 8, 25, 20, 0, 0, 0, service.opts.Location)
	service.now = func() time.Time { return first }
	fixture.pages = map[string]discoverPage{
		"": {chats: discoverP2PChats("oc_first", "oc_second", "oc_third")},
	}
	if err := service.DiscoverChats(context.Background()); err != nil {
		t.Fatalf("first DiscoverChats() error = %v", err)
	}
	assertDiscoverRelated(t, db, "oc_first", true)
	assertDiscoverRelated(t, db, "oc_second", true)
	assertDiscoverRelated(t, db, "oc_third", false)

	second := first.Add(4 * time.Hour)
	service.now = func() time.Time { return second }
	fixture.pages = map[string]discoverPage{
		"": {chats: discoverP2PChats("oc_third", "oc_first", "oc_second")},
	}
	if err := service.DiscoverChats(context.Background()); err != nil {
		t.Fatalf("rotating DiscoverChats() error = %v", err)
	}
	assertDiscoverRelated(t, db, "oc_third", true)
	assertDiscoverRelated(t, db, "oc_first", true)
	assertDiscoverRelated(t, db, "oc_second", false)
	var thirdCheckpoint domain.Checkpoint
	if err := db.Where("chat_id = ?", "oc_third").Take(&thirdCheckpoint).Error; err != nil {
		t.Fatalf("load newly monitored checkpoint: %v", err)
	}
	wantActivationStart := second.Add(-service.opts.ActivationContext).UnixMilli()
	if thirdCheckpoint.HighWaterCreateTime != wantActivationStart {
		t.Fatalf("newly monitored checkpoint = %d, want activation start %d", thirdCheckpoint.HighWaterCreateTime, wantActivationStart)
	}

	if err := db.Model(&domain.Group{}).Where("chat_id = ?", "oc_second").
		Updates(map[string]any{"related_group": true, "pinned": true}).Error; err != nil {
		t.Fatalf("pin second p2p: %v", err)
	}
	fixture.pages = map[string]discoverPage{
		"": {chats: discoverP2PChats("oc_second", "oc_third", "oc_first")},
	}
	if err := service.DiscoverChats(context.Background()); err != nil {
		t.Fatalf("pinned DiscoverChats() error = %v", err)
	}
	assertDiscoverRelated(t, db, "oc_second", true)
	assertDiscoverRelated(t, db, "oc_third", true)
	assertDiscoverRelated(t, db, "oc_first", true)
	var autoCount int64
	if err := db.Model(&domain.Group{}).
		Where("chat_mode = ? AND p2p_target_type = ? AND pinned = ? AND related_group = ?", "p2p", "user", false, true).
		Count(&autoCount).Error; err != nil {
		t.Fatalf("count automatic p2p monitoring set: %v", err)
	}
	if autoCount != 2 {
		t.Fatalf("automatic p2p monitoring count = %d, want 2", autoCount)
	}
}

func TestDiscoverChatsDoesNotRotateFromPartialListing(t *testing.T) {
	db := newDiscoverTestDB(t)
	fixture := &discoverRotationFixture{}
	service := newDiscoverTestService(t, db, fixture, 2)
	service.now = func() time.Time {
		return time.Date(2026, 8, 25, 20, 0, 0, 0, service.opts.Location)
	}
	fixture.pages = map[string]discoverPage{
		"": {chats: discoverP2PChats("oc_first", "oc_second", "oc_third")},
	}
	if err := service.DiscoverChats(context.Background()); err != nil {
		t.Fatalf("initial DiscoverChats() error = %v", err)
	}

	fixture.pages = map[string]discoverPage{
		"":     {chats: discoverP2PChats("oc_third"), hasMore: true, pageToken: "next"},
		"next": {err: errors.New("page failed")},
	}
	if err := service.DiscoverChats(context.Background()); err == nil || !strings.Contains(err.Error(), "page failed") {
		t.Fatalf("partial DiscoverChats() error = %v, want page failure", err)
	}
	assertDiscoverRelated(t, db, "oc_first", true)
	assertDiscoverRelated(t, db, "oc_second", true)
	assertDiscoverRelated(t, db, "oc_third", false)
}

type discoverPage struct {
	chats     []CLIChat
	hasMore   bool
	pageToken string
	err       error
}

type discoverRotationFixture struct {
	pages map[string]discoverPage
}

func (f *discoverRotationFixture) Run(_ context.Context, out any, args ...string) error {
	if !strings.Contains(strings.Join(args, " "), "+chat-list") {
		return nil
	}
	page, ok := f.pages[argValue(args, "--page-token")]
	if !ok {
		return fmt.Errorf("unexpected discover page token %q", argValue(args, "--page-token"))
	}
	if page.err != nil {
		return page.err
	}
	response := out.(*ChatListResponse)
	response.OK = true
	response.Data.Chats = append([]CLIChat(nil), page.chats...)
	response.Data.HasMore = page.hasMore
	response.Data.PageToken = page.pageToken
	return nil
}

func discoverP2PChats(chatIDs ...string) []CLIChat {
	chats := make([]CLIChat, 0, len(chatIDs))
	for _, chatID := range chatIDs {
		chats = append(chats, CLIChat{ChatID: chatID, ChatMode: "p2p", Name: chatID, P2PTargetType: "user"})
	}
	return chats
}

func newDiscoverTestService(t *testing.T, db *gorm.DB, runner runner, topN int) *Service {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	service, err := NewService(db, runner, Options{
		PageSize: 50, ScanWorkers: 1, HotAge: 6 * time.Hour, WarmAge: 7 * 24 * time.Hour,
		Location: location, PrincipalOpenID: "ou_principal", SearchOverlap: 10 * time.Minute,
		ActivationContext: 2 * time.Hour, AutoRelatedP2PTopN: topN,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func newDiscoverTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Checkpoint{}, &domain.ScanRecord{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func assertDiscoverRelated(t *testing.T, db *gorm.DB, chatID string, want bool) {
	t.Helper()
	var group domain.Group
	if err := db.Select("related_group").Where("chat_id = ?", chatID).Take(&group).Error; err != nil {
		t.Fatalf("load group %s: %v", chatID, err)
	}
	if group.RelatedGroup != want {
		t.Fatalf("group %s related_group = %t, want %t", chatID, group.RelatedGroup, want)
	}
}

func TestNormalizeChatIDs(t *testing.T) {
	got, err := normalizeChatIDs([]string{" oc_one ", "oc_two"})
	if err != nil {
		t.Fatalf("normalizeChatIDs() error = %v", err)
	}
	if strings.Join(got, ",") != "oc_one,oc_two" {
		t.Fatalf("normalizeChatIDs() = %#v", got)
	}
	if _, err := normalizeChatIDs([]string{"oc_one", "oc_one"}); err == nil {
		t.Fatal("normalizeChatIDs() accepted duplicate chat_id")
	}
	if _, err := normalizeChatIDs([]string{"oc_one", " "}); err == nil {
		t.Fatal("normalizeChatIDs() accepted empty chat_id")
	}

	dynamic := make([]string, 21)
	for i := range dynamic {
		dynamic[i] = fmt.Sprintf("oc_%02d", i)
	}
	got, err = normalizeChatIDs(dynamic)
	if err != nil {
		t.Fatalf("normalizeChatIDs() rejected dynamic list larger than 20: %v", err)
	}
	if len(got) != len(dynamic) {
		t.Fatalf("normalizeChatIDs() length = %d, want %d", len(got), len(dynamic))
	}
}

func TestParseCLITime(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	got, err := parseCLITime("2026-07-19 10:25", location)
	if err != nil {
		t.Fatalf("parseCLITime() error = %v", err)
	}
	want := time.Date(2026, 7, 19, 10, 25, 0, 0, location).UnixMilli()
	if got != want {
		t.Fatalf("parseCLITime() = %d, want %d", got, want)
	}
	if _, err := parseCLITime("2026/07/19 10:25", location); err == nil {
		t.Fatal("parseCLITime() accepted an unobserved timestamp layout")
	}
}

func TestFlattenMessages(t *testing.T) {
	input := []CLIMessage{{
		MessageID: "root",
		ThreadID:  "thread",
		ThreadReplies: []CLIMessage{{
			MessageID: "reply-1",
			ThreadReplies: []CLIMessage{{
				MessageID: "reply-2",
			}},
		}},
	}}
	got := flattenMessages(input)
	if len(got) != 3 {
		t.Fatalf("flattenMessages() length = %d, want 3", len(got))
	}
	if got[1].RootID != "root" || got[1].ParentID != "root" || got[1].ThreadID != "thread" {
		t.Fatalf("first reply linkage = root:%q parent:%q thread:%q", got[1].RootID, got[1].ParentID, got[1].ThreadID)
	}
	if got[2].RootID != "root" || got[2].ParentID != "reply-1" || got[2].ThreadID != "thread" {
		t.Fatalf("nested reply linkage = root:%q parent:%q thread:%q", got[2].RootID, got[2].ParentID, got[2].ThreadID)
	}
}

func TestToDomainMessageSystemSender(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	svc := &Service{opts: Options{Location: location}}
	group := &domain.Group{ID: 1, ChatID: "oc_fixture", ChatMode: "group"}

	// 群系统消息（msg_type=system、无 sender，message_id 前缀仍是 om_）应落库为占位
	// sender，而不是报错。这正是 "测试用户 invited ... to the group" 这类系统消息的形状。
	sys, err := svc.toDomainMessage(group, CLIMessage{
		MessageID: "om_x100b6ae8", MessageType: "system", CreateTime: "2026-07-19 10:00",
		Content: "测试用户 invited local dev to the group.", Sender: CLISender{},
	})
	if err != nil {
		t.Fatalf("system message rejected: %v", err)
	}
	if sys.SenderOpenID != systemSenderOpenID || sys.SenderType != systemMessageType {
		t.Fatalf("system sender = open_id:%q type:%q", sys.SenderOpenID, sys.SenderType)
	}

	// 普通消息若 sender 为空，仍 fail-fast 暴露问题。
	if _, err := svc.toDomainMessage(group, CLIMessage{
		MessageID: "om_normal", MessageType: "text", CreateTime: "2026-07-19 10:00",
		Sender: CLISender{},
	}); err == nil {
		t.Fatal("normal message with empty sender should fail-fast")
	}
}

func TestExtractResourceRefs(t *testing.T) {
	content := `img_key:img_v3_demo img_key:img_v3_demo
[minutes](https://example.feishu.cn/minutes/obcnMinute123)
[doc](https://example.feishu.cn/docx/DocToken456)
[link](https://example.com/path)`
	got := extractResourceRefs(content)
	if len(got) != 4 {
		t.Fatalf("extractResourceRefs() length = %d, want 4: %#v", len(got), got)
	}
	if got[0].ResourceType != "image" || got[0].FileKey != "img_v3_demo" {
		t.Errorf("image ref = %#v", got[0])
	}
	if got[1].ResourceType != "minutes" || got[1].MinuteToken == nil || *got[1].MinuteToken != "obcnMinute123" {
		t.Errorf("minutes ref = %#v", got[1])
	}
	if got[2].ResourceType != "doc" || got[2].DocToken == nil || *got[2].DocToken != "DocToken456" {
		t.Errorf("doc ref = %#v", got[2])
	}
	if got[3].ResourceType != "link" || got[3].URL == nil || *got[3].URL != "https://example.com/path" {
		t.Errorf("link ref = %#v", got[3])
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: &larkcli.APIError{Subtype: "rate_limited"}, want: "lark_api_rate_limited"},
		{err: &larkcli.APIError{Subtype: "unknown", Code: json.RawMessage("232001")}, want: "lark_api_232001"},
		{err: &larkcli.CommandError{Cause: errors.New("exit")}, want: "lark_cli_process"},
		{err: errors.New("plain"), want: "errors.errorString"},
	}
	for _, tt := range tests {
		if got := classifyError(tt.err); got != tt.want {
			t.Errorf("classifyError(%T) = %q, want %q", tt.err, got, tt.want)
		}
	}
}
