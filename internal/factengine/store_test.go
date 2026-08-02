package factengine

import (
	"strings"
	"testing"
	"time"
)

func minutes(base time.Time, offset int) int64 {
	return base.Add(time.Duration(offset) * time.Minute).UnixMilli()
}

func TestSplitWindowsCutsOnGapAndSize(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	rows := []messageRow{
		{ID: 1, CreateTime: minutes(base, 0)},
		{ID: 2, CreateTime: minutes(base, 5)},
		// 90 minutes later: a different conversation, not a continuation.
		{ID: 3, CreateTime: minutes(base, 95)},
		{ID: 4, CreateTime: minutes(base, 96)},
		{ID: 5, CreateTime: minutes(base, 97)},
	}
	windows := splitWindows(rows, 30*time.Minute, 40)
	if len(windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(windows))
	}
	if len(windows[0]) != 2 || len(windows[1]) != 3 {
		t.Fatalf("window sizes = %d/%d, want 2/3", len(windows[0]), len(windows[1]))
	}

	// A conversation with no pauses still gets cut, so one busy chat cannot
	// produce a single unbounded prompt.
	sized := splitWindows(rows, 30*time.Minute, 2)
	if len(sized) != 3 {
		t.Fatalf("size-capped windows = %d, want 3", len(sized))
	}
}

func TestGroupByChatOrdersEachChatByConversationTime(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	// Insert order (id) and conversation order (create_time) disagree: a backfill
	// stored an older message later. Windowing has to see the conversation.
	rows := []messageRow{
		{ID: 1, ChatID: "a", CreateTime: minutes(base, 10)},
		{ID: 2, ChatID: "b", CreateTime: minutes(base, 0)},
		{ID: 3, ChatID: "a", CreateTime: minutes(base, 5)},
	}
	groups := groupByChat(rows)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0][0].ID != 3 || groups[0][1].ID != 1 {
		t.Fatalf("chat a order = %d,%d, want 3,1", groups[0][0].ID, groups[0][1].ID)
	}
}

func TestBuildUnitKeepsEveryCapturedMessage(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	rows := []messageRow{
		{ID: 1, MessageID: "om_user", ChatID: "chat-a", ChatName: "上下文群", ChatMode: "group", GroupID: 3, Content: "方案定了走 B", SenderType: "user", SenderName: "张三", CreateTime: minutes(base, 0), RenderOK: true},
		{ID: 2, MessageID: "om_bot", ChatID: "chat-a", ChatName: "上下文群", ChatMode: "group", GroupID: 3, Content: "构建成功", SenderType: "bot", SenderName: "机器人", CreateTime: minutes(base, 1), RenderOK: true},
		{ID: 3, MessageID: "om_raw", ChatID: "chat-a", ChatName: "上下文群", ChatMode: "group", GroupID: 3, Content: "看不懂的原始内容", SenderType: "user", SenderName: "李四", CreateTime: minutes(base, 2), RenderOK: false},
	}
	unit := buildUnit(rows, nil, time.UTC)
	for _, want := range []string{"om_user", "om_bot", "om_raw", "sender_type=bot", "render_ok=false"} {
		if !strings.Contains(unit.Body, want) {
			t.Fatalf("body missing %q:\n%s", want, unit.Body)
		}
	}
}

func TestBuildUnitOffersProjectGroupAndPersonSubjects(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	projectID := uint64(7)
	projectName := "Jarvis"
	rows := []messageRow{
		{ID: 10, MessageID: "om_10", ChatID: "chat-a", ChatName: "研发群", ChatMode: "group", GroupID: 3, ProjectID: &projectID,
			ProjectName: &projectName, SenderOpenID: "ou_1", SenderName: "张三",
			Content: "方案定了走 B", CreateTime: minutes(base, 0), RenderOK: true},
		{ID: 11, MessageID: "om_11", ChatID: "chat-a", ChatName: "研发群", ChatMode: "group", GroupID: 3, ProjectID: &projectID,
			ProjectName: &projectName, SenderOpenID: "ou_2", SenderName: "李四",
			Content: "我明天上线", CreateTime: minutes(base, 2), RenderOK: true},
		// Same person speaking twice must not appear twice in the subject list.
		{ID: 12, MessageID: "om_12", ChatID: "chat-a", ChatName: "研发群", ChatMode: "group", GroupID: 3, ProjectID: &projectID,
			ProjectName: &projectName, SenderOpenID: "ou_1", SenderName: "张三",
			Content: "好", CreateTime: minutes(base, 3), RenderOK: true},
	}
	persons := map[string]Subject{
		"ou_1": {Type: "person", ID: 21, Name: "张三"},
		// ou_2 is not a tracked person: no subject, but the words still get read.
	}
	unit := buildUnit(rows, persons, time.UTC)

	if unit.Source != SourceMessage || unit.LastID != 12 {
		t.Fatalf("unit source/last_id = %s/%d", unit.Source, unit.LastID)
	}
	if !unit.OccurredAt.Equal(time.UnixMilli(minutes(base, 3))) {
		t.Fatalf("unit occurred_at = %v", unit.OccurredAt)
	}
	want := []Subject{
		{Type: "group", ID: 3, Name: "研发群"},
		{Type: "person", ID: 21, Name: "张三"},
		{Type: "project", ID: 7, Name: "Jarvis"},
	}
	if len(unit.Subjects) != len(want) {
		t.Fatalf("subjects = %+v, want %+v", unit.Subjects, want)
	}
	for i := range want {
		if unit.Subjects[i] != want[i] {
			t.Fatalf("subject %d = %+v, want %+v", i, unit.Subjects[i], want[i])
		}
	}
	for _, fragment := range []string{"sender_name=\"张三\"", "方案定了走 B", "我明天上线", "2026-08-01T09:00:00Z", "known_association: project/7"} {
		text := unit.Body + "\n" + unit.Context
		if !strings.Contains(text, fragment) {
			t.Fatalf("unit missing %q:\n%s", fragment, text)
		}
	}
}

// An unbound chat still offers its group, so a fact from it has somewhere to go.
func TestBuildUnitWithoutProjectStillOffersGroup(t *testing.T) {
	rows := []messageRow{{ID: 1, ChatID: "chat-b", ChatName: "临时群", GroupID: 9,
		SenderOpenID: "ou_x", SenderName: "王五", Content: "记一下", CreateTime: time.Now().UnixMilli(), RenderOK: true}}
	unit := buildUnit(rows, map[string]Subject{}, time.UTC)
	if len(unit.Subjects) != 1 || unit.Subjects[0].Type != "group" || unit.Subjects[0].ID != 9 {
		t.Fatalf("subjects = %+v, want only group 9", unit.Subjects)
	}
}

func TestWindowOptionsValidate(t *testing.T) {
	valid := WindowOptions{Gap: time.Minute, MaxMessages: 10, Location: time.UTC}
	if err := valid.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	for _, tt := range []struct {
		name string
		opts WindowOptions
	}{
		{"no gap", WindowOptions{MaxMessages: 10, Location: time.UTC}},
		{"no max", WindowOptions{Gap: time.Minute, Location: time.UTC}},
		{"no location", WindowOptions{Gap: time.Minute, MaxMessages: 10}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.validate(); err == nil {
				t.Fatal("validate() error = nil, want rejection")
			}
		})
	}
}
