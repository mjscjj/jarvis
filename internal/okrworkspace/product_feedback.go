package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxProductFeedbackTitleLength   = 100
	maxProductFeedbackContentLength = 4000
	maxProductFeedbackReplyLength   = 2000
	maxProductFeedbackContextLength = 4096
)

var ErrFeedbackForbidden = errors.New("product feedback action is not allowed")

// FeedbackActor is the authenticated browser identity used by the Emily
// feedback surface. CanManage is presentation-domain access, not an Agent or
// Task permission.
type FeedbackActor struct {
	OpenID    string
	UnionID   string
	Email     string
	Name      string
	AvatarURL string
	CanManage bool
}

func (actor FeedbackActor) key() (string, error) {
	if value := strings.TrimSpace(actor.UnionID); value != "" {
		return "union:" + value, nil
	}
	if value := domain.NormalizeEmail(actor.Email); value != "" {
		return "email:" + strings.ToLower(value), nil
	}
	if value := strings.TrimSpace(actor.OpenID); value != "" {
		return "open:" + value, nil
	}
	return "", fmt.Errorf("feedback actor has no stable identity")
}

type FeedbackPersonView struct {
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type ProductFeedbackReplyView struct {
	ID        string             `json:"id"`
	Content   string             `json:"content"`
	Author    FeedbackPersonView `json:"author"`
	CreatedAt time.Time          `json:"created_at"`
}

type ProductFeedbackView struct {
	ID            string                     `json:"id"`
	Version       int32                      `json:"version"`
	Title         string                     `json:"title"`
	Content       string                     `json:"content"`
	Images        []domain.ImageRef          `json:"images"`
	SourceContext json.RawMessage            `json:"source_context"`
	Author        FeedbackPersonView         `json:"author"`
	Resolved      bool                       `json:"resolved"`
	ResolvedBy    *FeedbackPersonView        `json:"resolved_by,omitempty"`
	ResolvedAt    *time.Time                 `json:"resolved_at,omitempty"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at"`
	Replies       []ProductFeedbackReplyView `json:"replies"`
	PlusOnes      []FeedbackPersonView       `json:"plus_ones"`
	MyPlusOne     bool                       `json:"my_plus_one"`
	CanResolve    bool                       `json:"can_resolve"`
}

type ProductFeedbackList struct {
	Total int64                 `json:"total"`
	Items []ProductFeedbackView `json:"items"`
}

type CreateProductFeedbackInput struct {
	Title         string            `json:"title"`
	Content       string            `json:"content"`
	Images        []domain.ImageRef `json:"images"`
	SourceContext json.RawMessage   `json:"source_context"`
}

type ProductFeedbackQuery struct {
	Resolved bool
	Sort     string
	Offset   int
	Limit    int
}

func (service *Service) CreateProductFeedback(ctx context.Context, actor FeedbackActor, input CreateProductFeedbackInput) (ProductFeedbackView, error) {
	actorKey, err := actor.key()
	if err != nil {
		return ProductFeedbackView{}, err
	}
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.Content)
	if title == "" || content == "" {
		return ProductFeedbackView{}, fmt.Errorf("feedback title and content are required")
	}
	if len([]rune(title)) > maxProductFeedbackTitleLength {
		return ProductFeedbackView{}, fmt.Errorf("feedback title exceeds %d characters", maxProductFeedbackTitleLength)
	}
	if len([]rune(content)) > maxProductFeedbackContentLength {
		return ProductFeedbackView{}, fmt.Errorf("feedback content exceeds %d characters", maxProductFeedbackContentLength)
	}
	images, err := normalizeCommentImages(input.Images)
	if err != nil {
		return ProductFeedbackView{}, err
	}
	contextJSON, err := normalizeFeedbackSourceContext(input.SourceContext)
	if err != nil {
		return ProductFeedbackView{}, err
	}
	name := strings.TrimSpace(actor.Name)
	if name == "" {
		name = "当前用户"
	}
	now := time.Now().UTC()
	row := domain.ProductFeedback{
		ID: newProductFeedbackID(now, actorKey, title), Version: 1, Title: title, Content: content,
		Images: images, SourceContext: datatypes.JSON(contextJSON), AuthorKey: actorKey,
		AuthorOpenID: strings.TrimSpace(actor.OpenID), AuthorUnionID: strings.TrimSpace(actor.UnionID),
		AuthorEmail: domain.NormalizeEmail(actor.Email), AuthorName: name, AuthorAvatarURL: strings.TrimSpace(actor.AvatarURL),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := service.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ProductFeedbackView{}, fmt.Errorf("create product feedback: %w", err)
	}
	return service.productFeedback(ctx, actor, row.ID)
}

func (service *Service) ProductFeedbacks(ctx context.Context, actor FeedbackActor, query ProductFeedbackQuery) (ProductFeedbackList, error) {
	actorKey, err := actor.key()
	if err != nil {
		return ProductFeedbackList{}, err
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	if query.Offset < 0 {
		return ProductFeedbackList{}, fmt.Errorf("feedback offset must not be negative")
	}
	if query.Sort == "" {
		query.Sort = "latest"
	}
	if query.Sort != "latest" && query.Sort != "popular" {
		return ProductFeedbackList{}, fmt.Errorf("feedback sort must be latest or popular")
	}
	base := service.db.WithContext(ctx).Model(&domain.ProductFeedback{}).Where("resolved = ?", query.Resolved)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return ProductFeedbackList{}, fmt.Errorf("count product feedback: %w", err)
	}
	rows := make([]domain.ProductFeedback, 0)
	ordered := service.db.WithContext(ctx).Where("resolved = ?", query.Resolved)
	if query.Sort == "popular" {
		ordered = ordered.Order("(SELECT COUNT(*) FROM okr_product_feedback_plus_one AS vote WHERE vote.feedback_id = okr_product_feedback.id) DESC").Order("updated_at DESC")
	} else {
		ordered = ordered.Order("updated_at DESC")
	}
	if err := ordered.Offset(query.Offset).Limit(query.Limit).Find(&rows).Error; err != nil {
		return ProductFeedbackList{}, fmt.Errorf("list product feedback: %w", err)
	}
	items, err := service.productFeedbackViews(ctx, actor, actorKey, rows)
	if err != nil {
		return ProductFeedbackList{}, err
	}
	return ProductFeedbackList{Total: total, Items: items}, nil
}

func (service *Service) ReplyProductFeedback(ctx context.Context, actor FeedbackActor, feedbackID, content string) (ProductFeedbackView, error) {
	actorKey, err := actor.key()
	if err != nil {
		return ProductFeedbackView{}, err
	}
	feedbackID = strings.TrimSpace(feedbackID)
	content = strings.TrimSpace(content)
	if feedbackID == "" || content == "" {
		return ProductFeedbackView{}, fmt.Errorf("feedback id and reply content are required")
	}
	if len([]rune(content)) > maxProductFeedbackReplyLength {
		return ProductFeedbackView{}, fmt.Errorf("feedback reply exceeds %d characters", maxProductFeedbackReplyLength)
	}
	if err := service.requireProductFeedback(ctx, feedbackID); err != nil {
		return ProductFeedbackView{}, err
	}
	name := strings.TrimSpace(actor.Name)
	if name == "" {
		name = "当前用户"
	}
	now := time.Now().UTC()
	row := domain.ProductFeedbackReply{
		ID: newProductFeedbackReplyID(now, feedbackID, actorKey, content), FeedbackID: feedbackID, Content: content,
		AuthorKey: actorKey, AuthorOpenID: strings.TrimSpace(actor.OpenID), AuthorUnionID: strings.TrimSpace(actor.UnionID),
		AuthorEmail: domain.NormalizeEmail(actor.Email), AuthorName: name, AuthorAvatarURL: strings.TrimSpace(actor.AvatarURL), CreatedAt: now,
	}
	if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create product feedback reply: %w", err)
		}
		return touchProductFeedback(tx, feedbackID, now)
	}); err != nil {
		return ProductFeedbackView{}, err
	}
	return service.productFeedback(ctx, actor, feedbackID)
}

func (service *Service) AddProductFeedbackPlusOne(ctx context.Context, actor FeedbackActor, feedbackID string) (ProductFeedbackView, error) {
	actorKey, err := actor.key()
	if err != nil {
		return ProductFeedbackView{}, err
	}
	feedbackID = strings.TrimSpace(feedbackID)
	if err := service.requireProductFeedback(ctx, feedbackID); err != nil {
		return ProductFeedbackView{}, err
	}
	name := strings.TrimSpace(actor.Name)
	if name == "" {
		name = "当前用户"
	}
	now := time.Now().UTC()
	row := domain.ProductFeedbackPlusOne{
		FeedbackID: feedbackID, ActorKey: actorKey, ActorOpenID: strings.TrimSpace(actor.OpenID), ActorUnionID: strings.TrimSpace(actor.UnionID),
		ActorEmail: domain.NormalizeEmail(actor.Email), ActorName: name, ActorAvatarURL: strings.TrimSpace(actor.AvatarURL), CreatedAt: now,
	}
	if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("add product feedback plus one: %w", err)
		}
		return touchProductFeedback(tx, feedbackID, now)
	}); err != nil {
		return ProductFeedbackView{}, err
	}
	return service.productFeedback(ctx, actor, feedbackID)
}

func (service *Service) RemoveProductFeedbackPlusOne(ctx context.Context, actor FeedbackActor, feedbackID string) (ProductFeedbackView, error) {
	actorKey, err := actor.key()
	if err != nil {
		return ProductFeedbackView{}, err
	}
	feedbackID = strings.TrimSpace(feedbackID)
	if err := service.requireProductFeedback(ctx, feedbackID); err != nil {
		return ProductFeedbackView{}, err
	}
	if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("feedback_id = ? AND actor_key = ?", feedbackID, actorKey).Delete(&domain.ProductFeedbackPlusOne{}).Error; err != nil {
			return fmt.Errorf("remove product feedback plus one: %w", err)
		}
		return touchProductFeedback(tx, feedbackID, time.Now().UTC())
	}); err != nil {
		return ProductFeedbackView{}, err
	}
	return service.productFeedback(ctx, actor, feedbackID)
}

func (service *Service) SetProductFeedbackResolved(ctx context.Context, actor FeedbackActor, feedbackID string, expectedVersion int32, resolved bool) (ProductFeedbackView, error) {
	actorKey, err := actor.key()
	if err != nil {
		return ProductFeedbackView{}, err
	}
	if expectedVersion <= 0 {
		return ProductFeedbackView{}, fmt.Errorf("expected_version must be positive")
	}
	feedbackID = strings.TrimSpace(feedbackID)
	var row domain.ProductFeedback
	if err := service.db.WithContext(ctx).First(&row, "id = ?", feedbackID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ProductFeedbackView{}, ErrNotFound
		}
		return ProductFeedbackView{}, fmt.Errorf("get product feedback: %w", err)
	}
	if row.AuthorKey != actorKey && !actor.CanManage {
		return ProductFeedbackView{}, ErrFeedbackForbidden
	}
	if row.Version != expectedVersion {
		return ProductFeedbackView{}, ErrConflict
	}
	if row.Resolved == resolved {
		return service.productFeedback(ctx, actor, feedbackID)
	}
	now := time.Now().UTC()
	updates := map[string]any{"resolved": resolved, "updated_at": now, "version": gorm.Expr("version + 1")}
	if resolved {
		updates["resolved_by_key"] = actorKey
		updates["resolved_by_name"] = strings.TrimSpace(actor.Name)
		updates["resolved_by_avatar_url"] = strings.TrimSpace(actor.AvatarURL)
		updates["resolved_at"] = now
	} else {
		updates["resolved_by_key"] = ""
		updates["resolved_by_name"] = ""
		updates["resolved_by_avatar_url"] = ""
		updates["resolved_at"] = nil
	}
	result := service.db.WithContext(ctx).Model(&domain.ProductFeedback{}).Where("id = ? AND version = ?", feedbackID, expectedVersion).Updates(updates)
	if result.Error != nil {
		return ProductFeedbackView{}, fmt.Errorf("update product feedback status: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ProductFeedbackView{}, ErrConflict
	}
	return service.productFeedback(ctx, actor, feedbackID)
}

func (service *Service) productFeedback(ctx context.Context, actor FeedbackActor, feedbackID string) (ProductFeedbackView, error) {
	actorKey, err := actor.key()
	if err != nil {
		return ProductFeedbackView{}, err
	}
	var row domain.ProductFeedback
	if err := service.db.WithContext(ctx).First(&row, "id = ?", strings.TrimSpace(feedbackID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ProductFeedbackView{}, ErrNotFound
		}
		return ProductFeedbackView{}, fmt.Errorf("get product feedback: %w", err)
	}
	views, err := service.productFeedbackViews(ctx, actor, actorKey, []domain.ProductFeedback{row})
	if err != nil {
		return ProductFeedbackView{}, err
	}
	return views[0], nil
}

func (service *Service) productFeedbackViews(ctx context.Context, actor FeedbackActor, actorKey string, rows []domain.ProductFeedback) ([]ProductFeedbackView, error) {
	views := make([]ProductFeedbackView, len(rows))
	if len(rows) == 0 {
		return views, nil
	}
	ids := make([]string, len(rows))
	indexes := make(map[string]int, len(rows))
	for index, row := range rows {
		ids[index] = row.ID
		indexes[row.ID] = index
		views[index] = productFeedbackView(row, actorKey, actor.CanManage)
	}
	var replies []domain.ProductFeedbackReply
	if err := service.db.WithContext(ctx).Where("feedback_id IN ?", ids).Order("created_at ASC, id ASC").Find(&replies).Error; err != nil {
		return nil, fmt.Errorf("list product feedback replies: %w", err)
	}
	for _, reply := range replies {
		index, ok := indexes[reply.FeedbackID]
		if !ok {
			continue
		}
		views[index].Replies = append(views[index].Replies, ProductFeedbackReplyView{
			ID: reply.ID, Content: reply.Content, CreatedAt: reply.CreatedAt,
			Author: feedbackPerson(reply.AuthorName, reply.AuthorEmail, reply.AuthorAvatarURL),
		})
	}
	var plusOnes []domain.ProductFeedbackPlusOne
	if err := service.db.WithContext(ctx).Where("feedback_id IN ?", ids).Order("created_at ASC").Find(&plusOnes).Error; err != nil {
		return nil, fmt.Errorf("list product feedback plus ones: %w", err)
	}
	for _, plusOne := range plusOnes {
		index, ok := indexes[plusOne.FeedbackID]
		if !ok {
			continue
		}
		views[index].PlusOnes = append(views[index].PlusOnes, feedbackPerson(plusOne.ActorName, plusOne.ActorEmail, plusOne.ActorAvatarURL))
		if plusOne.ActorKey == actorKey {
			views[index].MyPlusOne = true
		}
	}
	return views, nil
}

func productFeedbackView(row domain.ProductFeedback, actorKey string, canManage bool) ProductFeedbackView {
	view := ProductFeedbackView{
		ID: row.ID, Version: row.Version, Title: row.Title, Content: row.Content,
		Images: append([]domain.ImageRef(nil), row.Images...), SourceContext: append(json.RawMessage(nil), row.SourceContext...),
		Author: feedbackPerson(row.AuthorName, row.AuthorEmail, row.AuthorAvatarURL), Resolved: row.Resolved,
		ResolvedAt: row.ResolvedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Replies: []ProductFeedbackReplyView{}, PlusOnes: []FeedbackPersonView{}, CanResolve: row.AuthorKey == actorKey || canManage,
	}
	if row.Resolved && row.ResolvedByName != "" {
		person := feedbackPerson(row.ResolvedByName, "", row.ResolvedByAvatarURL)
		view.ResolvedBy = &person
	}
	return view
}

func feedbackPerson(name, email, avatarURL string) FeedbackPersonView {
	return FeedbackPersonView{Name: strings.TrimSpace(name), Email: domain.NormalizeEmail(email), AvatarURL: strings.TrimSpace(avatarURL)}
}

func (service *Service) requireProductFeedback(ctx context.Context, feedbackID string) error {
	feedbackID = strings.TrimSpace(feedbackID)
	if feedbackID == "" {
		return fmt.Errorf("feedback id is required")
	}
	var count int64
	if err := service.db.WithContext(ctx).Model(&domain.ProductFeedback{}).Where("id = ?", feedbackID).Count(&count).Error; err != nil {
		return fmt.Errorf("find product feedback: %w", err)
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func touchProductFeedback(db *gorm.DB, feedbackID string, now time.Time) error {
	if err := db.Model(&domain.ProductFeedback{}).Where("id = ?", feedbackID).Update("updated_at", now).Error; err != nil {
		return fmt.Errorf("update product feedback activity: %w", err)
	}
	return nil
}

func normalizeFeedbackSourceContext(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > maxProductFeedbackContextLength {
		return nil, fmt.Errorf("feedback source context exceeds %d bytes", maxProductFeedbackContextLength)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("feedback source context must be a JSON object: %w", err)
	}
	return append(json.RawMessage(nil), raw...), nil
}

func newProductFeedbackID(now time.Time, actorKey, title string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", now.UnixNano(), actorKey, title)))
	return fmt.Sprintf("feedback_%x", sum[:10])
}

func newProductFeedbackReplyID(now time.Time, feedbackID, actorKey, content string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s", now.UnixNano(), feedbackID, actorKey, content)))
	return fmt.Sprintf("feedback_reply_%x", sum[:10])
}
