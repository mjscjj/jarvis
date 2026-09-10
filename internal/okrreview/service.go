package okrreview

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
)

// Scope kinds accepted from the board UI. A review can cover the whole source
// or one node in its O / KR / point hierarchy.
const (
	KindAll       = "all"
	KindObjective = "objective"
	KindKR        = "kr"
	KindPoint     = "point"

	ReviewTypePlan     = "plan"
	ReviewTypeProgress = "progress"
)

// WorkspaceReader is the slice of the OKR workspace the review needs. The agent can
// still reach further through the read tools; this only pre-loads the object
// under review so the common case costs one model turn instead of a tool round trip.
type WorkspaceReader interface {
	Board(ctx context.Context, quarter, week string) (okrworkspace.Board, error)
	GetPlan(ctx context.Context, id string) (okrworkspace.PlanView, error)
}

// Request identifies what to review. IDs are the stable board IDs the frontend
// already holds.
type Request struct {
	ReviewType  string `json:"review_type"`
	Quarter     string `json:"quarter"`
	Week        string `json:"week"`
	PlanID      string `json:"plan_id"`
	Kind        string `json:"kind"`
	ObjectiveID string `json:"objective_id"`
	KRID        string `json:"kr_id"`
	PointID     string `json:"point_id"`
}

type Service struct {
	workspace WorkspaceReader
	prompts   textstore.Reader
	runner    *runner
}

// Options carries everything the review needs. Timeout only bounds a runaway
// agent; a normal review finishes well inside it.
type Options struct {
	Workspace       WorkspaceReader
	Prompts         textstore.Reader
	Bin             string
	Model           string
	Sandbox         string
	ReasoningEffort string
	Timeout         time.Duration
}

func NewService(opts Options) (*Service, error) {
	if opts.Workspace == nil {
		return nil, fmt.Errorf("okr review workspace reader is required")
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("okr review prompt reader is required")
	}
	r, err := newRunner(opts.Bin, opts.Model, opts.Sandbox, opts.ReasoningEffort, opts.Timeout)
	if err != nil {
		return nil, err
	}
	return &Service{workspace: opts.Workspace, prompts: opts.Prompts, runner: r}, nil
}

// Review runs one advisory review and returns its Markdown report.
func (s *Service) Review(ctx context.Context, req Request) (string, error) {
	prompt, err := s.buildPrompt(ctx, req)
	if err != nil {
		return "", err
	}
	return s.runner.Run(ctx, prompt)
}

func (s *Service) buildPrompt(ctx context.Context, req Request) (string, error) {
	req.ReviewType = strings.TrimSpace(req.ReviewType)
	req.Quarter = strings.TrimSpace(req.Quarter)
	req.Week = strings.TrimSpace(req.Week)
	req.PlanID = strings.TrimSpace(req.PlanID)
	req.Kind = strings.TrimSpace(req.Kind)
	req.ObjectiveID = strings.TrimSpace(req.ObjectiveID)
	req.KRID = strings.TrimSpace(req.KRID)
	req.PointID = strings.TrimSpace(req.PointID)
	var instructions, target, metadata string
	var err error
	switch req.ReviewType {
	case ReviewTypePlan:
		instructions, target, metadata, err = s.buildPlanInput(ctx, req)
	case ReviewTypeProgress:
		instructions, target, metadata, err = s.buildProgressInput(ctx, req)
	default:
		return "", fmt.Errorf("okr review_type must be plan or progress, got %q", req.ReviewType)
	}
	if err != nil {
		return "", err
	}
	tools, err := toolcatalog.Block(toolcatalog.StageOKRReview)
	if err != nil {
		return "", fmt.Errorf("build OKR review tool catalog: %w", err)
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(instructions))
	b.WriteString("\n\n")
	b.WriteString(tools)
	b.WriteString("\n\n## 本次评审对象\n")
	b.WriteString(metadata)
	b.WriteString("\n以下是该对象当前的完整内容，已经从库里读出，不需要再查一遍。需要看关联 KR、其它 O 或历史内容时才用工具。\n\n")
	b.WriteString("BEGIN_REVIEW_TARGET\n")
	b.WriteString(target)
	b.WriteString("\nEND_REVIEW_TARGET\n")
	return b.String(), nil
}

func (s *Service) buildProgressInput(ctx context.Context, req Request) (instructions, target, metadata string, err error) {
	if req.Quarter == "" || req.Week == "" {
		return "", "", "", fmt.Errorf("okr progress review requires quarter and week")
	}
	board, err := s.workspace.Board(ctx, req.Quarter, req.Week)
	if err != nil {
		return "", "", "", fmt.Errorf("load OKR board for review: %w", err)
	}
	if board.TemplateKey != domain.WeekTemplateOKRPreview {
		return "", "", "", fmt.Errorf("week %s uses template %q, OKR progress review only applies to %q",
			board.Week, board.TemplateKey, domain.WeekTemplateOKRPreview)
	}
	target, err = renderTarget(board, req)
	if err != nil {
		return "", "", "", err
	}
	instructions, err = s.prompts.Content(ctx, textstore.OKRAgentProgressReviewKey)
	if err != nil {
		return "", "", "", fmt.Errorf("read OKR progress review prompt: %w", err)
	}
	metadata = fmt.Sprintf("- 类型：OKR 进度评审\n- 季度：%s\n- 周次：%s\n- 模板：%s\n- 范围：%s\n",
		board.Quarter, board.Week, board.TemplateKey, scopeLabel(req))
	return instructions, target, metadata, nil
}

func (s *Service) buildPlanInput(ctx context.Context, req Request) (instructions, target, metadata string, err error) {
	if req.Quarter == "" || req.PlanID == "" {
		return "", "", "", fmt.Errorf("okr plan review requires quarter and plan_id")
	}
	plan, err := s.workspace.GetPlan(ctx, req.PlanID)
	if err != nil {
		return "", "", "", fmt.Errorf("load OKR plan for review: %w", err)
	}
	if req.Quarter != "" && req.Quarter != plan.Quarter {
		return "", "", "", fmt.Errorf("plan %s belongs to %s, not %s", plan.ID, plan.Quarter, req.Quarter)
	}
	target, err = renderPlanTarget(plan, req)
	if err != nil {
		return "", "", "", err
	}
	instructions, err = s.prompts.Content(ctx, textstore.OKRAgentPlanReviewKey)
	if err != nil {
		return "", "", "", fmt.Errorf("read OKR Plan review prompt: %w", err)
	}
	metadata = fmt.Sprintf("- 类型：OKR Plan 评审\n- 季度：%s\n- Plan：%s（%s）\n- 范围：%s\n",
		plan.Quarter, plan.Title, plan.ID, scopeLabel(req))
	return instructions, target, metadata, nil
}

func scopeLabel(req Request) string {
	switch req.Kind {
	case KindObjective:
		return "单个 O（id=" + req.ObjectiveID + "）"
	case KindKR:
		return "单个一级 KR（id=" + req.KRID + "）"
	case KindPoint:
		return "单个具体 KR（id=" + req.PointID + "）"
	default:
		return "全部 OKR"
	}
}

func renderPlanTarget(plan okrworkspace.PlanView, req Request) (string, error) {
	switch req.Kind {
	case KindAll, "":
		if len(plan.Objectives) == 0 {
			return "", fmt.Errorf("plan %s has no OKR content to review", plan.ID)
		}
		var b strings.Builder
		for _, objective := range plan.Objectives {
			writePlanObjective(&b, objective)
		}
		return b.String(), nil
	case KindObjective:
		objective, err := findPlanObjective(plan, req.ObjectiveID)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		writePlanObjective(&b, objective)
		return b.String(), nil
	case KindKR:
		objective, kr, err := findPlanKR(plan, req.KRID)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s（%s）\n\n", objective.Title, objective.ID))
		writePlanKR(&b, kr)
		return b.String(), nil
	case KindPoint:
		objective, kr, point, err := findPlanPoint(plan, req.PointID)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s（%s）\n", objective.Title, objective.ID))
		b.WriteString(fmt.Sprintf("## 所属一级 KR：%s（%s）\n\n", kr.Title, kr.ID))
		b.WriteString("父级内容仅作背景，不在本次评审范围内。\n\n")
		writePlanPoint(&b, point)
		return b.String(), nil
	default:
		return "", fmt.Errorf("okr review kind must be all, objective, kr or point, got %q", req.Kind)
	}
}

// renderTarget projects exactly the object under review into Markdown. A point
// review carries its parent KR as context but is judged on its own content.
func renderTarget(board okrworkspace.Board, req Request) (string, error) {
	switch req.Kind {
	case KindAll, "":
		if len(board.Objectives) == 0 {
			return "", fmt.Errorf("week %s has no OKR content to review", board.Week)
		}
		var b strings.Builder
		for _, objective := range board.Objectives {
			writeObjective(&b, objective)
		}
		return b.String(), nil
	case KindObjective:
		objective, err := findObjective(board, req.ObjectiveID)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		writeObjective(&b, objective)
		return b.String(), nil
	case KindKR:
		objective, kr, err := findKR(board, req.KRID)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s（%s）\n\n", objective.Title, objective.ID))
		writeKR(&b, kr)
		return b.String(), nil
	case KindPoint:
		objective, kr, point, err := findPoint(board, req.PointID)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s（%s）\n", objective.Title, objective.ID))
		b.WriteString(fmt.Sprintf("## 所属一级 KR：%s（%s）\n\n", kr.Title, kr.ID))
		b.WriteString("父级内容仅作背景，不在本次评审范围内。\n\n")
		writePoint(&b, point)
		return b.String(), nil
	default:
		return "", fmt.Errorf("okr review kind must be all, objective, kr or point, got %q", req.Kind)
	}
}

func findObjective(board okrworkspace.Board, objectiveID string) (okrworkspace.ObjectiveView, error) {
	if objectiveID == "" {
		return okrworkspace.ObjectiveView{}, fmt.Errorf("okr review kind=objective requires objective_id")
	}
	for _, objective := range board.Objectives {
		if objective.ID == objectiveID {
			return objective, nil
		}
	}
	return okrworkspace.ObjectiveView{}, fmt.Errorf("objective %q is not in %s / %s", objectiveID, board.Quarter, board.Week)
}

func findKR(board okrworkspace.Board, krID string) (okrworkspace.ObjectiveView, okrworkspace.KRView, error) {
	if krID == "" {
		return okrworkspace.ObjectiveView{}, okrworkspace.KRView{}, fmt.Errorf("okr review kind=kr requires kr_id")
	}
	for _, objective := range board.Objectives {
		for _, kr := range objective.KRs {
			if kr.ID == krID {
				return objective, kr, nil
			}
		}
	}
	return okrworkspace.ObjectiveView{}, okrworkspace.KRView{}, fmt.Errorf("KR %q is not in %s / %s", krID, board.Quarter, board.Week)
}

func findPoint(board okrworkspace.Board, pointID string) (okrworkspace.ObjectiveView, okrworkspace.KRView, okrworkspace.PointView, error) {
	if pointID == "" {
		return okrworkspace.ObjectiveView{}, okrworkspace.KRView{}, okrworkspace.PointView{}, fmt.Errorf("okr review kind=point requires point_id")
	}
	for _, objective := range board.Objectives {
		for _, kr := range objective.KRs {
			for _, point := range kr.Points {
				if point.ID == pointID {
					return objective, kr, point, nil
				}
			}
		}
	}
	return okrworkspace.ObjectiveView{}, okrworkspace.KRView{}, okrworkspace.PointView{},
		fmt.Errorf("point %q is not in %s / %s", pointID, board.Quarter, board.Week)
}

func findPlanKR(plan okrworkspace.PlanView, krID string) (okrworkspace.PlanObjectiveView, okrworkspace.PlanKRView, error) {
	if krID == "" {
		return okrworkspace.PlanObjectiveView{}, okrworkspace.PlanKRView{}, fmt.Errorf("okr review kind=kr requires kr_id")
	}
	for _, objective := range plan.Objectives {
		for _, kr := range objective.KRs {
			if kr.ID == krID {
				return objective, kr, nil
			}
		}
	}
	return okrworkspace.PlanObjectiveView{}, okrworkspace.PlanKRView{}, fmt.Errorf("KR %q is not in plan %s", krID, plan.ID)
}

func findPlanObjective(plan okrworkspace.PlanView, objectiveID string) (okrworkspace.PlanObjectiveView, error) {
	if objectiveID == "" {
		return okrworkspace.PlanObjectiveView{}, fmt.Errorf("okr review kind=objective requires objective_id")
	}
	for _, objective := range plan.Objectives {
		if objective.ID == objectiveID {
			return objective, nil
		}
	}
	return okrworkspace.PlanObjectiveView{}, fmt.Errorf("objective %q is not in plan %s", objectiveID, plan.ID)
}

func findPlanPoint(plan okrworkspace.PlanView, pointID string) (okrworkspace.PlanObjectiveView, okrworkspace.PlanKRView, okrworkspace.PlanPointView, error) {
	if pointID == "" {
		return okrworkspace.PlanObjectiveView{}, okrworkspace.PlanKRView{}, okrworkspace.PlanPointView{}, fmt.Errorf("okr review kind=point requires point_id")
	}
	for _, objective := range plan.Objectives {
		for _, kr := range objective.KRs {
			for _, point := range kr.Points {
				if point.ID == pointID {
					return objective, kr, point, nil
				}
			}
		}
	}
	return okrworkspace.PlanObjectiveView{}, okrworkspace.PlanKRView{}, okrworkspace.PlanPointView{},
		fmt.Errorf("point %q is not in plan %s", pointID, plan.ID)
}

func writeObjective(b *strings.Builder, objective okrworkspace.ObjectiveView) {
	b.WriteString(fmt.Sprintf("# %s（%s）\n\n", objective.Title, objective.ID))
	for _, kr := range objective.KRs {
		writeKR(b, kr)
	}
}

func writePlanObjective(b *strings.Builder, objective okrworkspace.PlanObjectiveView) {
	b.WriteString(fmt.Sprintf("# %s（%s）\n\n", objective.Title, objective.ID))
	for _, kr := range objective.KRs {
		writePlanKR(b, kr)
	}
}

func writePlanKR(b *strings.Builder, kr okrworkspace.PlanKRView) {
	b.WriteString(fmt.Sprintf("## %s（%s）\n", kr.Title, kr.ID))
	if owners := ownerNames(kr.Owners, ""); owners != "" {
		b.WriteString(fmt.Sprintf("- 负责人：%s\n", owners))
	}
	if tags := tagList(kr.Tags); tags != "" {
		b.WriteString(fmt.Sprintf("- 标签：%s\n", tags))
	}
	if note := strings.TrimSpace(kr.MetricNote); note != "" {
		b.WriteString(fmt.Sprintf("- 数据口径：%s\n", note))
	}
	if len(kr.Metrics) > 0 {
		b.WriteString("- 核心指标：\n")
		for _, metric := range kr.Metrics {
			b.WriteString(fmt.Sprintf("  - %s%s\n", strings.TrimSpace(metric.Text), lightSuffix(metric.Light)))
		}
	}
	b.WriteString("\n")
	for _, point := range kr.Points {
		writePlanPoint(b, point)
	}
}

func writePlanPoint(b *strings.Builder, point okrworkspace.PlanPointView) {
	b.WriteString(fmt.Sprintf("### %s（%s，kind=%s）\n", point.Title, point.ID, point.Kind))
	if owners := ownerNames(point.Owners, ""); owners != "" {
		b.WriteString(fmt.Sprintf("- 负责人：%s\n", owners))
	}
	if tags := tagList(point.Tags); tags != "" {
		b.WriteString(fmt.Sprintf("- 标签：%s\n", tags))
	}
	if url := strings.TrimSpace(point.MeegoURL); url != "" {
		b.WriteString(fmt.Sprintf("- Meego：%s\n", url))
	}
	b.WriteString("\n")
}

func writeKR(b *strings.Builder, kr okrworkspace.KRView) {
	b.WriteString(fmt.Sprintf("## %s（%s）\n", kr.Title, kr.ID))
	if owners := ownerNames(kr.Owners, kr.OwnerName); owners != "" {
		b.WriteString(fmt.Sprintf("- 负责人：%s\n", owners))
	}
	if tags := tagList(kr.Tags); tags != "" {
		b.WriteString(fmt.Sprintf("- 标签：%s\n", tags))
	}
	if kr.Score != nil {
		b.WriteString(fmt.Sprintf("- 人工评分：%.2f\n", kr.Score.Value))
	}
	if note := strings.TrimSpace(kr.MetricNote); note != "" {
		b.WriteString(fmt.Sprintf("- 数据口径：%s\n", note))
	}
	if len(kr.Metrics) > 0 {
		b.WriteString("- 核心指标：\n")
		for _, metric := range kr.Metrics {
			b.WriteString(fmt.Sprintf("  - %s%s\n", strings.TrimSpace(metric.Text), lightSuffix(metric.Light)))
		}
	}
	b.WriteString("\n")
	for _, point := range kr.Points {
		writePoint(b, point)
	}
}

func writePoint(b *strings.Builder, point okrworkspace.PointView) {
	b.WriteString(fmt.Sprintf("### %s（%s，kind=%s）\n", point.Title, point.ID, point.Kind))
	if owners := ownerNames(point.Owners, ""); owners != "" {
		b.WriteString(fmt.Sprintf("- 负责人：%s\n", owners))
	}
	if tags := tagList(point.Tags); tags != "" {
		b.WriteString(fmt.Sprintf("- 标签：%s\n", tags))
	}
	if point.Score != nil {
		b.WriteString(fmt.Sprintf("- 人工评分：%.2f\n", point.Score.Value))
	}
	if url := strings.TrimSpace(point.MeegoURL); url != "" {
		b.WriteString(fmt.Sprintf("- Meego：%s\n", url))
	}
	writeEntries(b, "本周进展", point.Entries)
	writeEntries(b, "上周进展", point.PreviousEntries)
	b.WriteString("\n")
}

func writeEntries(b *strings.Builder, label string, entries []okrworkspace.ProgressView) {
	if len(entries) == 0 {
		b.WriteString(fmt.Sprintf("- %s：（空）\n", label))
		return
	}
	b.WriteString(fmt.Sprintf("- %s：\n", label))
	for _, entry := range entries {
		text := strings.TrimSpace(entry.Text)
		if text == "" {
			text = "（空）"
		}
		b.WriteString(fmt.Sprintf("  - [%s] %s\n", entry.Status, text))
		for _, doc := range entry.Docs {
			b.WriteString(fmt.Sprintf("    - 文档：%s %s\n", strings.TrimSpace(doc.Title), strings.TrimSpace(doc.URL)))
		}
	}
}

func ownerNames(owners []okrworkspace.OwnerView, fallback string) string {
	var names []string
	for _, owner := range owners {
		if name := strings.TrimSpace(owner.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return strings.TrimSpace(fallback)
	}
	return strings.Join(names, "、")
}

func tagList(tags []okrworkspace.TagView) string {
	var rendered []string
	for _, tag := range tags {
		value := strings.TrimSpace(tag.Value)
		if value == "" {
			continue
		}
		rendered = append(rendered, strings.TrimSpace(tag.Type)+"="+value)
	}
	return strings.Join(rendered, "、")
}

func lightSuffix(light domain.Light) string {
	if strings.TrimSpace(string(light)) == "" {
		return ""
	}
	return fmt.Sprintf("（%s）", light)
}
