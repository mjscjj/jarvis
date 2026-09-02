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

// Scope kinds accepted from the board UI. They mirror the three review buttons:
// the whole week, one KR, or one concrete point under a KR.
const (
	KindAll   = "all"
	KindKR    = "kr"
	KindPoint = "point"
)

// BoardReader is the slice of the OKR workspace the review needs. The agent can
// still reach further through the read tools; this only pre-loads the object
// under review so the common case costs one model turn instead of a tool round trip.
type BoardReader interface {
	Board(ctx context.Context, quarter, week string) (okrworkspace.Board, error)
}

// Request identifies what to review. IDs are the stable board IDs the frontend
// already holds.
type Request struct {
	Quarter string `json:"quarter"`
	Week    string `json:"week"`
	Kind    string `json:"kind"`
	KRID    string `json:"kr_id"`
	PointID string `json:"point_id"`
}

type Service struct {
	board   BoardReader
	prompts textstore.Reader
	runner  *runner
}

// Options carries everything the review needs. Timeout only bounds a runaway
// agent; a normal review finishes well inside it.
type Options struct {
	Board           BoardReader
	Prompts         textstore.Reader
	Bin             string
	Model           string
	Sandbox         string
	ReasoningEffort string
	Timeout         time.Duration
}

func NewService(opts Options) (*Service, error) {
	if opts.Board == nil {
		return nil, fmt.Errorf("okr review board reader is required")
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("okr review prompt reader is required")
	}
	r, err := newRunner(opts.Bin, opts.Model, opts.Sandbox, opts.ReasoningEffort, opts.Timeout)
	if err != nil {
		return nil, err
	}
	return &Service{board: opts.Board, prompts: opts.Prompts, runner: r}, nil
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
	req.Quarter = strings.TrimSpace(req.Quarter)
	req.Week = strings.TrimSpace(req.Week)
	req.Kind = strings.TrimSpace(req.Kind)
	req.KRID = strings.TrimSpace(req.KRID)
	req.PointID = strings.TrimSpace(req.PointID)
	if req.Quarter == "" || req.Week == "" {
		return "", fmt.Errorf("okr review requires quarter and week")
	}

	board, err := s.board.Board(ctx, req.Quarter, req.Week)
	if err != nil {
		return "", fmt.Errorf("load OKR board for review: %w", err)
	}
	if board.TemplateKey != domain.WeekTemplateOKRPreview {
		return "", fmt.Errorf("week %s uses template %q, OKR Preview review only applies to %q",
			board.Week, board.TemplateKey, domain.WeekTemplateOKRPreview)
	}
	target, err := renderTarget(board, req)
	if err != nil {
		return "", err
	}

	instructions, err := s.prompts.Content(ctx, textstore.OKRAgentPreviewReviewKey)
	if err != nil {
		return "", fmt.Errorf("read OKR Preview review prompt: %w", err)
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
	b.WriteString(fmt.Sprintf("- 季度：%s\n- 周次：%s\n- 模板：%s\n- 范围：%s\n",
		board.Quarter, board.Week, board.TemplateKey, scopeLabel(req)))
	b.WriteString("\n以下是该对象当前的完整内容，已经从库里读出，不需要再查一遍。需要看关联 KR、其它 O 或历史周次时才用工具。\n\n")
	b.WriteString("BEGIN_REVIEW_TARGET\n")
	b.WriteString(target)
	b.WriteString("\nEND_REVIEW_TARGET\n")
	return b.String(), nil
}

func scopeLabel(req Request) string {
	switch req.Kind {
	case KindKR:
		return "单个一级 KR（id=" + req.KRID + "）"
	case KindPoint:
		return "单个具体 KR（id=" + req.PointID + "）"
	default:
		return "该周全部 OKR"
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
		return "", fmt.Errorf("okr review kind must be all, kr or point, got %q", req.Kind)
	}
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

func writeObjective(b *strings.Builder, objective okrworkspace.ObjectiveView) {
	b.WriteString(fmt.Sprintf("# %s（%s）\n\n", objective.Title, objective.ID))
	for _, kr := range objective.KRs {
		writeKR(b, kr)
	}
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
