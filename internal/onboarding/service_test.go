package onboarding

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextpack"
	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type streamingRunnerStub struct {
	emitted chan struct{}
	release chan struct{}
}

type onboardingRunnerStub struct{}

func (onboardingRunnerStub) Run(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
	if strings.Join(args, " ") == "auth status --json --verify" {
		return []byte(`{
			"appId":"cli_test",
			"identities":{
				"bot":{"status":"ready","verified":true,"openId":"ou_bot","appName":"Jarvis Bot"},
				"user":{"status":"ready","verified":true,"openId":"ou_principal","userName":"Principal","tokenStatus":"valid"}
			}
		}`), nil
	}
	return nil, nil
}

type readyNotifierStub struct{}

func (readyNotifierStub) TaskReady(context.Context, uint64, int32) error {
	return nil
}

func (streamingRunnerStub) Run(context.Context, string, []string, string) ([]byte, error) {
	return nil, nil
}

func (runner streamingRunnerStub) RunStreaming(
	_ context.Context,
	_ string,
	_ []string,
	_ string,
	onOutput func([]byte),
) ([]byte, error) {
	output := []byte("Open https://example.test/device and enter ABCD-EFGH\n")
	onOutput(output)
	close(runner.emitted)
	<-runner.release
	return output, nil
}

func TestAgentLoginPublishesDeviceFlowBeforeCommandCompletes(t *testing.T) {
	runner := streamingRunnerStub{
		emitted: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := &Service{
		options: Options{AgentCLIBin: "traex"},
		runner:  runner,
		flows: map[string]*Flow{
			"flow-1": {ID: "flow-1", Status: flowPending},
		},
	}

	done := make(chan struct{})
	go func() {
		service.completeAgentLogin("flow-1")
		close(done)
	}()

	select {
	case <-runner.emitted:
	case <-time.After(time.Second):
		t.Fatal("device flow output was not published")
	}
	flow, err := service.Flow("flow-1")
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != flowPending {
		t.Fatalf("status = %q, want %q", flow.Status, flowPending)
	}
	if !strings.Contains(flow.Output, "ABCD-EFGH") {
		t.Fatalf("pending output = %q, want device code", flow.Output)
	}

	close(runner.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent login did not finish")
	}
	flow, err = service.Flow("flow-1")
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != flowSuccess {
		t.Fatalf("status = %q, want %q", flow.Status, flowSuccess)
	}
}

func TestWorldModelBootstrapSeedsPrincipalAndWaitsForTaskCompletion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "onboarding.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PrincipalProfile{}, &domain.Task{}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options: Options{DB: db, StateRoot: t.TempDir()},
	}
	identity := IdentityStatus{OpenID: "ou_principal", Name: "Principal"}
	if err := service.ensurePrincipalProfile(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	var profile domain.PrincipalProfile
	if err := db.Where("open_id = ?", identity.OpenID).Take(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if profile.Name != identity.Name {
		t.Fatalf("profile name = %q, want %q", profile.Name, identity.Name)
	}
	if err := service.requireWorldModel(); err != nil {
		t.Fatal(err)
	}
	if ready, err := service.worldModelReady(t.Context(), identity.OpenID); err != nil || ready {
		t.Fatalf("worldModelReady before Task = %t, %v", ready, err)
	}
	task := domain.Task{
		Title: "建立初始世界模型", ActionType: worldModelActionType, Target: "bootstrap",
		Status: "pending", SourceType: "manual", SourcePayload: []byte(`{}`),
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if ready, err := service.worldModelReady(t.Context(), identity.OpenID); err != nil || ready {
		t.Fatalf("worldModelReady for pending Task = %t, %v", ready, err)
	}
	if err := db.Model(&task).Update("status", "done").Error; err != nil {
		t.Fatal(err)
	}
	if ready, err := service.worldModelReady(t.Context(), identity.OpenID); err != nil || !ready {
		t.Fatalf("worldModelReady for completed Task = %t, %v", ready, err)
	}
}

func TestEnsurePrincipalProfilePreservesExistingProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "onboarding.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PrincipalProfile{}); err != nil {
		t.Fatal(err)
	}
	title := "Engineer"
	existing := domain.PrincipalProfile{OpenID: "ou_principal", Name: "Saved Name", Title: &title}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	service := &Service{options: Options{DB: db}}
	if err := service.ensurePrincipalProfile(t.Context(), IdentityStatus{
		OpenID: "ou_principal",
		Name:   "Lark Name",
	}); err != nil {
		t.Fatal(err)
	}
	var profile domain.PrincipalProfile
	if err := db.Where("open_id = ?", existing.OpenID).Take(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if profile.Name != existing.Name || profile.Title == nil || *profile.Title != title {
		t.Fatalf("existing profile was overwritten: %+v", profile)
	}
}

func TestBootstrapWorldModelCreatesPrincipalBeforeManualTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "onboarding.db")), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	models := append(domain.CoreModels(), &domain.TaskEvent{})
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	assembler, err := contextsnap.NewAssembler(db, "ou_principal")
	if err != nil {
		t.Fatal(err)
	}
	factory, err := taskcreate.NewFactory(db, assembler)
	if err != nil {
		t.Fatal(err)
	}
	submitter, err := taskcreate.NewSubmitter(factory)
	if err != nil {
		t.Fatal(err)
	}
	if err := submitter.SetNotifier(readyNotifierStub{}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options: Options{
			DB: db, StateRoot: t.TempDir(), RuntimeRoot: filepath.Join(t.TempDir(), "Jarvis runtime"), LarkCLIBin: "lark-cli",
			TaskSubmitter: submitter,
		},
		runner: onboardingRunnerStub{},
		flows:  make(map[string]*Flow),
	}

	task, err := service.BootstrapWorldModel(t.Context())
	if err != nil {
		t.Fatalf("BootstrapWorldModel() error = %v", err)
	}
	if task.ActionType != worldModelActionType || task.Status != "pending" {
		t.Fatalf("task = %+v", task)
	}
	// Use the same projection as the execution prompt, not the full packet:
	// the Skill and recovery instruction must be visible without another read.
	overview, err := contextpack.Read(task.SourcePayload, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		Source struct {
			RuntimeRoot    string `json:"runtime_root"`
			SkillPath      string `json:"skill_path"`
			Instruction    string `json:"instruction"`
			PreviousTaskID uint64 `json:"previous_task_id"`
		} `json:"source"`
	}
	if err := json.Unmarshal(overview, &projected); err != nil {
		t.Fatal(err)
	}
	if projected.Source.RuntimeRoot != service.options.RuntimeRoot ||
		projected.Source.SkillPath != filepath.Join(service.options.RuntimeRoot, ".agents", "skills", "bootstrap-jarvis-world-model", "SKILL.md") ||
		projected.Source.Instruction == "" || projected.Source.PreviousTaskID != 0 {
		t.Fatalf("initial request missing from prompt overview: %s", overview)
	}
	background, err := contextpack.Read(task.SourcePayload, "request_context", "")
	if err != nil || strings.Contains(string(background), "instruction") {
		t.Fatalf("instruction still owned by deferred background: %s, %v", background, err)
	}
	var profile domain.PrincipalProfile
	if err := db.Where("open_id = ?", "ou_principal").Take(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Principal" {
		t.Fatalf("profile name = %q", profile.Name)
	}
	// Reloading only discovers the existing task, including a failed attempt.
	// Retrying is an explicit action on that same Task through /rerun.
	for _, state := range []string{"pending", "executing", "waiting", "needs_human", "failed", "observing", "done"} {
		if err := db.Model(task).Update("status", state).Error; err != nil {
			t.Fatal(err)
		}
		again, err := service.BootstrapWorldModel(t.Context())
		if err != nil || again.ID != task.ID || again.Status != state || string(again.SourcePayload) != string(task.SourcePayload) {
			t.Fatalf("reuse %s: %v, %v", state, again, err)
		}
	}
	var count int64
	if err := db.Model(&domain.Task{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("created another initialization task: count=%d, error=%v", count, err)
	}
}

func (r onboardingRunnerStub) RunJSON(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
	return r.Run(ctx, bin, args, input)
}

func (r streamingRunnerStub) RunJSON(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
	return r.Run(ctx, bin, args, input)
}
