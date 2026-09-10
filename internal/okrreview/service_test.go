package okrreview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/okrworkspace"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/textstore"
)

type stubWorkspace struct {
	board      okrworkspace.Board
	plan       okrworkspace.PlanView
	boardErr   error
	planErr    error
	boardCalls int
	planCalls  int
}

func (s *stubWorkspace) Board(context.Context, string, string) (okrworkspace.Board, error) {
	s.boardCalls++
	return s.board, s.boardErr
}

func (s *stubWorkspace) GetPlan(context.Context, string) (okrworkspace.PlanView, error) {
	s.planCalls++
	return s.plan, s.planErr
}

type stubPrompts struct {
	text    string
	err     error
	lastKey string
}

func (s *stubPrompts) Content(_ context.Context, key string) (string, error) {
	s.lastKey = key
	return s.text, s.err
}

func previewBoard() okrworkspace.Board {
	return okrworkspace.Board{
		Quarter:     "2026-Q3",
		Week:        "2026-W37",
		TemplateKey: domain.WeekTemplateOKRPreview,
		Objectives: []okrworkspace.ObjectiveView{{
			ID: "o-1", Title: "O1：B端市场",
			KRs: []okrworkspace.KRView{{
				ID: "kr-1", Title: "KR1：线索质量", OwnerName: "Anqi Feng",
				MetricNote: "8 月 25 日口径",
				Metrics: []okrworkspace.MetricView{
					{ID: "m-1", Text: "CPL $5.93", Light: domain.LightGreen},
					{ID: "m-2", Text: "HVR 16%", Light: domain.LightYellow},
				},
				Owners: []okrworkspace.OwnerView{{OpenID: "ou_1", Name: "Anqi Feng"}},
				Tags:   []okrworkspace.TagView{{Type: "custom", Value: "双周报"}},
				Points: []okrworkspace.PointView{{
					ID: "p-1", Kind: domain.PointKindStrategy, Title: "线索线上化",
					Entries: []okrworkspace.ProgressView{
						{ID: "e-1", Status: domain.StatusInProgress, Text: "已接入 70%"},
					},
					PreviousEntries: []okrworkspace.ProgressView{
						{ID: "e-0", Status: domain.StatusInProgress, Text: "上周 60%"},
					},
				}, {
					ID: "p-2", Kind: domain.PointKindProduct, Title: "结算效率",
				}},
			}},
		}},
	}
}

func previewPlan() okrworkspace.PlanView {
	return okrworkspace.PlanView{
		ID: "plan-1", Quarter: "2026-Q4", Title: "Q4 增长计划",
		Objectives: []okrworkspace.PlanObjectiveView{{
			ID: "plan-o-1", Title: "O1：建立增长飞轮",
			KRs: []okrworkspace.PlanKRView{{
				ID: "plan-kr-1", Title: "KR1：提升高质量线索", MetricNote: "以季度末数据为准",
				Owners:  []okrworkspace.OwnerView{{OpenID: "ou_1", Name: "Anqi Feng"}},
				Metrics: []okrworkspace.MetricView{{ID: "plan-m-1", Text: "HVR 从 12% 提升至 16%"}},
				Points: []okrworkspace.PlanPointView{{
					ID: "plan-p-1", Kind: domain.PointKindStrategy, Title: "完成线索分层策略",
				}},
			}},
		}},
	}
}

func newTestService(t *testing.T, workspace WorkspaceReader, script string) *Service {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "review-cli")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Options{
		Workspace: workspace, Prompts: &stubPrompts{text: "评审提示词正文"},
		Bin: bin, Model: "DeepSeek-V4-Pro", Sandbox: "workspace-write", ReasoningEffort: "high",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// The whole point of pre-loading the board is that a KR review arrives with its
// own content already in the prompt, so the agent does not have to fetch it.
func TestPromptCarriesTheReviewedKRWithoutTheRestOfTheBoard(t *testing.T) {
	workspace := &stubWorkspace{board: previewBoard()}
	service := newTestService(t, workspace, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{
		ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindKR, KRID: "kr-1",
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{
		"评审提示词正文",
		"BEGIN_AVAILABLE_TOOLS",
		"okr-module-tools",
		"BEGIN_REVIEW_TARGET",
		"KR1：线索质量",
		"CPL $5.93（green）",
		"线索线上化",
		"[in_progress] 已接入 70%",
		"上周 60%",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, prompt)
		}
	}
	if got := service.prompts.(*stubPrompts).lastKey; got != textstore.OKRAgentProgressReviewKey {
		t.Fatalf("progress review prompt key = %q", got)
	}
}

func TestPlanReviewReadsThePlanAndItsOwnPrompt(t *testing.T) {
	workspace := &stubWorkspace{plan: previewPlan()}
	service := newTestService(t, workspace, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{
		ReviewType: ReviewTypePlan, Quarter: "2026-Q4", PlanID: "plan-1", Kind: KindAll,
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{"类型：OKR Plan 评审", "Q4 增长计划", "O1：建立增长飞轮", "KR1：提升高质量线索", "HVR 从 12% 提升至 16%", "完成线索分层策略"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("plan prompt is missing %q:\n%s", want, prompt)
		}
	}
	if workspace.planCalls != 1 || workspace.boardCalls != 0 {
		t.Fatalf("plan review calls: plan=%d board=%d", workspace.planCalls, workspace.boardCalls)
	}
	if got := service.prompts.(*stubPrompts).lastKey; got != textstore.OKRAgentPlanReviewKey {
		t.Fatalf("plan review prompt key = %q", got)
	}
}

func TestObjectiveReviewCarriesOnlyTheSelectedObjective(t *testing.T) {
	board := previewBoard()
	board.Objectives = append(board.Objectives, okrworkspace.ObjectiveView{
		ID: "o-2", Title: "O2：不应进入本次评审",
		KRs: []okrworkspace.KRView{{ID: "kr-2", Title: "KR2：范围外"}},
	})
	service := newTestService(t, &stubWorkspace{board: board}, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{
		ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindObjective, ObjectiveID: "o-1",
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "范围：单个 O（id=o-1）") || !strings.Contains(prompt, "O1：B端市场") {
		t.Fatalf("objective review lost its selected O:\n%s", prompt)
	}
	if strings.Contains(prompt, "O2：不应进入本次评审") {
		t.Fatalf("objective review leaked another O:\n%s", prompt)
	}
}

func TestPointReviewCarriesParentAsContextOnly(t *testing.T) {
	workspace := &stubWorkspace{board: previewBoard()}
	service := newTestService(t, workspace, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{
		ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindPoint, PointID: "p-1",
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "所属一级 KR：KR1：线索质量") {
		t.Fatalf("point review lost its parent context:\n%s", prompt)
	}
	// The sibling point is out of scope and must not be reviewed alongside it.
	if strings.Contains(prompt, "结算效率") {
		t.Fatalf("point review leaked a sibling point:\n%s", prompt)
	}
}

func TestAllScopeCarriesEveryObjective(t *testing.T) {
	workspace := &stubWorkspace{board: previewBoard()}
	service := newTestService(t, workspace, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{"O1：B端市场", "KR1：线索质量", "线索线上化", "结算效率"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("all-scope prompt is missing %q", want)
		}
	}
}

// fail-fast: a classic weekly report week is not an OKR Preview, and reviewing
// it under Preview rules would silently judge it by the wrong standard.
func TestReviewRejectsNonPreviewWeekAndUnknownTargets(t *testing.T) {
	classic := previewBoard()
	classic.TemplateKey = domain.WeekTemplateClassic
	service := newTestService(t, &stubWorkspace{board: classic}, "#!/bin/sh\nexit 0\n")
	_, err := service.buildPrompt(t.Context(), Request{ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "okr_weekly_preview_v1") {
		t.Fatalf("buildPrompt() error = %v, want template mismatch", err)
	}

	ok := newTestService(t, &stubWorkspace{board: previewBoard()}, "#!/bin/sh\nexit 0\n")
	for name, req := range map[string]Request{
		"missing review type":  {},
		"missing quarter":      {ReviewType: ReviewTypeProgress, Week: "2026-W37", Kind: KindAll},
		"missing week":         {ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Kind: KindAll},
		"unknown kind":         {ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: "everything"},
		"objective without id": {ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindObjective},
		"unknown objective":    {ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindObjective, ObjectiveID: "o-404"},
		"kr without id":        {ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindKR},
		"unknown kr":           {ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindKR, KRID: "kr-404"},
		"unknown point":        {ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindPoint, PointID: "p-404"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ok.buildPrompt(t.Context(), req); err == nil {
				t.Fatalf("buildPrompt(%+v) accepted an unusable request", req)
			}
		})
	}
}

func TestReviewReturnsOnlyWhatTheAgentSaid(t *testing.T) {
	// A real run interleaves reasoning and command output with the answer.
	script := `#!/bin/sh
cat <<'JSONL'
{"type":"thread.started","thread_id":"t-1"}
{"type":"item.completed","item":{"type":"reasoning","text":"内部思考不应该出现在报告里"}}
{"type":"item.completed","item":{"type":"command_execution","command":"okr-module-tools scope","aggregated_output":"{}"}}
{"type":"item.completed","item":{"type":"agent_message","text":"## 总体判断\n\n结论先行。"}}
{"type":"item.completed","item":{"type":"agent_message","text":"## 建议\n\n补充业务影响。"}}
{"type":"turn.completed","usage":{}}
JSONL
`
	service := newTestService(t, &stubWorkspace{board: previewBoard()}, script)
	content, err := service.Review(t.Context(), Request{ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if content != "## 总体判断\n\n结论先行。\n\n## 建议\n\n补充业务影响。" {
		t.Fatalf("Review() = %q", content)
	}
	if strings.Contains(content, "内部思考") || strings.Contains(content, "okr-module-tools scope") {
		t.Fatalf("Review() leaked the agent's working notes: %q", content)
	}
}

// fail-fast: a failed CLI must surface its own error, not an empty review.
func TestReviewSurfacesAgentFailureAndSilence(t *testing.T) {
	failing := newTestService(t, &stubWorkspace{board: previewBoard()},
		"#!/bin/sh\nprintf 'quota exceeded\\n' >&2\nexit 1\n")
	_, err := failing.Review(t.Context(), Request{ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("Review() error = %v, want the CLI failure", err)
	}

	silent := newTestService(t, &stubWorkspace{board: previewBoard()},
		"#!/bin/sh\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"t\"}'\n")
	_, err = silent.Review(t.Context(), Request{ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "no review text") {
		t.Fatalf("Review() error = %v, want an empty-answer failure", err)
	}
}

func TestReviewSurfacesBoardFailure(t *testing.T) {
	service := newTestService(t, &stubWorkspace{boardErr: fmt.Errorf("database is locked")}, "#!/bin/sh\nexit 0\n")
	_, err := service.Review(t.Context(), Request{ReviewType: ReviewTypeProgress, Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("Review() error = %v, want the board failure", err)
	}
}

func TestNewServiceRejectsUnusableAgentSettings(t *testing.T) {
	workspace := &stubWorkspace{board: previewBoard()}
	base := Options{
		Workspace: workspace, Prompts: &stubPrompts{text: "x"},
		Bin: "sh", Model: "m", Sandbox: "read-only", ReasoningEffort: "high", Timeout: time.Second,
	}
	for name, mutate := range map[string]func(*Options){
		"workspace reader is required": func(o *Options) { o.Workspace = nil },
		"prompt reader is required":    func(o *Options) { o.Prompts = nil },
		"model is required":            func(o *Options) { o.Model = "" },
		"sandbox must be":              func(o *Options) { o.Sandbox = "none" },
		"timeout must be positive":     func(o *Options) { o.Timeout = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			opts := base
			mutate(&opts)
			_, err := NewService(opts)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("NewService() error = %v, want containing %q", err, name)
			}
		})
	}
}
