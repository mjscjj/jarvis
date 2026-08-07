package taskcreate

import (
	"encoding/json"
	"fmt"
	"testing"

	"jarvis/internal/contextsnap"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeInputDefaultsTodoSourceID(t *testing.T) {
	todoID := uint64(42)
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "执行任务", ActionType: "investigate", Target: "目标",
		Background:    json.RawMessage(`{"snapshot_version":"v1"}`),
		SourcePayload: json.RawMessage(`{"instruction":"查清问题"}`),
		SourceType:    SourceTodo,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.SourceID == nil || *input.SourceID != todoID {
		t.Fatalf("source_id = %v, want %d", input.SourceID, todoID)
	}
}

// TestNormalizeInputKeepsSourcePayloadVerbatim pins that source semantics ride
// into the Task untouched, regardless of source-specific shape.
func TestNormalizeInputKeepsSourcePayloadVerbatim(t *testing.T) {
	todoID := uint64(42)
	clue := `{"desired_outcome":"产出会议结论与我的待办","semantics":"当前妙记无 view 权限"}`
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "会后处理", ActionType: "manual_followup", Target: "公会基建Agent 日会",
		Background:    json.RawMessage(`{"snapshot_version":"v1"}`),
		SourcePayload: json.RawMessage(clue),
		SourceType:    SourceTodo,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if string(input.SourcePayload) != clue {
		t.Fatalf("source_payload = %s, want %s", input.SourcePayload, clue)
	}
}

func TestNormalizeInputRejectsNullSourcePayload(t *testing.T) {
	_, err := normalizeInput(Input{
		Title: "任务", ActionType: "agent_task", Target: "输出结论",
		Background:    json.RawMessage(`{}`),
		SourcePayload: json.RawMessage(`null`),
		SourceType:    SourceManual,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted a null source_payload")
	}
}

func TestNormalizeInputRequiresScheduledOccurrence(t *testing.T) {
	sourceID := uint64(5)
	_, err := normalizeInput(Input{
		Title: "定时任务", ActionType: "agent_task", Target: "会议",
		Background:    json.RawMessage(`{"meeting_number":"123"}`),
		SourcePayload: json.RawMessage(`{"instruction":"加入会议"}`),
		SourceType:    SourceScheduledTask, SourceID: &sourceID,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted scheduled source without occurrence_key")
	}
}

func TestNormalizeInputAcceptsEmptyBackgroundObject(t *testing.T) {
	input, err := normalizeInput(Input{
		Title: "无额外背景任务", ActionType: "agent_task", Target: "输出结论",
		Background:    json.RawMessage(`{}`),
		SourcePayload: json.RawMessage(`{"instruction":"输出结论"}`),
		SourceType:    SourceManual,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if string(input.Background) != `{}` {
		t.Fatalf("background = %s, want {}", input.Background)
	}
}

func TestNormalizeInputRejectsMissingSourcePayload(t *testing.T) {
	_, err := normalizeInput(Input{
		Title: "缺少来源语义", ActionType: "agent_task", Target: "输出结论",
		Background: json.RawMessage(`{}`), SourceType: SourceManual,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted missing source_payload")
	}
}

func TestNormalizeInputAcceptsOpenSourcePayloadJSON(t *testing.T) {
	for name, payload := range map[string]string{
		"string":  `"直接调查并给出结论"`,
		"array":   `["调查","验证","汇报"]`,
		"number":  `3`,
		"boolean": `true`,
		"object":  `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			input, err := normalizeInput(Input{
				Title: "开放来源语义", ActionType: "agent_task", Target: "输出结论",
				Background: json.RawMessage(`{}`), SourcePayload: json.RawMessage(payload),
				SourceType: SourceManual,
			})
			if err != nil {
				t.Fatalf("normalizeInput() error = %v", err)
			}
			if string(input.SourcePayload) != payload {
				t.Fatalf("source_payload = %s, want %s", input.SourcePayload, payload)
			}
		})
	}
}

func TestNormalizeInputRejectsNullSourcePayloadJSON(t *testing.T) {
	for name, payload := range map[string]string{
		"null":  `null`,
		"blank": ``,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeInput(Input{
				Title: "空来源语义", ActionType: "agent_task", Target: "输出结论",
				Background: json.RawMessage(`{}`), SourcePayload: json.RawMessage(payload),
				SourceType: SourceManual,
			})
			if err == nil {
				t.Fatalf("normalizeInput() accepted source_payload %s", payload)
			}
		})
	}
}

func TestFactoryAssemblesCommonContextForManualScheduledAndProactiveSources(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE principal_profile (
			id INTEGER PRIMARY KEY AUTOINCREMENT, open_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
			department TEXT, title TEXT, background TEXT, preferences TEXT,
			leader_open_id TEXT, leader_name TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT, name TEXT NOT NULL, role TEXT NOT NULL,
			status TEXT NOT NULL, priority INTEGER NOT NULL, description TEXT, repos JSON,
			tech_stack JSON, key_decisions JSON, timeline JSON, notes TEXT,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE managed_resource (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, resource_type TEXT NOT NULL,
			url TEXT, description TEXT, person_id INTEGER, project_id INTEGER,
			link_principal INTEGER NOT NULL, is_active INTEGER NOT NULL,
			last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE fact (
			id INTEGER PRIMARY KEY AUTOINCREMENT, subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL,
			description TEXT NOT NULL, occurred_at DATETIME NOT NULL,
			source_kind TEXT, source_id INTEGER, created_at DATETIME
		)`,
		`CREATE TABLE feishu_group (
			id INTEGER PRIMARY KEY AUTOINCREMENT, chat_id TEXT NOT NULL UNIQUE,
			name TEXT, description TEXT, background_note TEXT, project_id INTEGER,
			is_key_group INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE todo (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL,
			action_type TEXT NOT NULL, status TEXT NOT NULL, group_id INTEGER,
			project_id INTEGER, last_evidence_at DATETIME
		)`,
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY AUTOINCREMENT, todo_id INTEGER, title TEXT NOT NULL,
			status TEXT NOT NULL, summary TEXT, project_id INTEGER,
			last_progress_at DATETIME, created_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create sqlite table: %v", err)
		}
	}
	project := domain.Project{
		Name: "Jarvis", Role: "owner", Status: "active", Priority: 1,
		Repos: datatypes.JSON(`[{"local_path":"/tmp/first-repo"},{"local_path":"/tmp/second-repo"}]`),
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.Exec(`INSERT INTO feishu_group(id, chat_id, name, project_id, is_key_group)
		VALUES (7, 'oc_scheduled', 'Jarvis 群', ?, 1)`, project.ID).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&domain.PrincipalProfile{OpenID: "ou_me", Name: "我"}).Error; err != nil {
		t.Fatalf("create principal: %v", err)
	}
	assembler, err := contextsnap.NewAssembler(db, "ou_me")
	if err != nil {
		t.Fatalf("NewAssembler() error = %v", err)
	}
	factory, err := NewFactory(db, assembler)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}

	manual, err := factory.assembleBackground(t.Context(), Input{
		SourceType: SourceManual, ProjectID: &project.ID,
		Background: json.RawMessage(`{"chat_id":"oc_scheduled","note":"手工任务背景"}`),
	})
	if err != nil {
		t.Fatalf("assemble manual background: %v", err)
	}
	manualSnapshot, err := contextsnap.Decode(manual.Background)
	if err != nil {
		t.Fatalf("decode manual background: %v", err)
	}
	if manualSnapshot.Principal == nil || manualSnapshot.Project == nil || manualSnapshot.Group != nil || string(manualSnapshot.RequestContext) != `{"chat_id":"oc_scheduled","note":"手工任务背景"}` {
		t.Fatalf("manual snapshot = %#v", manualSnapshot)
	}
	if manual.RepoPath != nil {
		t.Fatalf("manual repo_path = %v, want nil without an explicit selection", *manual.RepoPath)
	}

	scheduled, err := factory.assembleBackground(t.Context(), Input{
		SourceType: SourceScheduledTask,
		Background: json.RawMessage(`{"chat_id":"oc_scheduled","note":"定时任务背景"}`),
	})
	if err != nil {
		t.Fatalf("assemble scheduled background: %v", err)
	}
	scheduledSnapshot, err := contextsnap.Decode(scheduled.Background)
	if err != nil {
		t.Fatalf("decode scheduled background: %v", err)
	}
	if scheduled.ProjectID == nil || *scheduled.ProjectID != project.ID || scheduledSnapshot.Project == nil || scheduledSnapshot.Group == nil || scheduledSnapshot.Group.ChatID != "oc_scheduled" {
		t.Fatalf("scheduled input/snapshot = %#v / %#v", scheduled, scheduledSnapshot)
	}
	if scheduled.RepoPath != nil {
		t.Fatalf("scheduled repo_path = %v, want nil without an explicit selection", *scheduled.RepoPath)
	}

	proactive, err := factory.assembleBackground(t.Context(), Input{
		SourceType: SourceProactive, ProjectID: &project.ID,
		Background: json.RawMessage(`{"why_now":"发现真实阻塞"}`),
	})
	if err != nil {
		t.Fatalf("assemble proactive background: %v", err)
	}
	proactiveSnapshot, err := contextsnap.Decode(proactive.Background)
	if err != nil {
		t.Fatalf("decode proactive background: %v", err)
	}
	if proactiveSnapshot.Principal == nil || proactiveSnapshot.Project == nil || string(proactiveSnapshot.RequestContext) != `{"why_now":"发现真实阻塞"}` {
		t.Fatalf("proactive snapshot = %#v", proactiveSnapshot)
	}
	if proactive.RepoPath != nil {
		t.Fatalf("proactive repo_path = %v, want nil without an explicit selection", *proactive.RepoPath)
	}

	explicitRepo := "/tmp/explicit-repo"
	explicit, err := factory.assembleBackground(t.Context(), Input{
		SourceType: SourceManual, ProjectID: &project.ID, RepoPath: &explicitRepo,
		Background: json.RawMessage(`{"note":"明确指定仓库"}`),
	})
	if err != nil {
		t.Fatalf("assemble explicit repo background: %v", err)
	}
	if explicit.RepoPath == nil || *explicit.RepoPath != explicitRepo {
		t.Fatalf("explicit repo_path = %v, want %q", explicit.RepoPath, explicitRepo)
	}
}
