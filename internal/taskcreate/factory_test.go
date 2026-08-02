package taskcreate

import (
	"encoding/json"
	"fmt"
	"testing"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeInputDefaultsTodoSourceID(t *testing.T) {
	todoID := uint64(42)
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "执行任务", ActionType: "investigate", Target: "目标",
		Background:  json.RawMessage(`{"snapshot_version":"v1"}`),
		Plan:        json.RawMessage(`{"instruction":"查清问题"}`),
		ConfirmedBy: "user", SourceType: SourceTodo, ExecutionMode: ExecutionModeStandard,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.SourceID == nil || *input.SourceID != todoID {
		t.Fatalf("source_id = %v, want %d", input.SourceID, todoID)
	}
}

// TestNormalizeInputKeepsSourceClueVerbatim pins that M3's original clue rides
// into the Task untouched: M5 reads it to recover the real goal when its judgment
// direction only covers an intermediate step.
func TestNormalizeInputKeepsSourceClueVerbatim(t *testing.T) {
	todoID := uint64(42)
	clue := `{"desired_outcome":"产出会议结论与我的待办","semantics":"当前妙记无 view 权限"}`
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "会后处理", ActionType: "manual_followup", Target: "公会基建Agent 日会",
		Background:  json.RawMessage(`{"snapshot_version":"v1"}`),
		SourceClue:  json.RawMessage(clue),
		Plan:        json.RawMessage(`{"instruction":"先申请权限"}`),
		ConfirmedBy: "user", SourceType: SourceTodo, ExecutionMode: ExecutionModeStandard,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if string(input.SourceClue) != clue {
		t.Fatalf("source_clue = %s, want %s", input.SourceClue, clue)
	}
}

func TestNormalizeInputRejectsNullSourceClue(t *testing.T) {
	_, err := normalizeInput(Input{
		Title: "任务", ActionType: "agent_task", Target: "输出结论",
		Background:  json.RawMessage(`{}`),
		SourceClue:  json.RawMessage(`null`),
		Plan:        json.RawMessage(`{"instruction":"输出结论"}`),
		ConfirmedBy: "user", SourceType: SourceManual, ExecutionMode: ExecutionModeDirect,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted a null source_clue")
	}
}

func TestNormalizeInputRequiresScheduledOccurrence(t *testing.T) {
	sourceID := uint64(5)
	_, err := normalizeInput(Input{
		Title: "定时任务", ActionType: "agent_task", Target: "会议",
		Background:  json.RawMessage(`{"meeting_number":"123"}`),
		Plan:        json.RawMessage(`{"instruction":"加入会议"}`),
		ConfirmedBy: "scheduled_task", SourceType: SourceScheduledTask, SourceID: &sourceID,
		ExecutionMode: ExecutionModeDirect,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted scheduled source without occurrence_key")
	}
}

func TestNormalizeInputAcceptsEmptyBackgroundObject(t *testing.T) {
	input, err := normalizeInput(Input{
		Title: "无额外背景任务", ActionType: "agent_task", Target: "输出结论",
		Background:  json.RawMessage(`{}`),
		Plan:        json.RawMessage(`{"instruction":"输出结论"}`),
		ConfirmedBy: "user", SourceType: SourceManual, ExecutionMode: ExecutionModeDirect,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if string(input.Background) != `{}` {
		t.Fatalf("background = %s, want {}", input.Background)
	}
}

func TestNormalizeInputAcceptsMissingPlan(t *testing.T) {
	todoID := uint64(42)
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "无上游计划任务", ActionType: "agent_task", Target: "输出结论",
		Background: json.RawMessage(`{}`), ConfirmedBy: "materializer",
		SourceType: SourceTodo, ExecutionMode: ExecutionModeStandard,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.Plan != nil {
		t.Fatalf("plan = %s, want nil", input.Plan)
	}
}

func TestNormalizeInputRejectsEmptyPlanObject(t *testing.T) {
	_, err := normalizeInput(Input{
		Title: "空计划任务", ActionType: "agent_task", Target: "输出结论",
		Background:  json.RawMessage(`{}`),
		Plan:        json.RawMessage(`{}`),
		ConfirmedBy: "user", SourceType: SourceManual, ExecutionMode: ExecutionModeDirect,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted empty plan")
	}
}

func TestNormalizeInputAcceptsOpenPlanJSON(t *testing.T) {
	for name, plan := range map[string]string{
		"string":  `"直接调查并给出结论"`,
		"array":   `["调查","验证","汇报"]`,
		"number":  `3`,
		"boolean": `true`,
	} {
		t.Run(name, func(t *testing.T) {
			input, err := normalizeInput(Input{
				Title: "开放计划", ActionType: "agent_task", Target: "输出结论",
				Background:  json.RawMessage(`{}`),
				Plan:        json.RawMessage(plan),
				ConfirmedBy: "user", SourceType: SourceManual, ExecutionMode: ExecutionModeDirect,
			})
			if err != nil {
				t.Fatalf("normalizeInput() error = %v", err)
			}
			if string(input.Plan) != plan {
				t.Fatalf("plan = %s, want %s", input.Plan, plan)
			}
		})
	}
}

func TestNormalizeInputRejectsEmptyOpenPlanJSON(t *testing.T) {
	for name, plan := range map[string]string{
		"null":         `null`,
		"blank string": `"  "`,
		"empty array":  `[]`,
		"empty object": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeInput(Input{
				Title: "空计划", ActionType: "agent_task", Target: "输出结论",
				Background:  json.RawMessage(`{}`),
				Plan:        json.RawMessage(plan),
				ConfirmedBy: "user", SourceType: SourceManual, ExecutionMode: ExecutionModeDirect,
			})
			if err == nil {
				t.Fatalf("normalizeInput() accepted plan %s", plan)
			}
		})
	}
}

func TestFactoryAssemblesCommonContextForManualAndScheduledSources(t *testing.T) {
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
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE fact (
			id INTEGER PRIMARY KEY AUTOINCREMENT, subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL,
			description TEXT NOT NULL, occurred_at DATETIME NOT NULL,
			source_kind TEXT, source_id INTEGER, created_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create sqlite table: %v", err)
		}
	}
	project := domain.Project{Name: "Jarvis", Role: "owner", Status: "active", Priority: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
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
		Background: json.RawMessage(`{"note":"手工任务背景"}`),
	})
	if err != nil {
		t.Fatalf("assemble manual background: %v", err)
	}
	manualSnapshot, err := contextsnap.Decode(manual.Background)
	if err != nil {
		t.Fatalf("decode manual background: %v", err)
	}
	if manualSnapshot.Principal == nil || manualSnapshot.Project == nil || string(manualSnapshot.RequestContext) != `{"note":"手工任务背景"}` {
		t.Fatalf("manual snapshot = %#v", manualSnapshot)
	}

	scheduled, err := factory.assembleBackground(t.Context(), Input{
		SourceType: SourceScheduledTask,
		Background: json.RawMessage(fmt.Sprintf(`{"project":{"id":%d},"note":"定时任务背景"}`, project.ID)),
	})
	if err != nil {
		t.Fatalf("assemble scheduled background: %v", err)
	}
	scheduledSnapshot, err := contextsnap.Decode(scheduled.Background)
	if err != nil {
		t.Fatalf("decode scheduled background: %v", err)
	}
	if scheduled.ProjectID == nil || *scheduled.ProjectID != project.ID || scheduledSnapshot.Project == nil {
		t.Fatalf("scheduled input/snapshot = %#v / %#v", scheduled, scheduledSnapshot)
	}
}
