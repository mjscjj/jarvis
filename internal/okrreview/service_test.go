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
)

type stubBoard struct {
	board okrworkspace.Board
	err   error
	calls int
}

func (s *stubBoard) Board(context.Context, string, string) (okrworkspace.Board, error) {
	s.calls++
	return s.board, s.err
}

type stubPrompts struct {
	text string
	err  error
}

func (s stubPrompts) Content(context.Context, string) (string, error) { return s.text, s.err }

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

func newTestService(t *testing.T, board BoardReader, script string) *Service {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "review-cli")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Options{
		Board: board, Prompts: stubPrompts{text: "评审提示词正文"},
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
	board := &stubBoard{board: previewBoard()}
	service := newTestService(t, board, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{
		Quarter: "2026-Q3", Week: "2026-W37", Kind: KindKR, KRID: "kr-1",
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
}

func TestPointReviewCarriesParentAsContextOnly(t *testing.T) {
	board := &stubBoard{board: previewBoard()}
	service := newTestService(t, board, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{
		Quarter: "2026-Q3", Week: "2026-W37", Kind: KindPoint, PointID: "p-1",
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
	board := &stubBoard{board: previewBoard()}
	service := newTestService(t, board, "#!/bin/sh\nexit 0\n")
	prompt, err := service.buildPrompt(t.Context(), Request{Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
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
	service := newTestService(t, &stubBoard{board: classic}, "#!/bin/sh\nexit 0\n")
	_, err := service.buildPrompt(t.Context(), Request{Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "okr_weekly_preview_v1") {
		t.Fatalf("buildPrompt() error = %v, want template mismatch", err)
	}

	ok := newTestService(t, &stubBoard{board: previewBoard()}, "#!/bin/sh\nexit 0\n")
	for name, req := range map[string]Request{
		"missing quarter": {Week: "2026-W37", Kind: KindAll},
		"missing week":    {Quarter: "2026-Q3", Kind: KindAll},
		"unknown kind":    {Quarter: "2026-Q3", Week: "2026-W37", Kind: "everything"},
		"kr without id":   {Quarter: "2026-Q3", Week: "2026-W37", Kind: KindKR},
		"unknown kr":      {Quarter: "2026-Q3", Week: "2026-W37", Kind: KindKR, KRID: "kr-404"},
		"unknown point":   {Quarter: "2026-Q3", Week: "2026-W37", Kind: KindPoint, PointID: "p-404"},
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
	service := newTestService(t, &stubBoard{board: previewBoard()}, script)
	content, err := service.Review(t.Context(), Request{Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
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
	failing := newTestService(t, &stubBoard{board: previewBoard()},
		"#!/bin/sh\nprintf 'quota exceeded\\n' >&2\nexit 1\n")
	_, err := failing.Review(t.Context(), Request{Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("Review() error = %v, want the CLI failure", err)
	}

	silent := newTestService(t, &stubBoard{board: previewBoard()},
		"#!/bin/sh\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"t\"}'\n")
	_, err = silent.Review(t.Context(), Request{Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "no review text") {
		t.Fatalf("Review() error = %v, want an empty-answer failure", err)
	}
}

func TestReviewSurfacesBoardFailure(t *testing.T) {
	service := newTestService(t, &stubBoard{err: fmt.Errorf("database is locked")}, "#!/bin/sh\nexit 0\n")
	_, err := service.Review(t.Context(), Request{Quarter: "2026-Q3", Week: "2026-W37", Kind: KindAll})
	if err == nil || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("Review() error = %v, want the board failure", err)
	}
}

func TestNewServiceRejectsUnusableAgentSettings(t *testing.T) {
	board := &stubBoard{board: previewBoard()}
	base := Options{
		Board: board, Prompts: stubPrompts{text: "x"},
		Bin: "sh", Model: "m", Sandbox: "read-only", ReasoningEffort: "high", Timeout: time.Second,
	}
	for name, mutate := range map[string]func(*Options){
		"board reader is required":  func(o *Options) { o.Board = nil },
		"prompt reader is required": func(o *Options) { o.Prompts = nil },
		"model is required":         func(o *Options) { o.Model = "" },
		"sandbox must be":           func(o *Options) { o.Sandbox = "none" },
		"timeout must be positive":  func(o *Options) { o.Timeout = 0 },
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
