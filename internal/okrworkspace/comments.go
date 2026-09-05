package okrworkspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

const (
	maxCommentLength        = 2000
	maxSelectedTextLength   = 500
	maxSelectionContextSize = 120
)

type CommentView struct {
	ID              string        `json:"id"`
	ParentID        string        `json:"parent_id,omitempty"`
	TargetType      string        `json:"target_type"`
	TargetID        string        `json:"target_id,omitempty"`
	TargetTitle     string        `json:"target_title,omitempty"`
	SelectedText    string        `json:"selected_text,omitempty"`
	SelectionStart  int           `json:"selection_start,omitempty"`
	SelectionEnd    int           `json:"selection_end,omitempty"`
	SelectionPrefix string        `json:"selection_prefix,omitempty"`
	SelectionSuffix string        `json:"selection_suffix,omitempty"`
	AuthorOpenID    string        `json:"author_open_id,omitempty"`
	AuthorUnionID   string        `json:"author_union_id,omitempty"`
	AuthorName      string        `json:"author_name"`
	Content         string        `json:"content"`
	Todo            bool          `json:"todo"`
	Resolved        bool          `json:"resolved"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	Replies         []CommentView `json:"replies"`
}

type CommentList struct {
	Quarter  string        `json:"quarter"`
	Week     string        `json:"week"`
	Count    int           `json:"count"`
	Comments []CommentView `json:"comments"`
}

type CreateCommentInput struct {
	Quarter         string `json:"quarter"`
	Week            string `json:"week"`
	ParentID        string `json:"parent_id"`
	TargetType      string `json:"target_type"`
	TargetID        string `json:"target_id"`
	TargetTitle     string `json:"target_title"`
	SelectedText    string `json:"selected_text"`
	SelectionStart  int    `json:"selection_start"`
	SelectionEnd    int    `json:"selection_end"`
	SelectionPrefix string `json:"selection_prefix"`
	SelectionSuffix string `json:"selection_suffix"`
	AuthorOpenID    string `json:"author_open_id"`
	AuthorUnionID   string `json:"author_union_id"`
	AuthorName      string `json:"author_name"`
	Content         string `json:"content"`
}

type UpdateCommentInput struct {
	// Pointers distinguish an omitted field from an explicit false value. Text
	// edits, To do toggles and resolution are independent comment actions.
	Content  *string `json:"content"`
	Todo     *bool   `json:"todo"`
	Resolved *bool   `json:"resolved"`
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
		Where("quarter = ? AND week = ?", quarter, week).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return CommentList{}, fmt.Errorf("list page comments: %w", err)
	}
	return buildCommentList(quarter, week, rows), nil
}

func (service *Service) CreateComment(ctx context.Context, input CreateCommentInput) (CommentView, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Week = strings.TrimSpace(input.Week)
	input.ParentID = strings.TrimSpace(input.ParentID)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.TargetTitle = strings.TrimSpace(input.TargetTitle)
	input.SelectionPrefix = strings.TrimSpace(input.SelectionPrefix)
	input.SelectionSuffix = strings.TrimSpace(input.SelectionSuffix)
	input.AuthorOpenID = strings.TrimSpace(input.AuthorOpenID)
	input.AuthorUnionID = strings.TrimSpace(input.AuthorUnionID)
	input.AuthorName = strings.TrimSpace(input.AuthorName)
	input.Content = strings.TrimSpace(input.Content)
	if input.Quarter == "" {
		return CommentView{}, fmt.Errorf("quarter is required")
	}
	if !weekPattern.MatchString(input.Week) {
		return CommentView{}, fmt.Errorf("week must use YYYY-Www")
	}
	if err := service.requireOpenWeek(ctx, input.Quarter, input.Week); err != nil {
		return CommentView{}, err
	}
	if input.Content == "" {
		return CommentView{}, fmt.Errorf("comment content is required")
	}
	if len([]rune(input.Content)) > maxCommentLength {
		return CommentView{}, fmt.Errorf("comment content exceeds %d characters", maxCommentLength)
	}
	if input.TargetType == "" {
		input.TargetType = "page"
	}
	if input.TargetType != "page" && input.TargetType != "kr" && input.TargetType != "metric" && input.TargetType != "point" && input.TargetType != "entry" && input.TargetType != "follow_up" {
		return CommentView{}, fmt.Errorf("target_type must be page, kr, metric, point, entry or follow_up")
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
	if input.ParentID == "" && input.TargetType == "follow_up" {
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
		Quarter:         input.Quarter,
		Week:            input.Week,
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
			if parent.Quarter != input.Quarter || parent.Week != input.Week {
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
		return tx.Create(&row).Error
	}); err != nil {
		return CommentView{}, fmt.Errorf("create page comment: %w", err)
	}
	return commentView(row), nil
}

func (service *Service) UpdateComment(ctx context.Context, id string, input UpdateCommentInput) (CommentView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CommentView{}, fmt.Errorf("comment_id is required")
	}
	if input.Content == nil && input.Todo == nil && input.Resolved == nil {
		return CommentView{}, fmt.Errorf("content, todo or resolved is required")
	}

	var row domain.PageComment
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrNotFound
			}
			return err
		}
		updates := map[string]any{}
		if input.Content != nil {
			content := strings.TrimSpace(*input.Content)
			if content == "" {
				return fmt.Errorf("comment content is required")
			}
			if len([]rune(content)) > maxCommentLength {
				return fmt.Errorf("comment content exceeds %d characters", maxCommentLength)
			}
			row.Content = content
			updates["content"] = content
		}
		if input.Todo != nil {
			if row.ParentID != "" {
				return fmt.Errorf("only a top-level comment can be marked as todo")
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
		return tx.Model(&domain.PageComment{}).Where("id = ?", id).Updates(updates).Error
	})
	if err != nil {
		return CommentView{}, fmt.Errorf("update page comment: %w", err)
	}
	return commentView(row), nil
}

func (service *Service) DeleteComment(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("comment_id is required")
	}
	if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row domain.PageComment
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrNotFound
			}
			return err
		}
		if row.ParentID == "" {
			if err := tx.Where("parent_id = ?", row.ID).Delete(&domain.PageComment{}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&domain.PageComment{}, "id = ?", row.ID).Error
	}); err != nil {
		return fmt.Errorf("delete page comment: %w", err)
	}
	return nil
}

func buildCommentList(quarter, week string, rows []domain.PageComment) CommentList {
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
			roots[index].Replies = append(roots[index].Replies, commentView(row))
		}
	}
	return CommentList{Quarter: quarter, Week: week, Count: len(rows), Comments: roots}
}

func commentView(row domain.PageComment) CommentView {
	return CommentView{
		ID: row.ID, ParentID: row.ParentID, TargetType: row.TargetType,
		TargetID: row.TargetID, TargetTitle: row.TargetTitle,
		SelectedText: row.SelectedText, SelectionStart: row.SelectionStart, SelectionEnd: row.SelectionEnd,
		SelectionPrefix: row.SelectionPrefix, SelectionSuffix: row.SelectionSuffix,
		AuthorOpenID: row.AuthorOpenID, AuthorUnionID: row.AuthorUnionID, AuthorName: row.AuthorName,
		Content: row.Content, Todo: row.Todo, Resolved: row.Resolved,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Replies: []CommentView{},
	}
}

func newCommentID(now time.Time, input CreateCommentInput) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s", now.UnixNano(), input.ParentID, input.AuthorOpenID, input.Content)))
	return fmt.Sprintf("comment_%x", sum[:10])
}
