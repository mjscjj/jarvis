package dailydigest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

const (
	personMessageContentCap = 300
	personMessageLimit      = 300
	personInternalLimit     = 300
	jarvisSeedMarker        = "<!-- jarvis-seed-complete -->"
)

var personReportHeadings = []string{
	"## 今日数据",
	"## 今天的会议",
	"## 消息与协作",
	"## 项目与工作进展",
	"## 已完成事项",
	"## 待讨论事项",
	"## 后续计划",
	"## 关联、洞察与其他发现",
	"## 数据说明",
}

// SummaryRunner 是个人/群总结共用的 Codex 能力。个人日报必须从 Jarvis
// workspace 运行，以便加载项目 Skill、并行调查并持久化当天 Markdown。
type SummaryRunner interface {
	RunTextSandbox(ctx context.Context, prompt, sandbox string) (string, error)
	RunTextSandboxAt(ctx context.Context, prompt, sandbox, workspaceRoot string) (string, error)
}

type personGenerator struct {
	db              *gorm.DB
	runner          SummaryRunner
	location        *time.Location
	principalOpenID string
	gitAuthor       string
	repoRoot        string
	workspaceRoot   string
	skillDir        string
	skillText       string
	sandbox         string
}

// personBaseline 只保存 Jarvis 能确定查询的事实。当天进展读取 append-only 事件，
// 不把所有历史未完成 Task/Todo 混成“今天发生的事”。
type personBaseline struct {
	Messages        []baselineMessage
	TodoEvents      []baselineTodoEvent
	TaskEvents      []baselineTaskEvent
	ExecutionRuns   []baselineExecutionRun
	Facts           []baselineFact
	TruncatedScopes []string
	IntegrityGaps   []string
}

type baselineMessage struct {
	MessageID    string
	OccurredAt   string
	Conversation string
	Project      string
	Content      string
}

type baselineTodoEvent struct {
	EventID            uint64
	TodoID             uint64
	OccurredAt         string
	FromStatus         string
	ToStatus           string
	Actor              string
	Title              string
	ProjectBinding     string
	CommitmentStrength string
	LeaderAssigned     bool
	DueAt              string
	SourceQuote        string
	Context            string
	ContextSnapshot    string
	Resolution         string
	Detail             string
}

type baselineTaskEvent struct {
	EventID    uint64
	TaskID     uint64
	OccurredAt string
	EventType  string
	FromStatus string
	ToStatus   string
	ActorType  string
	ActorRef   string
	Title      string
	Project    string
	Background string
	Detail     string
}

type baselineExecutionRun struct {
	RunID           uint64
	TaskID          uint64
	OccurredAt      string
	StartedAt       string
	FinishedAt      string
	Status          string
	ActionType      string
	Title           string
	Project         string
	Summary         string
	ErrorDetail     string
	Commit          string
	MergeRequestURL string
	CodexSessionID  string
}

type baselineFact struct {
	FactID      uint64
	SubjectType string
	SubjectID   uint64
	OccurredAt  string
	Subject     string
	Description string
}

type factSubjectKey struct {
	Type string
	ID   uint64
}

type personGenerateResult struct {
	Summary     string
	SourceCount int
	Coverage    SourceCoverage
	CutoffAt    time.Time
	ReportPath  string
}

type reportSnapshot struct {
	exists  bool
	modTime time.Time
	hash    [sha256.Size]byte
}

// Generate 只保留程序必须保证的边界：自然日、Jarvis 确定性证据、工作目录、
// canonical 报告的新鲜度和九个导航章节。顶层 Agent 只做一轮并行外部取证，
// 随后直接归并成稿。
func (g *personGenerator) Generate(
	ctx context.Context,
	date string,
	dayStart, dayEnd, cutoffAt time.Time,
) (*personGenerateResult, error) {
	windowEnd := dayEnd
	if cutoffAt.Before(windowEnd) {
		windowEnd = cutoffAt
	}
	if !windowEnd.After(dayStart) {
		return nil, fmt.Errorf("personal digest window end %s must be after start %s", windowEnd, dayStart)
	}

	baseline, err := g.loadBaseline(ctx, dayStart, windowEnd)
	if err != nil {
		return nil, err
	}
	dayDir, err := g.initializeDayWorkspace(ctx, date)
	if err != nil {
		return nil, err
	}
	jarvisEvidencePath, err := g.writeJarvisEvidence(
		dayDir,
		date,
		dayStart,
		windowEnd,
		cutoffAt,
		baseline,
	)
	if err != nil {
		return nil, err
	}

	reportPath := filepath.Join(dayDir, "99-report.md")
	before, err := snapshotReport(reportPath)
	if err != nil {
		return nil, err
	}
	runID := "daily-report-" + time.Now().In(g.location).Format("20060102T150405.000000000")
	prompt := g.buildSkillPrompt(
		runID,
		date,
		dayStart,
		windowEnd,
		cutoffAt,
		dayDir,
		jarvisEvidencePath,
		before.exists,
	)
	if _, err := g.runner.RunTextSandboxAt(ctx, prompt, g.sandbox, g.workspaceRoot); err != nil {
		return nil, fmt.Errorf("codex personal daily report for %s: %w", date, err)
	}

	report, err := readFreshPersonReport(reportPath, date, before)
	if err != nil {
		return nil, err
	}
	sourceCount, coverage, err := inspectPersonWorkspace(dayDir, runID)
	if err != nil {
		return nil, err
	}
	return &personGenerateResult{
		Summary:     report,
		SourceCount: sourceCount,
		Coverage:    coverage,
		CutoffAt:    cutoffAt,
		ReportPath:  reportPath,
	}, nil
}

func (g *personGenerator) initializeDayWorkspace(ctx context.Context, date string) (string, error) {
	scriptPath := filepath.Join(g.skillDir, "scripts", "init-day.sh")
	command := exec.CommandContext(ctx, "bash", scriptPath, date, g.workspaceRoot)
	command.Dir = g.workspaceRoot
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("initialize personal daily workspace for %s: %w: %s", date, err, strings.TrimSpace(string(output)))
	}
	dayDir := filepath.Join(g.workspaceRoot, "data", "personal-daily", date)
	stat, err := os.Stat(dayDir)
	if err != nil {
		return "", fmt.Errorf("stat initialized personal daily workspace %q: %w", dayDir, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("initialized personal daily workspace %q is not a directory", dayDir)
	}
	return dayDir, nil
}

func (g *personGenerator) writeJarvisEvidence(
	dayDir, date string,
	dayStart, windowEnd, cutoffAt time.Time,
	baseline *personBaseline,
) (string, error) {
	initialPath := filepath.Join(dayDir, "10-evidence-jarvis.md")
	raw, err := os.ReadFile(initialPath)
	if err != nil {
		return "", fmt.Errorf("read initialized Jarvis evidence file %q: %w", initialPath, err)
	}
	path := initialPath
	evidenceSection, _ := markdownSection(string(raw), "## Evidence")
	if strings.Contains(string(raw), jarvisSeedMarker) || strings.TrimSpace(evidenceSection) != "" {
		path = filepath.Join(
			dayDir,
			"11-evidence-jarvis-refresh-"+time.Now().In(g.location).Format("20060102T150405.000000000")+".md",
		)
	}

	content := g.renderJarvisEvidence(date, dayStart, windowEnd, cutoffAt, baseline)
	if path == initialPath {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write Jarvis evidence %q: %w", path, err)
		}
		return path, nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create Jarvis refresh evidence %q: %w", path, err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write Jarvis refresh evidence %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close Jarvis refresh evidence %q: %w", path, err)
	}
	return path, nil
}

func (g *personGenerator) renderJarvisEvidence(
	date string,
	dayStart, windowEnd, cutoffAt time.Time,
	baseline *personBaseline,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Jarvis evidence — %s\n\n", date)
	b.WriteString("## Scope\n")
	fmt.Fprintf(&b, "- Principal: %s\n", g.principalOpenID)
	fmt.Fprintf(&b, "- Window: [%s, %s)\n", formatTime(g.location, dayStart), formatTime(g.location, windowEnd))
	fmt.Fprintf(&b, "- Cutoff: %s\n", formatTime(g.location, cutoffAt))
	b.WriteString("- Query plan: deterministic Jarvis DB event-time queries\n\n")
	b.WriteString("## Coverage\n")
	g.writeBaselineCoverage(&b, baseline, "authored_messages", len(baseline.Messages), nil)
	g.writeBaselineCoverage(&b, baseline, "todo_events", len(baseline.TodoEvents), baseline.IntegrityGaps)
	g.writeBaselineCoverage(&b, baseline, "task_events", len(baseline.TaskEvents), nil)
	g.writeBaselineCoverage(&b, baseline, "execution_runs", len(baseline.ExecutionRuns), nil)
	g.writeBaselineCoverage(&b, baseline, "facts", len(baseline.Facts), nil)

	b.WriteString("\n## Evidence\n")
	for _, item := range baseline.Messages {
		g.writeEvidence(&b, "JARVIS-message-"+item.MessageID, "message in "+item.Conversation, [][2]string{
			{"Source kind", "message"},
			{"Source ID", item.MessageID},
			{"Occurred at", item.OccurredAt},
			{"Actor", g.principalOpenID},
			{"Project binding", item.Project},
			{"Relation to principal", "direct"},
			{"Observed facts", item.Content},
		})
	}
	g.writeTodoEvidence(&b, baseline.TodoEvents)
	g.writeTaskEvidence(&b, baseline.TaskEvents)
	g.writeExecutionRunEvidence(&b, baseline.ExecutionRuns)

	b.WriteString("\n## Context\n")
	for _, item := range baseline.Facts {
		subject := firstNonBlank(item.Subject, fmt.Sprintf("%s#%d", item.SubjectType, item.SubjectID))
		projectBinding := ""
		if item.SubjectType == "project" {
			projectBinding = item.Subject
		}
		g.writeEvidence(&b, fmt.Sprintf("JARVIS-fact-%d", item.FactID), subject, [][2]string{
			{"Source kind", "fact"},
			{"Source ID", fmt.Sprint(item.FactID)},
			{"Occurred at", item.OccurredAt},
			{"Subject", subject},
			{"Project binding", projectBinding},
			{"Relation to principal", "context only; actor unknown"},
			{"Observed facts", item.Description},
		})
	}

	b.WriteString("\n## Gaps\n")
	if len(baseline.TruncatedScopes) == 0 && len(baseline.IntegrityGaps) == 0 {
		b.WriteString("- None in the deterministic Jarvis lane.\n")
	} else {
		for _, scope := range baseline.TruncatedScopes {
			fmt.Fprintf(&b, "- %s exceeded the local query limit.\n", markdownInline(scope))
		}
		for _, gap := range baseline.IntegrityGaps {
			fmt.Fprintf(&b, "- %s\n", markdownInline(gap))
		}
	}
	fmt.Fprintf(&b, "\n%s\n", jarvisSeedMarker)
	return b.String()
}

func (g *personGenerator) writeTodoEvidence(b *strings.Builder, events []baselineTodoEvent) {
	order := make([]uint64, 0)
	grouped := make(map[uint64][]baselineTodoEvent)
	for _, event := range events {
		if _, exists := grouped[event.TodoID]; !exists {
			order = append(order, event.TodoID)
		}
		grouped[event.TodoID] = append(grouped[event.TodoID], event)
	}
	for _, todoID := range order {
		items := grouped[todoID]
		first := items[0]
		last := items[len(items)-1]
		var sourceIDs, actors, relations, transitions, quotes []string
		title := ""
		project := ""
		commitment := ""
		dueAt := ""
		rawReference := ""
		for _, item := range items {
			sourceIDs = append(sourceIDs, fmt.Sprint(item.EventID))
			actors = appendUniqueString(actors, item.Actor)
			relations = appendUniqueString(relations, relationForActor(item.Actor))
			transition := strings.TrimSpace(item.FromStatus + " → " + item.ToStatus)
			transitions = appendUniqueString(
				transitions,
				strings.TrimSpace(item.OccurredAt+" "+transition+" actor="+item.Actor),
			)
			quotes = appendUniqueString(quotes, item.SourceQuote)
			title = firstNonBlank(title, item.Title)
			project = firstNonBlank(project, item.ProjectBinding)
			commitment = firstNonBlank(commitment, item.CommitmentStrength)
			dueAt = firstNonBlank(dueAt, item.DueAt)
			rawReference = firstNonBlank(
				rawReference,
				item.ContextSnapshot,
				item.Context,
				item.Resolution,
				item.Detail,
			)
		}
		observed := strings.Join(transitions, " | ")
		if len(quotes) > 0 {
			observed += "; source: " + strings.Join(quotes, " | ")
		}
		commitmentContext := commitment
		if dueAt != "" {
			commitmentContext = strings.TrimSpace(commitmentContext + "; due=" + dueAt)
		}
		g.writeEvidence(
			b,
			fmt.Sprintf("JARVIS-todo-%d", todoID),
			firstNonBlank(title, fmt.Sprintf("Todo#%d", todoID)),
			[][2]string{
				{"Source kind", "todo_lifecycle"},
				{"Source IDs", strings.Join(sourceIDs, ", ")},
				{"Occurred at", first.OccurredAt + " → " + last.OccurredAt},
				{"Actor", strings.Join(actors, ", ")},
				{"Project binding", project},
				{"Relation to principal", strings.Join(relations, ", ")},
				{"Observed facts", capRunes(observed, 800)},
				{"Commitment context", commitmentContext},
				{"Raw reference", capRunes(rawReference, 400)},
			},
		)
	}
}

func (g *personGenerator) writeTaskEvidence(b *strings.Builder, events []baselineTaskEvent) {
	order := make([]uint64, 0)
	grouped := make(map[uint64][]baselineTaskEvent)
	for _, event := range events {
		if _, exists := grouped[event.TaskID]; !exists {
			order = append(order, event.TaskID)
		}
		grouped[event.TaskID] = append(grouped[event.TaskID], event)
	}
	for _, taskID := range order {
		items := grouped[taskID]
		first := items[0]
		last := items[len(items)-1]
		var sourceIDs, actors, relations, transitions []string
		title := ""
		project := ""
		rawReference := ""
		for _, item := range items {
			sourceIDs = append(sourceIDs, fmt.Sprint(item.EventID))
			actor := firstNonBlank(item.ActorRef, item.ActorType)
			actors = appendUniqueString(actors, actor)
			relations = appendUniqueString(relations, relationForActor(item.ActorType))
			transition := strings.TrimSpace(item.FromStatus + " → " + item.ToStatus)
			transitions = appendUniqueString(
				transitions,
				strings.TrimSpace(item.OccurredAt+" "+item.EventType+": "+transition+" actor="+actor),
			)
			title = firstNonBlank(title, item.Title)
			project = firstNonBlank(project, item.Project)
			rawReference = firstNonBlank(rawReference, item.Detail, item.Background)
		}
		g.writeEvidence(
			b,
			fmt.Sprintf("JARVIS-task-%d", taskID),
			firstNonBlank(title, fmt.Sprintf("Task#%d", taskID)),
			[][2]string{
				{"Source kind", "task_lifecycle"},
				{"Source IDs", strings.Join(sourceIDs, ", ")},
				{"Occurred at", first.OccurredAt + " → " + last.OccurredAt},
				{"Actor", strings.Join(actors, ", ")},
				{"Project binding", project},
				{"Relation to principal", strings.Join(relations, ", ")},
				{"Observed facts", capRunes(strings.Join(transitions, " | "), 800)},
				{"Raw reference", capRunes(rawReference, 400)},
			},
		)
	}
}

func (g *personGenerator) writeExecutionRunEvidence(b *strings.Builder, runs []baselineExecutionRun) {
	order := make([]string, 0)
	grouped := make(map[string][]baselineExecutionRun)
	for _, run := range runs {
		key := fmt.Sprintf("task-%d", run.TaskID)
		if run.TaskID == 0 {
			key = fmt.Sprintf("run-%d", run.RunID)
		}
		if _, exists := grouped[key]; !exists {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], run)
	}
	for _, key := range order {
		items := grouped[key]
		first := items[0]
		last := items[len(items)-1]
		var runIDs, actors, states, facts, artifacts, rawReferences []string
		title := ""
		project := ""
		for _, item := range items {
			runIDs = append(runIDs, fmt.Sprint(item.RunID))
			actors = appendUniqueString(actors, firstNonBlank(item.CodexSessionID, "jarvis_execution"))
			states = appendUniqueString(states, item.Status)
			facts = appendUniqueString(
				facts,
				strings.TrimSpace(
					item.OccurredAt+" "+item.ActionType+" status="+item.Status+"; "+item.Summary,
				),
			)
			artifacts = appendUniqueString(artifacts, item.MergeRequestURL)
			rawReferences = appendUniqueString(
				rawReferences,
				strings.TrimSpace(item.Commit+"; "+item.ErrorDetail),
			)
			title = firstNonBlank(title, item.Title)
			project = firstNonBlank(project, item.Project)
		}
		g.writeEvidence(
			b,
			"JARVIS-execution-"+key,
			firstNonBlank(title, fmt.Sprintf("Run#%d", first.RunID)),
			[][2]string{
				{"Source kind", "execution_run_timeline"},
				{"Source IDs", strings.Join(runIDs, ", ")},
				{"Occurred at", first.OccurredAt + " → " + last.OccurredAt},
				{"Actor", strings.Join(actors, ", ")},
				{"Project binding", project},
				{"Relation to principal", "agent-delegated"},
				{"Lifecycle state", strings.Join(states, ", ")},
				{"Observed facts", capRunes(strings.Join(facts, " | "), 1000)},
				{"Artifact URL", strings.Join(artifacts, ", ")},
				{"Raw reference", capRunes(strings.Join(rawReferences, " | "), 500)},
			},
		)
	}
}

func (g *personGenerator) writeBaselineCoverage(
	b *strings.Builder,
	baseline *personBaseline,
	scope string,
	count int,
	gaps []string,
) {
	status := "complete"
	if count == 0 {
		status = "empty"
	}
	if containsString(baseline.TruncatedScopes, scope) || len(gaps) > 0 {
		status = "partial"
	}
	fmt.Fprintf(b, "- %s: %s\n", scope, status)
	fmt.Fprintf(b, "  - Query: jarvis_db:%s\n", scope)
	fmt.Fprintf(b, "  - Count: %d\n", count)
	if containsString(baseline.TruncatedScopes, scope) {
		fmt.Fprintf(b, "  - Error: exceeded local query limit\n")
	}
	if len(gaps) > 0 {
		fmt.Fprintf(b, "  - Error: %s\n", markdownInline(strings.Join(gaps, "; ")))
	}
}

func (g *personGenerator) writeEvidence(
	b *strings.Builder,
	evidenceID, subject string,
	fields [][2]string,
) {
	fmt.Fprintf(b, "\n### %s — %s\n", markdownInline(evidenceID), headingText(subject))
	for _, field := range fields {
		if strings.TrimSpace(field[1]) == "" {
			continue
		}
		fmt.Fprintf(b, "- %s: %s\n", field[0], markdownInline(field[1]))
	}
}

func (g *personGenerator) buildSkillPrompt(
	runID, date string,
	dayStart, windowEnd, cutoffAt time.Time,
	dayDir, jarvisEvidencePath string,
	regeneration bool,
) string {
	relativeJarvisEvidence, err := filepath.Rel(g.workspaceRoot, jarvisEvidencePath)
	if err != nil {
		relativeJarvisEvidence = jarvisEvidencePath
	}
	relativeDayDir, err := filepath.Rel(g.workspaceRoot, dayDir)
	if err != nil {
		relativeDayDir = dayDir
	}
	relativeSkillDir, err := filepath.Rel(g.workspaceRoot, g.skillDir)
	if err != nil {
		relativeSkillDir = g.skillDir
	}
	var b strings.Builder
	b.WriteString("你正在执行 Jarvis「进度 → 每日总结」里的“我的日报”。必须真正完成 Skill 工作流并写文件，不能只给方案或在最终消息中临时写一篇摘要。\n\n")
	b.WriteString("# Skill 入口\n")
	b.WriteString(g.skillText)
	b.WriteString("\n\n# 本轮硬边界\n")
	fmt.Fprintf(&b, "- Run ID: %s\n", runID)
	fmt.Fprintf(&b, "- Principal open_id: %s\n", g.principalOpenID)
	fmt.Fprintf(&b, "- Git author: %s\n", g.gitAuthor)
	fmt.Fprintf(&b, "- Natural day: %s\n", date)
	fmt.Fprintf(&b, "- Window: [%s, %s)\n", formatTime(g.location, dayStart), formatTime(g.location, windowEnd))
	fmt.Fprintf(&b, "- Cutoff: %s\n", formatTime(g.location, cutoffAt))
	fmt.Fprintf(&b, "- Timezone: %s\n", g.location.String())
	fmt.Fprintf(&b, "- Jarvis workspace root: %s\n", g.workspaceRoot)
	fmt.Fprintf(&b, "- Engineering repository discovery root: %s\n", g.repoRoot)
	fmt.Fprintf(&b, "- Day workspace: %s\n", relativeDayDir)
	fmt.Fprintf(&b, "- Skill directory: %s\n", relativeSkillDir)
	fmt.Fprintf(&b, "- Deterministic Jarvis evidence for this run: %s\n", relativeJarvisEvidence)
	fmt.Fprintf(&b, "- Regeneration: %t\n", regeneration)
	b.WriteString("\n# 执行要求\n")
	b.WriteString("- 日目录已经由 init-day.sh 初始化。Jarvis 确定性证据也已写好；保持该证据文件 append-only，不要覆盖。\n")
	b.WriteString("- 主 Agent 先把 Run ID 和本轮三个证据文件写入 00-context.md。Coverage 必须保留精确行 `- Jarvis: <status>`、`- Feishu: <status>`、`- Engineering: <status>`，status 只能是 complete/partial/empty/error/unavailable。\n")
	b.WriteString("- 只启动两个并行 subagent：一个 Feishu collector、一个 engineering collector。collector 禁止再派生任何 agent；每个 worker 只写自己的 Markdown 文件。\n")
	b.WriteString("- 如果初始 20/30 文件已有旧证据，为本轮分别分配一个新的 refresh 文件；只合成本轮 Jarvis/Feishu/Engineering 三个文件，不重读当天全部历史证据。\n")
	b.WriteString("- 日报只消费已发布报告、Run 最终结果和直接关联产物；禁止批量重算 Base/Sheets 数据、回放完整 session transcript、扫描全部仓库或枚举全部 CLI 能力。\n")
	b.WriteString("- 两个 collector 结束后立即由主 Agent 归并成稿。禁止额外调查轮次、独立审阅 agent、分章节 agent 或其他 subagent；未解决的信息直接写入覆盖缺口。\n")
	b.WriteString("- 所有外部系统只读；本任务不授权发消息、改文档、改代码、提交、部署或其他外部写操作。\n")
	b.WriteString("- 主 Agent 在内存中完成归并与写作，最后一次性创建或替换 99-report.md；不要创建 40/50/60/90 等中间阶段文件。报告标题必须是 `# 我的日报 · YYYY-MM-DD`，并严格保留 Skill 规定的九个 `##` 导航章节。\n")
	b.WriteString("- 完成前检查 99-report.md 已属于本轮，而不是沿用旧文件。最终回复只简短报告完成状态和 99-report.md 路径；文件才是 canonical 结果，不要输出 JSON DTO。\n")
	return b.String()
}

func snapshotReport(path string) (reportSnapshot, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return reportSnapshot{}, nil
	}
	if err != nil {
		return reportSnapshot{}, fmt.Errorf("read existing canonical personal report %q: %w", path, err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return reportSnapshot{}, fmt.Errorf("stat existing canonical personal report %q: %w", path, err)
	}
	return reportSnapshot{exists: true, modTime: stat.ModTime(), hash: sha256.Sum256(raw)}, nil
}

func readFreshPersonReport(path, date string, before reportSnapshot) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read canonical personal report %q: %w", path, err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat canonical personal report %q: %w", path, err)
	}
	afterHash := sha256.Sum256(raw)
	if before.exists && afterHash == before.hash && !stat.ModTime().After(before.modTime) {
		return "", fmt.Errorf("canonical personal report %q was not refreshed by this run", path)
	}
	report := strings.TrimSpace(string(raw))
	if err := validatePersonReport(report, date); err != nil {
		return "", fmt.Errorf("validate canonical personal report %q: %w", path, err)
	}
	return report, nil
}

func validatePersonReport(report, date string) error {
	if strings.TrimSpace(report) == "" {
		return fmt.Errorf("report is blank")
	}
	title := "# 我的日报 · " + date
	if !strings.HasPrefix(report, title) {
		return fmt.Errorf("report must start with %q", title)
	}
	last := -1
	for _, heading := range personReportHeadings {
		index := strings.Index(report, heading)
		if index < 0 {
			return fmt.Errorf("report missing heading %q", heading)
		}
		if index <= last {
			return fmt.Errorf("report heading %q is out of order", heading)
		}
		last = index
	}
	return nil
}

func inspectPersonWorkspace(dayDir, runID string) (int, SourceCoverage, error) {
	contextPath := filepath.Join(dayDir, "00-context.md")
	contextRaw, err := os.ReadFile(contextPath)
	if err != nil {
		return 0, nil, fmt.Errorf("read daily context %q: %w", contextPath, err)
	}
	contextText := string(contextRaw)
	if !strings.Contains(contextText, runID) {
		return 0, nil, fmt.Errorf("daily context %q does not contain current run ID %q", contextPath, runID)
	}
	coverageSection, ok := markdownSection(contextText, "## Coverage")
	if !ok {
		return 0, nil, fmt.Errorf("daily context %q is missing Coverage", contextPath)
	}

	files, err := filepath.Glob(filepath.Join(dayDir, "*-evidence-*.md"))
	if err != nil {
		return 0, nil, fmt.Errorf("glob personal evidence files: %w", err)
	}
	sort.Strings(files)
	laneIDs := map[string]map[string]struct{}{
		"jarvis_internal":       {},
		"feishu_work":           {},
		"engineering_execution": {},
	}
	allIDs := make(map[string]struct{})
	laneFiles := map[string]int{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return 0, nil, fmt.Errorf("read personal evidence file %q: %w", path, err)
		}
		ids, err := evidenceIDsFromMarkdown(string(raw))
		if err != nil {
			return 0, nil, fmt.Errorf("inspect personal evidence file %q: %w", path, err)
		}
		lane := evidenceLane(filepath.Base(path))
		if lane != "" {
			laneFiles[lane]++
		}
		for _, id := range ids {
			allIDs[id] = struct{}{}
			if lane != "" {
				laneIDs[lane][id] = struct{}{}
			}
		}
	}

	specs := []struct {
		key   string
		label string
	}{
		{key: "jarvis_internal", label: "Jarvis"},
		{key: "feishu_work", label: "Feishu"},
		{key: "engineering_execution", label: "Engineering"},
	}
	coverage := make(SourceCoverage, len(specs))
	for _, spec := range specs {
		if laneFiles[spec.key] == 0 {
			return 0, nil, fmt.Errorf("daily workspace %q has no %s evidence file", dayDir, spec.label)
		}
		status, line, err := coverageStatus(coverageSection, spec.label)
		if err != nil {
			return 0, nil, err
		}
		coverage[spec.key] = SourceCoverageItem{
			Status: status,
			Count:  len(laneIDs[spec.key]),
			Note:   line,
		}
	}
	return len(allIDs), coverage, nil
}

func markdownSection(content, heading string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var section []string
	found := false
	active := false
	for _, line := range lines {
		if strings.TrimSpace(line) == heading {
			section = nil
			found = true
			active = true
			continue
		}
		if active && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			active = false
		}
		if active {
			section = append(section, line)
		}
	}
	return strings.Join(section, "\n"), found
}

func evidenceIDsFromMarkdown(content string) ([]string, error) {
	section, ok := markdownSection(content, "## Evidence")
	if !ok {
		return nil, fmt.Errorf("missing Evidence section")
	}
	result := make([]string, 0)
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "### ") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "### "))
		if index := strings.Index(value, " — "); index >= 0 {
			value = strings.TrimSpace(value[:index])
		}
		if value == "" {
			return nil, fmt.Errorf("blank evidence ID heading")
		}
		result = append(result, value)
	}
	return result, nil
}

func evidenceLane(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "jarvis"):
		return "jarvis_internal"
	case strings.Contains(lower, "feishu"):
		return "feishu_work"
	case strings.Contains(lower, "engineering"):
		return "engineering_execution"
	default:
		return ""
	}
}

func coverageStatus(section, label string) (string, string, error) {
	prefix := "- " + strings.ToLower(label) + ":"
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), prefix) {
			continue
		}
		value := strings.TrimSpace(trimmed[len(prefix):])
		fields := strings.Fields(strings.Trim(value, "`*_"))
		if len(fields) == 0 {
			return "", "", fmt.Errorf("coverage %s has no status", label)
		}
		status := strings.Trim(strings.ToLower(fields[0]), "`*_,.;:()[]")
		switch status {
		case "complete", "partial", "empty", "error", "unavailable":
			return status, trimmed, nil
		default:
			return "", "", fmt.Errorf("coverage %s has invalid status %q", label, status)
		}
	}
	return "", "", fmt.Errorf("coverage is missing exact %s line", label)
}

// loadBaseline 使用精确自然日事件，而不是 updated_at + 当前 open 状态混查。
func (g *personGenerator) loadBaseline(
	ctx context.Context,
	dayStart, windowEnd time.Time,
) (*personBaseline, error) {
	baseline := &personBaseline{}

	var messages []domain.Message
	if err := g.db.WithContext(ctx).
		Preload("Group.Project").
		Where(
			"sender_open_id = ? AND create_time >= ? AND create_time < ?",
			g.principalOpenID,
			dayStart.UnixMilli(),
			windowEnd.UnixMilli(),
		).
		Order("create_time DESC, id DESC").
		Limit(personMessageLimit + 1).
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("load my messages: %w", err)
	}
	if len(messages) > personMessageLimit {
		messages = messages[:personMessageLimit]
		baseline.TruncatedScopes = append(baseline.TruncatedScopes, "authored_messages")
	}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		conversation := message.ChatID
		project := ""
		if message.Group != nil {
			if message.Group.Name != nil && strings.TrimSpace(*message.Group.Name) != "" {
				conversation = *message.Group.Name
			}
			if message.Group.Project != nil {
				project = message.Group.Project.Name
			}
		}
		baseline.Messages = append(baseline.Messages, baselineMessage{
			MessageID:    message.MessageID,
			OccurredAt:   formatTime(g.location, time.UnixMilli(message.CreateTime)),
			Conversation: conversation,
			Project:      project,
			Content:      capRunes(message.Content, personMessageContentCap),
		})
	}

	var todoEvents []domain.TodoEvent
	if err := g.db.WithContext(ctx).
		Where("created_at >= ? AND created_at < ?", dayStart, windowEnd).
		Order("created_at ASC, id ASC").
		Limit(personInternalLimit + 1).
		Find(&todoEvents).Error; err != nil {
		return nil, fmt.Errorf("load personal digest todo events: %w", err)
	}
	if len(todoEvents) > personInternalLimit {
		todoEvents = todoEvents[:personInternalLimit]
		baseline.TruncatedScopes = append(baseline.TruncatedScopes, "todo_events")
	}
	for _, event := range todoEvents {
		item := baselineTodoEvent{
			EventID:    event.ID,
			TodoID:     event.TodoID,
			OccurredAt: formatTime(g.location, event.CreatedAt),
			ToStatus:   event.ToStatus,
			Actor:      event.Actor,
			Detail:     capRunes(string(event.Detail), 300),
		}
		if event.FromStatus != nil {
			item.FromStatus = *event.FromStatus
		}
		if len(event.Snapshot) == 0 || string(event.Snapshot) == "null" {
			baseline.IntegrityGaps = append(
				baseline.IntegrityGaps,
				fmt.Sprintf("todo_event %d has no immutable snapshot", event.ID),
			)
		} else {
			var snapshot domain.TodoEventSnapshot
			if err := json.Unmarshal(event.Snapshot, &snapshot); err != nil {
				return nil, fmt.Errorf("decode todo_event id=%d snapshot: %w", event.ID, err)
			}
			item.Title = snapshot.Title
			if snapshot.ProjectID != nil {
				item.ProjectBinding = fmt.Sprintf("project_id:%d", *snapshot.ProjectID)
			}
			item.CommitmentStrength = snapshot.CommitmentStrength
			item.LeaderAssigned = snapshot.LeaderAssigned
			if snapshot.DueAt != nil {
				item.DueAt = formatTime(g.location, *snapshot.DueAt)
			}
			item.SourceQuote = capRunes(snapshot.SourceQuote, 300)
			item.Context = capRunes(snapshot.Context, 300)
			item.ContextSnapshot = capRunes(string(snapshot.ContextSnapshot), 400)
			item.Resolution = capRunes(string(snapshot.Resolution), 300)
		}
		baseline.TodoEvents = append(baseline.TodoEvents, item)
	}

	var taskEvents []domain.TaskEvent
	if err := g.db.WithContext(ctx).
		Preload("Task.Project").
		Where("occurred_at >= ? AND occurred_at < ?", dayStart, windowEnd).
		Order("occurred_at ASC, id ASC").
		Limit(personInternalLimit + 1).
		Find(&taskEvents).Error; err != nil {
		return nil, fmt.Errorf("load personal digest task events: %w", err)
	}
	if len(taskEvents) > personInternalLimit {
		taskEvents = taskEvents[:personInternalLimit]
		baseline.TruncatedScopes = append(baseline.TruncatedScopes, "task_events")
	}
	for _, event := range taskEvents {
		item := baselineTaskEvent{
			EventID:    event.ID,
			TaskID:     event.TaskID,
			OccurredAt: formatTime(g.location, event.OccurredAt),
			EventType:  event.EventType,
			ToStatus:   event.ToStatus,
			ActorType:  event.ActorType,
			Detail:     capRunes(string(event.Detail), 300),
		}
		if event.FromStatus != nil {
			item.FromStatus = *event.FromStatus
		}
		if event.ActorRef != nil {
			item.ActorRef = *event.ActorRef
		}
		if event.Task != nil {
			item.Title = event.Task.Title
			item.Background = capRunes(string(event.Task.Background), 400)
			if event.Task.Project != nil {
				item.Project = event.Task.Project.Name
			}
		}
		baseline.TaskEvents = append(baseline.TaskEvents, item)
	}

	var runs []domain.ExecutionRun
	if err := g.db.WithContext(ctx).
		Preload("Task.Project").
		Where(
			"(started_at >= ? AND started_at < ?) OR (finished_at >= ? AND finished_at < ?)",
			dayStart,
			windowEnd,
			dayStart,
			windowEnd,
		).
		Order("started_at ASC, id ASC").
		Limit(personInternalLimit + 1).
		Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("load personal digest execution runs: %w", err)
	}
	if len(runs) > personInternalLimit {
		runs = runs[:personInternalLimit]
		baseline.TruncatedScopes = append(baseline.TruncatedScopes, "execution_runs")
	}
	for _, run := range runs {
		item := baselineExecutionRun{
			RunID:      run.ID,
			TaskID:     run.TaskID,
			StartedAt:  formatTime(g.location, run.StartedAt),
			Status:     run.Status,
			ActionType: run.ActionType,
		}
		finishedByCutoff := run.FinishedAt != nil && run.FinishedAt.Before(windowEnd)
		if finishedByCutoff {
			item.FinishedAt = formatTime(g.location, *run.FinishedAt)
			item.OccurredAt = item.FinishedAt
			if run.Summary != nil {
				item.Summary = capRunes(*run.Summary, 500)
			}
			if run.ErrorDetail != nil {
				item.ErrorDetail = capRunes(*run.ErrorDetail, 300)
			}
			for _, effect := range runEffectsLoose(run.Effects) {
				kind, _ := effect["kind"].(string)
				if kind != "merge_request" {
					continue
				}
				if url, _ := effect["url"].(string); strings.TrimSpace(url) != "" {
					item.MergeRequestURL = strings.TrimSpace(url)
				}
				if commit, _ := effect["commit"].(string); strings.TrimSpace(commit) != "" {
					item.Commit = strings.TrimSpace(commit)
				}
			}
		} else {
			item.Status = "running_at_cutoff"
			item.OccurredAt = item.StartedAt
		}
		if run.CodexSessionID != nil {
			item.CodexSessionID = *run.CodexSessionID
		}
		if run.Task != nil {
			item.Title = run.Task.Title
			if run.Task.Project != nil {
				item.Project = run.Task.Project.Name
			}
		}
		baseline.ExecutionRuns = append(baseline.ExecutionRuns, item)
	}

	// Facts of every subject type count as evidence: the day's group and person
	// facts matter to a personal digest as much as the project ones do.
	var facts []domain.Fact
	if err := g.db.WithContext(ctx).
		Where("occurred_at >= ? AND occurred_at < ?", dayStart, windowEnd).
		Order("occurred_at ASC, id ASC").
		Limit(personInternalLimit + 1).
		Find(&facts).Error; err != nil {
		return nil, fmt.Errorf("load personal digest facts: %w", err)
	}
	if len(facts) > personInternalLimit {
		facts = facts[:personInternalLimit]
		baseline.TruncatedScopes = append(baseline.TruncatedScopes, "facts")
	}
	subjectNames, err := g.loadFactSubjectNames(ctx, facts)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		baseline.Facts = append(baseline.Facts, baselineFact{
			FactID:      fact.ID,
			SubjectType: fact.SubjectType,
			SubjectID:   fact.SubjectID,
			OccurredAt:  formatTime(g.location, fact.OccurredAt),
			Subject:     subjectNames[factSubjectKey{fact.SubjectType, fact.SubjectID}],
			Description: capRunes(fact.Description, 500),
		})
	}
	return baseline, nil
}

func relationForActor(actor string) string {
	switch strings.ToLower(strings.TrimSpace(actor)) {
	case "user", "principal":
		return "direct"
	case "agent", "m5", "codex":
		return "agent-delegated"
	default:
		return "system-recorded"
	}
}

func formatTime(location *time.Location, value time.Time) string {
	return value.In(location).Format(time.RFC3339)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || containsString(values, value) {
		return values
	}
	return append(values, value)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func markdownInline(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	value = strings.ReplaceAll(value, "\n", " ↵ ")
	return strings.ReplaceAll(value, "|", "\\|")
}

func headingText(value string) string {
	value = markdownInline(value)
	value = strings.ReplaceAll(value, "#", "")
	return capRunes(firstNonBlank(value, "untitled"), 120)
}

// loadPersonSummarySkill 在启动时校验完整 Skill 包，只把轻量 SKILL.md 注入
// 顶层 Codex；references 由 Agent 按需读取，避免每轮无条件占用上下文。
func loadPersonSummarySkill(skillDir string) (string, error) {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return "", fmt.Errorf("personal summary skill directory is empty")
	}
	files := []string{
		filepath.Join(skillDir, "SKILL.md"),
		filepath.Join(skillDir, "references", "context-and-capabilities.md"),
		filepath.Join(skillDir, "references", "storage-and-report.md"),
		filepath.Join(skillDir, "references", "channel-methods.md"),
	}
	var skillText string
	for index, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read personal summary skill file %q: %w", path, err)
		}
		if strings.TrimSpace(string(raw)) == "" {
			return "", fmt.Errorf("personal summary skill file %q is empty", path)
		}
		if index == 0 {
			skillText = strings.TrimSpace(string(raw))
		}
	}
	scriptPath := filepath.Join(skillDir, "scripts", "init-day.sh")
	stat, err := os.Stat(scriptPath)
	if err != nil {
		return "", fmt.Errorf("stat personal summary initializer %q: %w", scriptPath, err)
	}
	if stat.IsDir() || stat.Mode()&0o111 == 0 {
		return "", fmt.Errorf("personal summary initializer %q must be an executable file", scriptPath)
	}
	return skillText, nil
}

func runEffectsLoose(raw []byte) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var effects []map[string]any
	if err := json.Unmarshal(raw, &effects); err != nil {
		return nil
	}
	return effects
}

// loadFactSubjectNames resolves display names for fact subjects, one query per
// nameable type. Fact has no ORM association to preload: the subject is
// polymorphic by design, so names are looked up rather than joined. Subject
// types this does not recognize simply stay unnamed and render as type#id.
func (g *personGenerator) loadFactSubjectNames(ctx context.Context, facts []domain.Fact) (map[factSubjectKey]string, error) {
	names := make(map[factSubjectKey]string)
	ids := map[string][]uint64{}
	for _, fact := range facts {
		ids[fact.SubjectType] = append(ids[fact.SubjectType], fact.SubjectID)
	}
	for subjectType, subjectIDs := range ids {
		var rows []struct {
			ID   uint64
			Name string
		}
		var table string
		switch subjectType {
		case "project":
			table = "project"
		case "group":
			table = "feishu_group"
		case "person":
			table = "person"
		default:
			continue
		}
		if err := g.db.WithContext(ctx).Table(table).
			Select("id, name").Where("id IN ?", subjectIDs).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("load fact subject names type=%s: %w", subjectType, err)
		}
		for _, row := range rows {
			names[factSubjectKey{subjectType, row.ID}] = row.Name
		}
	}
	return names, nil
}
