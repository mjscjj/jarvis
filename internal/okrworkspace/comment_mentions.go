package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

const (
	commentSourceTabOKRPlan           = "okr-plan"
	commentSourceTabReviewFill        = "review-fill"
	commentSourceTabReviewMeeting     = "review-meeting"
	commentSourceTabWeeklyFill        = "weekly-fill"
	commentSourceTabWeeklyMeeting     = "weekly-meeting"
	commentSourceTabRegionalAlignment = "regional-alignment"
)

func validCommentSourceTab(value string) bool {
	switch value {
	case commentSourceTabOKRPlan, commentSourceTabReviewFill, commentSourceTabReviewMeeting, commentSourceTabWeeklyFill, commentSourceTabWeeklyMeeting, commentSourceTabRegionalAlignment:
		return true
	default:
		return false
	}
}

// CommentMentionNotification is the complete, immutable fact sent to one
// recipient. The notifier must not query OKR state again: doing so could make
// the outbound message disagree with the comment that triggered it.
type CommentMentionNotification struct {
	Recipient      CommentMention
	Reason         string
	OwnerLevel     string
	CommentID      string
	AuthorName     string
	AuthorEmail    string
	Quarter        string
	Week           string
	PlanID         string
	PlanTitle      string
	AlignmentID    string
	RegionCode     string
	ObjectiveID    string
	ObjectiveTitle string
	KRID           string
	KRTitle        string
	PointID        string
	PointKind      domain.PointKind
	PointTitle     string
	OriginalText   string
	Content        string
	Tab            string
}

type CommentMentionNotifier interface {
	NotifyCommentMention(context.Context, CommentMentionNotification) (string, error)
	VerifyCommentMention(context.Context, string) error
	AppID() string
}

type commentBroadcastSender interface {
	SendCardToEmail(context.Context, string, string, string, string) (string, error)
	VerifyMessage(context.Context, string) error
	AppID() string
}

type BotCommentMentionNotifier struct {
	sender        commentBroadcastSender
	publicBaseURL string
}

func NewBotCommentMentionNotifier(sender commentBroadcastSender, publicBaseURL string) (*BotCommentMentionNotifier, error) {
	if sender == nil {
		return nil, fmt.Errorf("create Bot comment mention notifier: sender is nil")
	}
	publicBaseURL = strings.TrimSpace(publicBaseURL)
	if publicBaseURL != "" {
		parsed, err := url.Parse(publicBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("create Bot comment mention notifier: public base URL %q is invalid", publicBaseURL)
		}
	}
	return &BotCommentMentionNotifier{sender: sender, publicBaseURL: publicBaseURL}, nil
}

func (n *BotCommentMentionNotifier) AppID() string { return n.sender.AppID() }
func (n *BotCommentMentionNotifier) VerifyCommentMention(ctx context.Context, id string) error {
	return n.sender.VerifyMessage(ctx, id)
}
func (n *BotCommentMentionNotifier) NotifyCommentMention(ctx context.Context, input CommentMentionNotification) (string, error) {
	card, err := formatCommentMentionCard(input, n.commentURL(input))
	if err != nil {
		return "", fmt.Errorf("build comment notification card: %w", err)
	}
	digest := sha256.Sum256([]byte(input.CommentID + "\x00" + input.Recipient.Email))
	return n.sender.SendCardToEmail(ctx, input.Recipient.Email, input.AuthorEmail, card, "okr-cmt-"+hex.EncodeToString(digest[:16]))
}

func (n *BotCommentMentionNotifier) commentURL(input CommentMentionNotification) string {
	if n.publicBaseURL == "" {
		return ""
	}
	parsed, _ := url.Parse(n.publicBaseURL)
	params := url.Values{
		"comment_id": {input.CommentID},
		"quarter":    {input.Quarter},
		"tab":        {input.Tab},
	}
	if input.Week != "" {
		params.Set("week", input.Week)
	}
	if input.PlanID != "" {
		params.Set("plan_id", input.PlanID)
	}
	if input.AlignmentID != "" {
		params.Set("region", input.RegionCode)
		parsed.Fragment = "/biz-okr?" + params.Encode()
	} else {
		parsed.Fragment = "/weekly-report?" + params.Encode()
	}
	return parsed.String()
}

func formatCommentMentionCard(input CommentMentionNotification, link string) (string, error) {
	week := input.Week
	if week == "" {
		if input.AlignmentID != "" {
			week = input.Quarter
		} else {
			week = "无周次（Biz OKR Plan）"
		}
	}
	objective := input.ObjectiveTitle
	if objective == "" {
		objective = "未指定"
	}
	kr := input.KRTitle
	if kr == "" {
		kr = "未指定"
	}
	original := input.OriginalText
	if original == "" {
		original = "未提供"
	}
	contextLines := []string{
		"页面：" + commentSourceTabLabel(input.Tab),
		"周期：" + week,
		"O：" + objective,
		"KR：" + kr,
	}
	if input.PointTitle != "" {
		contextLines = append(contextLines, pointKindLabel(input.PointKind)+"："+input.PointTitle)
	}
	if input.PlanTitle != "" {
		contextLines = append(contextLines[:2], append([]string{"Plan：" + input.PlanTitle}, contextLines[2:]...)...)
	}
	if input.AlignmentID != "" {
		contextLines = append(contextLines[:2], append([]string{"区域：" + strings.ToUpper(input.RegionCode)}, contextLines[2:]...)...)
	}
	for index := range contextLines {
		contextLines[index] = escapeCardMarkdown(contextLines[index])
	}

	elements := []any{
		map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag": "lark_md", "content": "**原文：**" + escapeCardMarkdown(original) + "\n**评论：**" + escapeCardMarkdown(input.Content), "lines": 6,
			},
		},
		map[string]any{
			"tag": "collapsible_panel", "expanded": false, "background_color": "grey-50",
			"border":  map[string]any{"color": "grey-100", "corner_radius": "8px"},
			"padding": "8px",
			"header": map[string]any{
				"title": map[string]string{"tag": "plain_text", "content": "查看评论上下文"},
			},
			"elements": []any{
				map[string]any{
					"tag":  "div",
					"text": map[string]any{"tag": "lark_md", "content": strings.Join(contextLines, "\n")},
				},
			},
		},
	}
	if link != "" {
		elements = append(elements, map[string]any{
			"tag": "button", "type": "primary_filled", "size": "small", "width": "fill",
			"text":      map[string]string{"tag": "plain_text", "content": "查看并回复"},
			"behaviors": []any{map[string]string{"type": "open_url", "default_url": link}},
		})
	}
	subtitle := input.AuthorName + " @ 了你"
	if input.Reason == "owner" {
		subtitle = fmt.Sprintf("%s 评论了你负责的%s", input.AuthorName, commentOwnerLevelLabel(input.OwnerLevel, input.PointKind))
	} else if input.Reason == "mention_and_owner" {
		subtitle = fmt.Sprintf("%s 评论了你负责的%s，并 @ 了你", input.AuthorName, commentOwnerLevelLabel(input.OwnerLevel, input.PointKind))
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"update_multi": true, "width_mode": "compact",
			"summary": map[string]string{"content": subtitle + "：" + input.Content},
		},
		"header": map[string]any{
			"title":    map[string]string{"tag": "plain_text", "content": "OKR 评论"},
			"subtitle": map[string]string{"tag": "plain_text", "content": subtitle},
			"template": "blue",
			"icon":     map[string]string{"tag": "standard_icon", "token": "lark-logo_colorful"},
		},
		"body": map[string]any{
			"direction": "vertical", "padding": "12px 12px 16px 12px", "vertical_spacing": "8px", "elements": elements,
		},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func commentOwnerLevelLabel(level string, kind domain.PointKind) string {
	switch level {
	case "point":
		return pointKindLabel(kind)
	case "kr":
		return "KR"
	case "objective":
		return "O"
	default:
		return "OKR"
	}
}

func escapeCardMarkdown(value string) string {
	return strings.NewReplacer(
		"&", "&#38;", "<", "&#60;", ">", "&#62;", "*", "&#42;", "~", "&#126;",
		"[", "&#91;", "]", "&#93;", "(", "&#40;", ")", "&#41;", "#", "&#35;",
		":", "&#58;", "_", "&#95;",
	).Replace(value)
}

func commentSourceTabLabel(tab string) string {
	switch tab {
	case commentSourceTabOKRPlan:
		return "Biz OKR Plan"
	case commentSourceTabReviewFill:
		return "Review 填写"
	case commentSourceTabReviewMeeting:
		return "Review 会议"
	case commentSourceTabWeeklyMeeting:
		return "周报会议"
	case commentSourceTabRegionalAlignment:
		return "区域 OKR 对齐"
	default:
		return "周报填写"
	}
}

type commentHierarchy struct {
	ObjectiveID    string
	ObjectiveTitle string
	KRID           string
	KRTitle        string
	PointID        string
	PointKind      domain.PointKind
	PointTitle     string
	TargetText     string
}

func pointKindLabel(kind domain.PointKind) string {
	if kind == domain.PointKindStrategy {
		return "策略具体 KR"
	}
	if kind == domain.PointKindProduct {
		return "产品具体 KR"
	}
	return "具体 KR"
}

func (service *Service) commentMentionNotification(ctx context.Context, row domain.PageComment, sourceTab string) (CommentMentionNotification, error) {
	result := CommentMentionNotification{
		CommentID: row.ID, AuthorName: row.AuthorName, Quarter: row.Quarter,
		Week: row.Week, PlanID: row.PlanID, AlignmentID: row.AlignmentID, RegionCode: row.RegionCode, Content: row.Content,
		Tab: sourceTab,
	}
	if row.AlignmentID != "" {
		if result.Tab == "" {
			result.Tab = commentSourceTabRegionalAlignment
		}
	} else if row.PlanID != "" {
		if result.Tab == "" {
			result.Tab = commentSourceTabOKRPlan
		}
		var plan domain.OKRPlan
		if err := service.db.WithContext(ctx).First(&plan, "id = ?", row.PlanID).Error; err != nil {
			return CommentMentionNotification{}, fmt.Errorf("read comment Plan %s: %w", row.PlanID, err)
		}
		result.PlanTitle = plan.Title
	} else {
		var week domain.WeeklyReportWeek
		if err := service.db.WithContext(ctx).First(&week, "quarter = ? AND week = ?", row.Quarter, row.Week).Error; err != nil {
			return CommentMentionNotification{}, fmt.Errorf("read comment week %s/%s: %w", row.Quarter, row.Week, err)
		}
		if result.Tab == "" {
			result.Tab = commentSourceTabWeeklyFill
			if week.TemplateKey == domain.WeekTemplateOKRPreview {
				result.Tab = commentSourceTabReviewFill
			}
		}
	}

	var found commentHierarchy
	db := service.db.WithContext(ctx)
	scope := "objective.quarter = ? AND objective.plan_id = ?"
	scopeArgs := []any{row.Quarter, row.PlanID}
	var query *gorm.DB
	switch row.TargetType {
	case "objective":
		query = db.Table("okr_workspace_objective AS objective").
			Select("objective.id AS objective_id, objective.title AS objective_title, '' AS kr_id, '' AS kr_title, objective.title AS target_text").
			Where("objective.id = ? AND "+scope, append([]any{row.TargetID}, scopeArgs...)...)
	case "kr":
		query = db.Table("okr_workspace_kr AS kr").
			Select("objective.id AS objective_id, objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, kr.title AS target_text").
			Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
			Where("kr.id = ? AND "+scope, append([]any{row.TargetID}, scopeArgs...)...)
	case "metric":
		if row.PlanID == "" {
			weekly, ok, err := service.weeklyMetricCommentHierarchy(ctx, row)
			if err != nil {
				return CommentMentionNotification{}, err
			}
			if ok {
				found = weekly
				break
			}
		}
		query = db.Table("okr_workspace_metric AS metric").
			Select("objective.id AS objective_id, objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, metric.text AS target_text").
			Joins("JOIN okr_workspace_kr AS kr ON kr.id = metric.kr_id").
			Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
			Where("metric.id = ? AND "+scope, append([]any{row.TargetID}, scopeArgs...)...)
	case "point":
		query = db.Table("okr_workspace_point AS point").
			Select("objective.id AS objective_id, objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, point.id AS point_id, point.kind AS point_kind, point.title AS point_title, point.title AS target_text").
			Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
			Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
			Where("point.id = ? AND "+scope, append([]any{row.TargetID}, scopeArgs...)...)
	case "entry":
		query = db.Table("okr_workspace_progress AS progress").
			Select("objective.id AS objective_id, objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, point.id AS point_id, point.kind AS point_kind, point.title AS point_title, progress.text AS target_text").
			Joins("JOIN okr_workspace_point AS point ON point.id = progress.point_id").
			Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
			Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
			Where("progress.id = ? AND progress.week = ? AND "+scope, append([]any{row.TargetID, row.Week}, scopeArgs...)...)
	}
	if row.TargetType == "alignment_item" {
		found.TargetText = row.TargetTitle
	}
	if query != nil {
		if err := query.Take(&found).Error; err != nil && err != gorm.ErrRecordNotFound {
			return CommentMentionNotification{}, fmt.Errorf("resolve comment O/KR context: %w", err)
		}
	}
	result.ObjectiveID, result.ObjectiveTitle, result.KRID, result.KRTitle = found.ObjectiveID, found.ObjectiveTitle, found.KRID, found.KRTitle
	result.PointID, result.PointKind, result.PointTitle = found.PointID, found.PointKind, found.PointTitle
	if row.TargetType == "objective" && result.ObjectiveTitle == "" {
		result.ObjectiveTitle = row.TargetTitle
	}
	if row.TargetType == "kr" && result.KRTitle == "" {
		result.KRTitle = row.TargetTitle
	}
	if strings.TrimSpace(row.SelectedText) != "" {
		result.OriginalText = row.SelectedText
	}
	if result.OriginalText == "" && strings.TrimSpace(found.TargetText) != "" {
		result.OriginalText = found.TargetText
	}
	if result.OriginalText == "" && strings.TrimSpace(row.TargetTitle) != "" {
		result.OriginalText = row.TargetTitle
	}
	if result.OriginalText == "" && strings.TrimSpace(result.PlanTitle) != "" {
		result.OriginalText = result.PlanTitle
	}
	return result, nil
}

// Weekly core metrics can be seeded for a single week without creating a
// stable KRMetric row, and existing metric IDs can carry week-specific text.
// Resolve that visible source before falling back to the definition table.
func (service *Service) weeklyMetricCommentHierarchy(ctx context.Context, row domain.PageComment) (commentHierarchy, bool, error) {
	var cores []domain.WeeklyKRCore
	if err := service.db.WithContext(ctx).Where("week = ?", row.Week).Find(&cores).Error; err != nil {
		return commentHierarchy{}, false, fmt.Errorf("list weekly metrics for comment: %w", err)
	}
	for _, core := range cores {
		for _, metric := range core.Metrics {
			if metric.ID != row.TargetID {
				continue
			}
			var found commentHierarchy
			err := service.db.WithContext(ctx).Table("okr_workspace_kr AS kr").
				Select("objective.id AS objective_id, objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title").
				Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
				Where("kr.id = ? AND objective.quarter = ? AND objective.plan_id = ''", core.KRID, row.Quarter).
				Take(&found).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commentHierarchy{}, false, nil
			}
			if err != nil {
				return commentHierarchy{}, false, fmt.Errorf("resolve weekly metric comment hierarchy: %w", err)
			}
			found.TargetText = metric.Text
			return found, true, nil
		}
	}
	return commentHierarchy{}, false, nil
}
