package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
)

const (
	maxCommentLength        = 2000
	maxSelectedTextLength   = 500
	maxSelectionContextSize = 120
	maxCommentMentions      = 10
	maxCommentImages        = 9
)

type CommentMention = domain.CommentMention
type CommentImage = domain.ImageRef

type CommentView struct {
	ID                 string                `json:"id"`
	Version            int32                 `json:"version"`
	DeleteToken        string                `json:"delete_token"`
	PlanID             string                `json:"plan_id,omitempty"`
	ParentID           string                `json:"parent_id,omitempty"`
	TargetType         string                `json:"target_type"`
	TargetID           string                `json:"target_id,omitempty"`
	TargetTitle        string                `json:"target_title,omitempty"`
	SelectedText       string                `json:"selected_text,omitempty"`
	SelectionStart     int                   `json:"selection_start,omitempty"`
	SelectionEnd       int                   `json:"selection_end,omitempty"`
	SelectionPrefix    string                `json:"selection_prefix,omitempty"`
	SelectionSuffix    string                `json:"selection_suffix,omitempty"`
	AuthorOpenID       string                `json:"author_open_id,omitempty"`
	AuthorUnionID      string                `json:"author_union_id,omitempty"`
	AuthorName         string                `json:"author_name"`
	Content            string                `json:"content"`
	Mentions           []CommentMention      `json:"mentions"`
	Images             []CommentImage        `json:"images"`
	Notifications      []CommentDeliveryView `json:"notifications,omitempty"`
	NotificationErrors []string              `json:"notification_errors,omitempty"`
	Todo               bool                  `json:"todo"`
	Resolved           bool                  `json:"resolved"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	Replies            []CommentView         `json:"replies"`
	AlignmentID        string                `json:"alignment_id,omitempty"`
	RegionCode         string                `json:"region_code,omitempty"`
}

type CommentList struct {
	Quarter     string        `json:"quarter"`
	Week        string        `json:"week,omitempty"`
	PlanID      string        `json:"plan_id,omitempty"`
	AlignmentID string        `json:"alignment_id,omitempty"`
	RegionCode  string        `json:"region_code,omitempty"`
	Count       int           `json:"count"`
	Comments    []CommentView `json:"comments"`
}

type CreateCommentInput struct {
	Quarter     string `json:"quarter"`
	Week        string `json:"week"`
	PlanID      string `json:"plan_id"`
	AlignmentID string `json:"alignment_id"`
	RegionCode  string `json:"region_code"`
	// SourceTab is the page that created this comment. It is an execution
	// parameter for the immediate notification deep link, not comment state.
	SourceTab       string           `json:"source_tab"`
	ParentID        string           `json:"parent_id"`
	TargetType      string           `json:"target_type"`
	TargetID        string           `json:"target_id"`
	TargetTitle     string           `json:"target_title"`
	SelectedText    string           `json:"selected_text"`
	SelectionStart  int              `json:"selection_start"`
	SelectionEnd    int              `json:"selection_end"`
	SelectionPrefix string           `json:"selection_prefix"`
	SelectionSuffix string           `json:"selection_suffix"`
	AuthorOpenID    string           `json:"author_open_id"`
	AuthorUnionID   string           `json:"author_union_id"`
	AuthorName      string           `json:"author_name"`
	AuthorEmail     string           `json:"-"`
	Content         string           `json:"content"`
	Mentions        []CommentMention `json:"mentions"`
	Images          []CommentImage   `json:"images"`
}

type UpdateCommentInput struct {
	// Pointers distinguish an omitted field from an explicit false value. Text
	// edits, To do toggles and resolution are independent comment actions.
	Content         *string           `json:"content"`
	Mentions        *[]CommentMention `json:"mentions"`
	Images          *[]CommentImage   `json:"images"`
	Todo            *bool             `json:"todo"`
	Resolved        *bool             `json:"resolved"`
	ExpectedVersion int32             `json:"expected_version"`
}

type DeleteCommentInput struct {
	ExpectedVersion int32  `json:"expected_version"`
	DeleteToken     string `json:"delete_token"`
}

func (service *Service) Comments(ctx context.Context, quarter, week string) (CommentList, error) {
	quarter = strings.TrimSpace(quarter)
	week = strings.TrimSpace(week)
	if quarter == "" {
		return CommentList{}, fmt.Errorf("quarter is required")
	}
	if !weekPattern.MatchString(week) {
		return CommentList{}, fmt.Errorf("week must use YYYY-Www")
	}
	if err := service.requireOpenWeek(ctx, quarter, week); err != nil {
		return CommentList{}, err
	}
	var rows []domain.PageComment
	if err := service.db.WithContext(ctx).
		Where("quarter = ? AND week = ? AND plan_id = ''", quarter, week).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return CommentList{}, fmt.Errorf("list page comments: %w", err)
	}
	return service.buildCommentList(ctx, quarter, week, rows)
}

func (service *Service) PlanComments(ctx context.Context, planID string) (CommentList, error) {
	plan, err := service.commentPlan(ctx, planID)
	if err != nil {
		return CommentList{}, err
	}
	var rows []domain.PageComment
	if err := service.db.WithContext(ctx).
		Where("plan_id = ?", plan.ID).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return CommentList{}, fmt.Errorf("list plan comments: %w", err)
	}
	result, err := service.buildCommentList(ctx, plan.Quarter, "", rows)
	if err != nil {
		return CommentList{}, err
	}
	result.PlanID = plan.ID
	return result, nil
}

func (service *Service) CreatePlanComment(ctx context.Context, planID string, input CreateCommentInput) (CommentView, error) {
	input.PlanID = strings.TrimSpace(planID)
	input.Quarter = ""
	input.Week = ""
	input.SourceTab = commentSourceTabOKRPlan
	return service.CreateComment(ctx, input)
}

func (service *Service) AlignmentComments(ctx context.Context, quarter, region string) (CommentList, error) {
	alignment, _, err := service.ensureRegionalAlignment(ctx, quarter, region, "")
	if err != nil {
		return CommentList{}, err
	}
	region = strings.ToLower(strings.TrimSpace(region))
	var rows []domain.PageComment
	if err := service.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ?", alignment.ID, region).Order("created_at ASC").Find(&rows).Error; err != nil {
		return CommentList{}, fmt.Errorf("list regional alignment comments: %w", err)
	}
	result, err := service.buildCommentList(ctx, alignment.Quarter, "", rows)
	if err != nil {
		return CommentList{}, err
	}
	result.AlignmentID, result.RegionCode = alignment.ID, region
	return result, nil
}

func (service *Service) CreateAlignmentComment(ctx context.Context, quarter, region string, input CreateCommentInput) (CommentView, error) {
	alignment, _, err := service.ensureRegionalAlignment(ctx, quarter, region, input.AuthorOpenID)
	if err != nil {
		return CommentView{}, err
	}
	input.AlignmentID, input.RegionCode, input.Quarter, input.Week, input.PlanID = alignment.ID, strings.ToLower(strings.TrimSpace(region)), alignment.Quarter, "", ""
	input.SourceTab = commentSourceTabRegionalAlignment
	return service.CreateComment(ctx, input)
}

func (service *Service) CreateComment(ctx context.Context, input CreateCommentInput) (CommentView, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Week = strings.TrimSpace(input.Week)
	input.PlanID = strings.TrimSpace(input.PlanID)
	input.AlignmentID = strings.TrimSpace(input.AlignmentID)
	input.RegionCode = strings.ToLower(strings.TrimSpace(input.RegionCode))
	input.SourceTab = strings.TrimSpace(input.SourceTab)
	input.ParentID = strings.TrimSpace(input.ParentID)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.TargetTitle = strings.TrimSpace(input.TargetTitle)
	input.SelectionPrefix = strings.TrimSpace(input.SelectionPrefix)
	input.SelectionSuffix = strings.TrimSpace(input.SelectionSuffix)
	input.AuthorOpenID = strings.TrimSpace(input.AuthorOpenID)
	input.AuthorUnionID = strings.TrimSpace(input.AuthorUnionID)
	input.AuthorName = strings.TrimSpace(input.AuthorName)
	input.AuthorEmail = strings.TrimSpace(input.AuthorEmail)
	input.Content = strings.TrimSpace(input.Content)
	images, err := normalizeCommentImages(input.Images)
	if err != nil {
		return CommentView{}, err
	}
	input.Images = images
	if input.SourceTab != "" && !validCommentSourceTab(input.SourceTab) {
		return CommentView{}, fmt.Errorf("unsupported comment source_tab %q", input.SourceTab)
	}
	if input.AlignmentID != "" {
		var alignment domain.RegionalAlignment
		if err := service.db.WithContext(ctx).First(&alignment, "id = ?", input.AlignmentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return CommentView{}, ErrNotFound
			}
			return CommentView{}, err
		}
		if _, ok := regionalAlignmentRegions[input.RegionCode]; !ok {
			return CommentView{}, fmt.Errorf("unsupported region %q", input.RegionCode)
		}
		input.Quarter, input.Week, input.PlanID = alignment.Quarter, "", ""
	} else if input.PlanID != "" {
		plan, err := service.commentPlan(ctx, input.PlanID)
		if err != nil {
			return CommentView{}, err
		}
		input.Quarter, input.Week = plan.Quarter, ""
	} else {
		if input.Quarter == "" {
			return CommentView{}, fmt.Errorf("quarter is required")
		}
		if !weekPattern.MatchString(input.Week) {
			return CommentView{}, fmt.Errorf("week must use YYYY-Www")
		}
		if err := service.requireOpenWeek(ctx, input.Quarter, input.Week); err != nil {
			return CommentView{}, err
		}
	}
	if input.Content == "" && len(input.Images) == 0 {
		return CommentView{}, fmt.Errorf("comment content or image is required")
	}
	if len([]rune(input.Content)) > maxCommentLength {
		return CommentView{}, fmt.Errorf("comment content exceeds %d characters", maxCommentLength)
	}
	mentions, err := normalizeCommentMentions(input.Content, input.Mentions)
	if err != nil {
		return CommentView{}, err
	}
	if err := service.verifyPeople(ctx, mentions); err != nil {
		return CommentView{}, err
	}
	input.Mentions = mentions
	if input.TargetType == "" {
		input.TargetType = "page"
	}
	if input.TargetType != "page" && input.TargetType != "objective" && input.TargetType != "kr" && input.TargetType != "metric" && input.TargetType != "point" && input.TargetType != "entry" && input.TargetType != "follow_up" && input.TargetType != "alignment_item" {
		return CommentView{}, fmt.Errorf("unsupported target_type")
	}
	if input.TargetType != "page" && input.TargetID == "" {
		return CommentView{}, fmt.Errorf("target_id is required for content comments")
	}
	if input.SelectedText != "" {
		if strings.TrimSpace(input.SelectedText) == "" {
			return CommentView{}, fmt.Errorf("selected_text must contain visible text")
		}
		if len([]rune(input.SelectedText)) > maxSelectedTextLength {
			return CommentView{}, fmt.Errorf("selected_text exceeds %d characters", maxSelectedTextLength)
		}
		if input.SelectionStart < 0 || input.SelectionEnd <= input.SelectionStart {
			return CommentView{}, fmt.Errorf("selection range is invalid")
		}
		if input.TargetType == "page" {
			return CommentView{}, fmt.Errorf("page comments cannot contain a text selection")
		}
	} else {
		input.SelectionStart, input.SelectionEnd = 0, 0
		input.SelectionPrefix, input.SelectionSuffix = "", ""
	}
	if len([]rune(input.SelectionPrefix)) > maxSelectionContextSize || len([]rune(input.SelectionSuffix)) > maxSelectionContextSize {
		return CommentView{}, fmt.Errorf("selection context exceeds %d characters", maxSelectionContextSize)
	}
	if input.ParentID == "" && input.PlanID != "" {
		if err := service.requirePlanCommentTarget(ctx, input.PlanID, &input); err != nil {
			return CommentView{}, err
		}
	}
	if input.ParentID == "" && input.PlanID == "" && input.TargetType == "follow_up" {
		var followUp domain.FollowUpItem
		if err := service.db.WithContext(ctx).First(&followUp, "id = ?", input.TargetID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return CommentView{}, ErrNotFound
			}
			return CommentView{}, fmt.Errorf("get follow-up comment target: %w", err)
		}
		if followUp.Quarter != input.Quarter || followUp.Week != input.Week {
			return CommentView{}, fmt.Errorf("follow-up comment target scope does not match comment scope")
		}
	}
	if input.AuthorName == "" {
		input.AuthorName = "当前用户"
	}

	now := time.Now().UTC()
	row := domain.PageComment{
		ID:              newCommentID(now, input),
		Version:         1,
		Quarter:         input.Quarter,
		Week:            input.Week,
		PlanID:          input.PlanID,
		AlignmentID:     input.AlignmentID,
		RegionCode:      input.RegionCode,
		ParentID:        input.ParentID,
		TargetType:      input.TargetType,
		TargetID:        input.TargetID,
		TargetTitle:     input.TargetTitle,
		SelectedText:    input.SelectedText,
		SelectionStart:  input.SelectionStart,
		SelectionEnd:    input.SelectionEnd,
		SelectionPrefix: input.SelectionPrefix,
		SelectionSuffix: input.SelectionSuffix,
		AuthorOpenID:    input.AuthorOpenID,
		AuthorUnionID:   input.AuthorUnionID,
		AuthorName:      input.AuthorName,
		Content:         input.Content,
		Mentions:        input.Mentions,
		Images:          input.Images,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.ParentID != "" {
			var parent domain.PageComment
			if err := tx.First(&parent, "id = ?", input.ParentID).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					return ErrNotFound
				}
				return err
			}
			if parent.Quarter != input.Quarter || parent.Week != input.Week || parent.PlanID != input.PlanID || parent.AlignmentID != input.AlignmentID || parent.RegionCode != input.RegionCode {
				return fmt.Errorf("reply scope does not match parent comment")
			}
			if parent.ParentID != "" {
				row.ParentID = parent.ParentID
			}
			// A reply always belongs to the exact content thread selected by the
			// root comment. Never allow a client payload to move it elsewhere.
			row.TargetType, row.TargetID, row.TargetTitle = parent.TargetType, parent.TargetID, parent.TargetTitle
			row.SelectedText, row.SelectionStart, row.SelectionEnd = parent.SelectedText, parent.SelectionStart, parent.SelectionEnd
			row.SelectionPrefix, row.SelectionSuffix = parent.SelectionPrefix, parent.SelectionSuffix
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		txService := *service
		txService.db = tx
		return txService.prepareCommentDeliveries(ctx, row, input.AuthorEmail, input.AuthorUnionID, input.SourceTab)
	}); err != nil {
		return CommentView{}, fmt.Errorf("create page comment: %w", err)
	}
	view := commentView(row)
	view.DeleteToken, err = service.commentDeleteToken(ctx, row.ID)
	if err != nil {
		return CommentView{}, err
	}
	view.Notifications, err = service.commentDeliveries(ctx, row.ID)
	if err != nil {
		hlog.CtxErrorf(ctx, "comment saved but notification ledger failed comment=%s: %v", row.ID, err)
		view.NotificationErrors = []string{"评论已保存，通知状态暂时无法读取，请刷新查看"}
		return view, nil
	}
	view.NotificationErrors = deliveryWarnings(view.Notifications)
	return view, nil
}

func (service *Service) UpdateComment(ctx context.Context, id string, input UpdateCommentInput) (CommentView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CommentView{}, fmt.Errorf("comment_id is required")
	}
	if input.Content == nil && input.Mentions == nil && input.Images == nil && input.Todo == nil && input.Resolved == nil {
		return CommentView{}, fmt.Errorf("content, mentions, images, todo or resolved is required")
	}
	if input.ExpectedVersion <= 0 {
		return CommentView{}, fmt.Errorf("expected_version must be positive")
	}

	if input.Mentions != nil {
		if err := service.verifyPeople(ctx, *input.Mentions); err != nil {
			return CommentView{}, err
		}
	}
	var row domain.PageComment
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrNotFound
			}
			return err
		}
		if row.Version != input.ExpectedVersion {
			return ErrConflict
		}
		updates := map[string]any{}
		if input.Content != nil {
			content := strings.TrimSpace(*input.Content)
			if len([]rune(content)) > maxCommentLength {
				return fmt.Errorf("comment content exceeds %d characters", maxCommentLength)
			}
			row.Content = content
			updates["content"] = content
		}
		if input.Images != nil {
			images, err := normalizeCommentImages(*input.Images)
			if err != nil {
				return err
			}
			row.Images = images
			encoded, err := json.Marshal(images)
			if err != nil {
				return fmt.Errorf("encode comment images: %w", err)
			}
			updates["images"] = string(encoded)
		}
		if row.Content == "" && len(row.Images) == 0 {
			return fmt.Errorf("comment content or image is required")
		}
		if input.Mentions != nil {
			mentions, err := normalizeCommentMentions(row.Content, *input.Mentions)
			if err != nil {
				return err
			}
			row.Mentions = mentions
			encoded, err := json.Marshal(mentions)
			if err != nil {
				return fmt.Errorf("encode comment mentions: %w", err)
			}
			updates["mentions"] = string(encoded)
		} else if input.Content != nil {
			row.Mentions = commentMentionsPresentInContent(row.Content, row.Mentions)
			encoded, err := json.Marshal(row.Mentions)
			if err != nil {
				return fmt.Errorf("encode comment mentions: %w", err)
			}
			updates["mentions"] = string(encoded)
		}
		if input.Todo != nil {
			if row.ParentID != "" {
				return fmt.Errorf("only a top-level comment can be marked as todo")
			}
			if row.PlanID != "" {
				return fmt.Errorf("plan comments cannot be marked as todo")
			}
			row.Todo = *input.Todo
			updates["todo"] = row.Todo
		}
		if input.Resolved != nil {
			if row.ParentID != "" {
				return fmt.Errorf("only a top-level comment can be resolved")
			}
			row.Resolved = *input.Resolved
			updates["resolved"] = row.Resolved
		}
		row.UpdatedAt = time.Now().UTC()
		updates["updated_at"] = row.UpdatedAt
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&domain.PageComment{}).Where("id = ? AND version = ?", id, input.ExpectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		row.Version++
		return nil
	})
	if err != nil {
		return CommentView{}, fmt.Errorf("update page comment: %w", err)
	}
	view := commentView(row)
	view.DeleteToken, err = service.commentDeleteToken(ctx, row.ID)
	if err != nil {
		return CommentView{}, err
	}
	view.Notifications, err = service.commentDeliveries(ctx, row.ID)
	return view, err
}

func (service *Service) DeleteComment(ctx context.Context, id string, input DeleteCommentInput) error {
	id = strings.TrimSpace(id)
	input.DeleteToken = strings.TrimSpace(input.DeleteToken)
	if id == "" || input.ExpectedVersion <= 0 || input.DeleteToken == "" {
		return fmt.Errorf("comment_id, positive expected_version and delete_token are required")
	}
	if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.PageComment
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrNotFound
			}
			return err
		}
		if row.Version != input.ExpectedVersion {
			return ErrConflict
		}
		currentToken, err := commentDeleteTokenWithDB(tx, row)
		if err != nil {
			return err
		}
		if currentToken != input.DeleteToken {
			return ErrConflict
		}
		if row.ParentID == "" {
			if err := tx.Where("parent_id = ?", row.ID).Delete(&domain.PageComment{}).Error; err != nil {
				return err
			}
		}
		result := tx.Where("id = ? AND version = ?", row.ID, input.ExpectedVersion).Delete(&domain.PageComment{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	}); err != nil {
		return fmt.Errorf("delete page comment: %w", err)
	}
	return nil
}

func (service *Service) buildCommentList(ctx context.Context, quarter, week string, rows []domain.PageComment) (CommentList, error) {
	roots := make([]CommentView, 0)
	rootIndex := make(map[string]int)
	for _, row := range rows {
		if row.ParentID != "" {
			continue
		}
		rootIndex[row.ID] = len(roots)
		roots = append(roots, commentView(row))
	}
	for _, row := range rows {
		if row.ParentID == "" {
			continue
		}
		if index, ok := rootIndex[row.ParentID]; ok {
			reply := commentView(row)
			var err error
			reply.DeleteToken, err = deletionToken(row)
			if err != nil {
				return CommentList{}, err
			}
			roots[index].Replies = append(roots[index].Replies, reply)
		}
	}
	for index := range roots {
		thread := []domain.PageComment{}
		for _, row := range rows {
			if row.ID == roots[index].ID || row.ParentID == roots[index].ID {
				thread = append(thread, row)
			}
		}
		var err error
		roots[index].DeleteToken, err = deletionToken(thread)
		if err != nil {
			return CommentList{}, err
		}
	}
	for i := range roots {
		var err error
		roots[i].Notifications, err = service.commentDeliveries(ctx, roots[i].ID)
		if err != nil {
			return CommentList{}, err
		}
		for j := range roots[i].Replies {
			roots[i].Replies[j].Notifications, err = service.commentDeliveries(ctx, roots[i].Replies[j].ID)
			if err != nil {
				return CommentList{}, err
			}
		}
	}
	return CommentList{Quarter: quarter, Week: week, Count: len(rows), Comments: roots}, nil
}

func commentView(row domain.PageComment) CommentView {
	return CommentView{
		ID: row.ID, Version: row.Version, PlanID: row.PlanID, AlignmentID: row.AlignmentID, RegionCode: row.RegionCode, ParentID: row.ParentID, TargetType: row.TargetType,
		TargetID: row.TargetID, TargetTitle: row.TargetTitle,
		SelectedText: row.SelectedText, SelectionStart: row.SelectionStart, SelectionEnd: row.SelectionEnd,
		SelectionPrefix: row.SelectionPrefix, SelectionSuffix: row.SelectionSuffix,
		AuthorOpenID: row.AuthorOpenID, AuthorUnionID: row.AuthorUnionID, AuthorName: row.AuthorName,
		Content: row.Content, Mentions: append([]CommentMention(nil), row.Mentions...), Images: nonNilImages(row.Images), Todo: row.Todo, Resolved: row.Resolved,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Replies: []CommentView{},
	}
}

func (service *Service) commentDeleteToken(ctx context.Context, id string) (string, error) {
	var row domain.PageComment
	if err := service.db.WithContext(ctx).First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get comment deletion snapshot: %w", err)
	}
	return commentDeleteTokenWithDB(service.db.WithContext(ctx), row)
}

func commentDeleteTokenWithDB(db *gorm.DB, row domain.PageComment) (string, error) {
	if row.ParentID != "" {
		return deletionToken(row)
	}
	var thread []domain.PageComment
	if err := db.Where("id = ? OR parent_id = ?", row.ID, row.ID).
		Order("created_at ASC, id ASC").Find(&thread).Error; err != nil {
		return "", fmt.Errorf("load comment deletion snapshot: %w", err)
	}
	return deletionToken(thread)
}

func normalizeCommentImages(images []CommentImage) ([]CommentImage, error) {
	if len(images) > maxCommentImages {
		return nil, fmt.Errorf("comment images exceed %d files", maxCommentImages)
	}
	normalized := make([]CommentImage, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, image := range images {
		image.ID = strings.TrimSpace(image.ID)
		image.Name = strings.TrimSpace(image.Name)
		image.URL = strings.TrimSpace(image.URL)
		if image.ID == "" || image.Name == "" || !strings.HasPrefix(image.URL, "/okr-assets/") {
			return nil, fmt.Errorf("comment image must reference an uploaded OKR asset")
		}
		if image.Width < 0 || image.Width > 2000 {
			return nil, fmt.Errorf("comment image width must be between 0 and 2000")
		}
		if seen[image.ID] {
			continue
		}
		seen[image.ID] = true
		normalized = append(normalized, image)
	}
	return normalized, nil
}

func normalizeCommentMentions(content string, mentions []CommentMention) ([]CommentMention, error) {
	if len(mentions) > maxCommentMentions {
		return nil, fmt.Errorf("comment mentions exceed %d people", maxCommentMentions)
	}
	normalized := make([]CommentMention, 0, len(mentions))
	seenEmails := make(map[string]bool, len(mentions))
	seenNames := make(map[string]string, len(mentions))
	for _, mention := range mentions {
		mention.Email = domain.NormalizeEmail(mention.Email)
		mention.Name = strings.TrimSpace(mention.Name)
		if mention.Email == "" || mention.Name == "" {
			return nil, fmt.Errorf("每个 @ 人员需要完整企业邮箱和姓名，请刷新后重新选择")
		}
		if !validCommentMentionEmail(mention.Email) {
			return nil, fmt.Errorf("comment mention email %q is invalid", mention.Email)
		}
		if !strings.Contains(content, "@"+mention.Name) {
			return nil, fmt.Errorf("comment mention %q is not present in content", mention.Name)
		}
		if existing, ok := seenNames[mention.Name]; ok && existing != mention.Email {
			return nil, fmt.Errorf("comment cannot mention two people with the same display name %q", mention.Name)
		}
		seenNames[mention.Name] = mention.Email
		if seenEmails[mention.Email] {
			continue
		}
		seenEmails[mention.Email] = true
		normalized = append(normalized, mention)
	}
	return normalized, nil
}

func validCommentMentionEmail(value string) bool { return domain.ValidEmail(value) }

func commentMentionsPresentInContent(content string, mentions []CommentMention) []CommentMention {
	present := make([]CommentMention, 0, len(mentions))
	for _, mention := range mentions {
		if strings.Contains(content, "@"+mention.Name) {
			present = append(present, mention)
		}
	}
	return present
}

func (service *Service) commentPlan(ctx context.Context, planID string) (domain.OKRPlan, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return domain.OKRPlan{}, fmt.Errorf("plan id is required")
	}
	var plan domain.OKRPlan
	if err := service.db.WithContext(ctx).First(&plan, "id = ?", planID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.OKRPlan{}, ErrNotFound
		}
		return domain.OKRPlan{}, fmt.Errorf("get comment plan: %w", err)
	}
	return plan, nil
}

func (service *Service) requirePlanCommentTarget(ctx context.Context, planID string, input *CreateCommentInput) error {
	if input.TargetType == "page" {
		input.TargetID = planID
		return nil
	}
	if input.TargetType == "entry" || input.TargetType == "follow_up" {
		return fmt.Errorf("target_type %s is not supported for plan comments", input.TargetType)
	}

	db := service.db.WithContext(ctx)
	var count int64
	switch input.TargetType {
	case "objective":
		if err := db.Model(&domain.Objective{}).Where("id = ? AND plan_id = ?", input.TargetID, planID).Count(&count).Error; err != nil {
			return fmt.Errorf("validate plan objective comment target: %w", err)
		}
	case "kr":
		if err := db.Model(&domain.KR{}).Joins("JOIN okr_workspace_objective objective ON objective.id = okr_workspace_kr.objective_id").Where("okr_workspace_kr.id = ? AND objective.plan_id = ?", input.TargetID, planID).Count(&count).Error; err != nil {
			return fmt.Errorf("validate plan KR comment target: %w", err)
		}
	case "metric":
		if err := db.Model(&domain.KRMetric{}).Joins("JOIN okr_workspace_kr kr ON kr.id = okr_workspace_metric.kr_id").Joins("JOIN okr_workspace_objective objective ON objective.id = kr.objective_id").Where("okr_workspace_metric.id = ? AND objective.plan_id = ?", input.TargetID, planID).Count(&count).Error; err != nil {
			return fmt.Errorf("validate plan metric comment target: %w", err)
		}
	case "point":
		if err := db.Model(&domain.KRPoint{}).Joins("JOIN okr_workspace_kr kr ON kr.id = okr_workspace_point.kr_id").Joins("JOIN okr_workspace_objective objective ON objective.id = kr.objective_id").Where("okr_workspace_point.id = ? AND objective.plan_id = ?", input.TargetID, planID).Count(&count).Error; err != nil {
			return fmt.Errorf("validate plan point comment target: %w", err)
		}
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func newCommentID(now time.Time, input CreateCommentInput) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s", now.UnixNano(), input.ParentID, input.AuthorOpenID, input.Content)))
	return fmt.Sprintf("comment_%x", sum[:10])
}
