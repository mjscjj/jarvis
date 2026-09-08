package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

const (
	commentSourceTabOKRPlan       = "okr-plan"
	commentSourceTabReviewFill    = "review-fill"
	commentSourceTabReviewMeeting = "review-meeting"
	commentSourceTabWeeklyFill    = "weekly-fill"
	commentSourceTabWeeklyMeeting = "weekly-meeting"
)

func validCommentSourceTab(value string) bool {
	switch value {
	case commentSourceTabOKRPlan, commentSourceTabReviewFill, commentSourceTabReviewMeeting, commentSourceTabWeeklyFill, commentSourceTabWeeklyMeeting:
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
	CommentID      string
	AuthorName     string
	AuthorEmail    string
	Quarter        string
	Week           string
	PlanID         string
	PlanTitle      string
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
	NotifyCommentMention(context.Context, CommentMentionNotification) error
}

type commentBroadcastSender interface {
	SendTextToMainAppUser(context.Context, string, string, string, string, string) error
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

func (n *BotCommentMentionNotifier) NotifyCommentMention(ctx context.Context, input CommentMentionNotification) error {
	text := formatCommentMentionNotification(input, n.commentURL(input))
	digest := sha256.Sum256([]byte(input.CommentID + "\x00" + input.Recipient.OpenID + "\x00" + text))
	idempotencyKey := "okr-cmt-" + hex.EncodeToString(digest[:16])
	if err := n.sender.SendTextToMainAppUser(ctx, input.Recipient.OpenID, input.Recipient.Name, input.AuthorEmail, text, idempotencyKey); err != nil {
		return err
	}
	return nil
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
	parsed.Fragment = "/weekly-report?" + params.Encode()
	return parsed.String()
}

func formatCommentMentionNotification(input CommentMentionNotification, link string) string {
	headline := fmt.Sprintf("%s 在 Jarvis OKR 评论中 @ 了你", input.AuthorName)
	week := input.Week
	if week == "" {
		week = "无周次（Biz OKR Plan）"
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
	lines := []string{
		headline,
		"",
		"页面：" + commentSourceTabLabel(input.Tab),
		"周次：" + week,
		"O：" + objective,
		"KR：" + kr,
	}
	if input.PointTitle != "" {
		lines = append(lines, pointKindLabel(input.PointKind)+"："+input.PointTitle)
	}
	lines = append(lines, "对应原文：", original, "", "评论：", input.Content)
	if input.PlanTitle != "" {
		lines = append(lines[:2], append([]string{"Plan：" + input.PlanTitle}, lines[2:]...)...)
	}
	if link != "" {
		lines = append(lines, "", "查看并回复："+link)
	}
	return strings.Join(lines, "\n")
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
	default:
		return "周报填写"
	}
}

func (service *Service) notifyCreatedComment(ctx context.Context, row domain.PageComment, authorEmail string, mentions []CommentMention, sourceTab string) []string {
	if len(mentions) == 0 {
		return nil
	}
	return service.notifyCommentRecipients(ctx, row, authorEmail, mentions, sourceTab)
}

type commentHierarchy struct {
	ObjectiveTitle string
	KRID           string
	KRTitle        string
	PointID        string
	PointKind      domain.PointKind
	PointTitle     string
	TargetText     string
}

func (service *Service) notifyCommentRecipients(ctx context.Context, row domain.PageComment, authorEmail string, mentions []CommentMention, sourceTab string) []string {
	notification, err := service.commentMentionNotification(ctx, row, sourceTab)
	if err != nil {
		return []string{fmt.Sprintf("生成飞书 Bot 评论提醒失败：%v", err)}
	}
	notification.AuthorEmail = strings.TrimSpace(authorEmail)
	errorsByRecipient := make([]string, 0)
	for _, mention := range mentions {
		if service.commentNotifier == nil {
			return []string{"飞书 Bot 评论提醒未配置"}
		}
		notification.Recipient = mention
		if err := service.commentNotifier.NotifyCommentMention(ctx, notification); err != nil {
			errorsByRecipient = append(errorsByRecipient, fmt.Sprintf("提醒 %s 失败：%v", mention.Name, err))
		}
	}
	return errorsByRecipient
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
		Week: row.Week, PlanID: row.PlanID, Content: row.Content,
		Tab: sourceTab,
	}
	if row.PlanID != "" {
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
			Select("objective.title AS objective_title, '' AS kr_id, '' AS kr_title, objective.title AS target_text").
			Where("objective.id = ? AND "+scope, append([]any{row.TargetID}, scopeArgs...)...)
	case "kr":
		query = db.Table("okr_workspace_kr AS kr").
			Select("objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, kr.title AS target_text").
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
			Select("objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, metric.text AS target_text").
			Joins("JOIN okr_workspace_kr AS kr ON kr.id = metric.kr_id").
			Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
			Where("metric.id = ? AND "+scope, append([]any{row.TargetID}, scopeArgs...)...)
	case "point":
		query = db.Table("okr_workspace_point AS point").
			Select("objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, point.id AS point_id, point.kind AS point_kind, point.title AS point_title, point.title AS target_text").
			Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
			Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
			Where("point.id = ? AND "+scope, append([]any{row.TargetID}, scopeArgs...)...)
	case "entry":
		query = db.Table("okr_workspace_progress AS progress").
			Select("objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title, point.id AS point_id, point.kind AS point_kind, point.title AS point_title, progress.text AS target_text").
			Joins("JOIN okr_workspace_point AS point ON point.id = progress.point_id").
			Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
			Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
			Where("progress.id = ? AND progress.week = ? AND "+scope, append([]any{row.TargetID, row.Week}, scopeArgs...)...)
	}
	if query != nil {
		if err := query.Take(&found).Error; err != nil && err != gorm.ErrRecordNotFound {
			return CommentMentionNotification{}, fmt.Errorf("resolve comment O/KR context: %w", err)
		}
	}
	result.ObjectiveTitle, result.KRID, result.KRTitle = found.ObjectiveTitle, found.KRID, found.KRTitle
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
				Select("objective.title AS objective_title, kr.id AS kr_id, kr.title AS kr_title").
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
