package okrworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
	"jarvis/internal/larkcli"
	"jarvis/internal/okrworkspace/domain"
)

type CommentDeliveryView struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) prepareCommentDeliveries(ctx context.Context, row domain.PageComment, authorEmail, authorUnionID, tab string) error {
	if len(row.Mentions) == 0 {
		return nil
	}
	input, err := s.commentMentionNotification(ctx, row, tab)
	if err != nil {
		return err
	}
	input.AuthorEmail = domain.NormalizeEmail(authorEmail)
	for _, recipient := range row.Mentions {
		if recipient.Email == input.AuthorEmail || (authorUnionID != "" && recipient.UnionID == authorUnionID) {
			continue
		}
		input.Recipient = recipient
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		appID := ""
		if s.commentNotifier != nil {
			appID = s.commentNotifier.AppID()
		}
		record := domain.CommentDelivery{CommentID: row.ID, Email: recipient.Email, Name: recipient.Name, AppID: appID, Payload: string(raw), Status: "pending", UpdatedAt: time.Now().UTC()}
		if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) commentDeliveries(ctx context.Context, id string) ([]CommentDeliveryView, error) {
	var rows []domain.CommentDelivery
	if err := s.db.WithContext(ctx).Where("comment_id = ?", id).Order("email").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]CommentDeliveryView, 0, len(rows))
	for _, r := range rows {
		status := r.Status
		if status == "sending" && time.Since(r.UpdatedAt) > 2*time.Minute {
			status = "unknown"
		}
		result = append(result, CommentDeliveryView{Email: r.Email, Name: r.Name, Status: status, MessageID: r.MessageID, Error: r.Error})
	}
	return result, nil
}

func deliveryWarnings(items []CommentDeliveryView) []string {
	var result []string
	for _, r := range items {
		if r.Status != "delivered" && r.Status != "pending" && r.Status != "sending" {
			result = append(result, fmt.Sprintf("提醒 %s 未完成：%s", r.Name, r.Error))
		}
	}
	return result
}

func (s *Service) RetryCommentNotifications(ctx context.Context, id, email string) ([]CommentDeliveryView, error) {
	var row domain.PageComment
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	// Only persisted original delivery intents are eligible. Historical comments
	// without receipts need an explicit, separately audited recovery operation.
	return s.deliverComment(ctx, id, domain.NormalizeEmail(email))
}

func (s *Service) deliverComment(ctx context.Context, id, email string) ([]CommentDeliveryView, error) {
	var records []domain.CommentDelivery
	query := s.db.WithContext(ctx).Where("comment_id = ?", id)
	if email != "" {
		query = query.Where("email = ?", email)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.Status == "delivered" {
			continue
		}
		db := s.db.WithContext(ctx).Model(&domain.CommentDelivery{}).Where("comment_id = ? AND email = ?", id, record.Email)
		if s.commentNotifier == nil {
			if err := db.Updates(map[string]any{"status": "failed", "error": "通知机器人未配置", "updated_at": time.Now().UTC()}).Error; err != nil {
				return nil, err
			}
			continue
		}
		if record.AppID != s.commentNotifier.AppID() {
			return nil, fmt.Errorf("notification application changed; review existing delivery before retry")
		}
		if record.MessageID != "" {
			if err := s.commentNotifier.VerifyCommentMention(ctx, record.MessageID); err != nil {
				continue
			}
			if err := db.Updates(map[string]any{"status": "delivered", "error": "", "updated_at": time.Now().UTC()}).Error; err != nil {
				return nil, err
			}
			continue
		}
		if record.Status != "pending" && record.Status != "failed" {
			continue
		}
		claim := db.Session(&gorm.Session{}).Where("status IN ?", []string{"pending", "failed"}).Updates(map[string]any{"status": "sending", "attempts": gorm.Expr("attempts + 1"), "updated_at": time.Now().UTC()})
		if claim.Error != nil {
			return nil, claim.Error
		}
		if claim.RowsAffected != 1 {
			continue
		}
		var input CommentMentionNotification
		if err := json.Unmarshal([]byte(record.Payload), &input); err != nil {
			return nil, err
		}
		messageID, sendErr := s.commentNotifier.NotifyCommentMention(ctx, input)
		status, message := "delivered", ""
		if sendErr != nil {
			status, message = "unknown", "发送结果待核验"
			var apiErr *larkcli.APIError
			if errors.As(sendErr, &apiErr) && apiErr.Type == "authorization" {
				status, message = "failed", "通知应用缺少发送权限或人员不在可用范围"
			}
			hlog.CtxErrorf(ctx, "comment notification comment=%s email=%s message_id=%s: %v", id, record.Email, messageID, sendErr)
		}
		if messageID == "" && sendErr == nil {
			status, message = "unknown", "未取得发送回执"
		}
		if err := db.Updates(map[string]any{"status": status, "message_id": messageID, "error": message, "updated_at": time.Now().UTC()}).Error; err != nil {
			return nil, err
		}
	}
	return s.commentDeliveries(ctx, id)
}

// RunCommentDeliveries drains durable pending intents outside the HTTP request.
// Existing claims arbitrate between the main and shared development processes.
func (s *Service) RunCommentDeliveries(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.processPendingCommentDeliveries(ctx); err != nil && ctx.Err() == nil {
			hlog.CtxErrorf(ctx, "process pending comment notifications: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) processPendingCommentDeliveries(ctx context.Context) error {
	var rows []domain.CommentDelivery
	if err := s.db.WithContext(ctx).Where("status = ?", "pending").Order("updated_at").Limit(20).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		_, err := s.deliverComment(callCtx, r.CommentID, r.Email)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
