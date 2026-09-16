package okrworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
	"jarvis/internal/larkcli"
	"jarvis/internal/okrworkspace/domain"
)

const (
	commentNotificationReasonMention         = "mention"
	commentNotificationReasonOwner           = "owner"
	commentNotificationReasonMentionAndOwner = "mention_and_owner"
)

type CommentDeliveryView struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) prepareCommentDeliveries(ctx context.Context, row domain.PageComment, authorEmail, authorUnionID, tab string) error {
	input, err := s.commentMentionNotification(ctx, row, tab)
	if err != nil {
		return err
	}
	input.AuthorEmail = domain.NormalizeEmail(authorEmail)
	type plannedRecipient struct {
		person     CommentMention
		mentioned  bool
		owner      bool
		ownerLevel string
	}
	planned := make(map[string]plannedRecipient, len(row.Mentions))
	for _, recipient := range row.Mentions {
		email := domain.NormalizeEmail(recipient.Email)
		planned[email] = plannedRecipient{person: recipient, mentioned: true}
	}
	owners, ownerLevel, err := s.commentOwnerRecipients(ctx, input)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		email := domain.NormalizeEmail(owner.Email)
		if !domain.ValidEmail(email) {
			continue
		}
		candidate := planned[email]
		if candidate.person.Email == "" {
			candidate.person = CommentMention{Email: email, Name: owner.Name, UnionID: owner.UnionID}
		}
		candidate.owner, candidate.ownerLevel = true, ownerLevel
		planned[email] = candidate
	}
	emails := make([]string, 0, len(planned))
	for email := range planned {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	for _, email := range emails {
		candidate := planned[email]
		recipient := candidate.person
		if recipient.Email == input.AuthorEmail || (authorUnionID != "" && recipient.UnionID == authorUnionID) {
			continue
		}
		input.Reason, input.OwnerLevel = commentNotificationReasonMention, ""
		if candidate.owner {
			input.Reason, input.OwnerLevel = commentNotificationReasonOwner, candidate.ownerLevel
			if candidate.mentioned {
				input.Reason = commentNotificationReasonMentionAndOwner
			}
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

// commentOwnerRecipients applies nearest-owner-wins. An assigned level with
// unresolved identities still stops fallback; notifying a parent would change
// accountability rather than repair the missing identity.
func (s *Service) commentOwnerRecipients(ctx context.Context, input CommentMentionNotification) ([]CommentMention, string, error) {
	db := s.db.WithContext(ctx)
	if input.PointID != "" {
		var rows []domain.PointOwner
		if err := db.Where("point_id = ?", input.PointID).Order("sort_order, owner_key, person_id").Find(&rows).Error; err != nil {
			return nil, "", fmt.Errorf("list comment point owners: %w", err)
		}
		if len(rows) > 0 {
			owners := make([]CommentMention, 0, len(rows))
			for _, row := range rows {
				owners = append(owners, CommentMention{Email: row.Email, Name: row.Name, UnionID: row.UnionID})
			}
			return owners, "point", nil
		}
	}
	if input.KRID != "" {
		var rows []domain.KROwner
		if err := db.Where("kr_id = ?", input.KRID).Order("sort_order, owner_key, person_id").Find(&rows).Error; err != nil {
			return nil, "", fmt.Errorf("list comment KR owners: %w", err)
		}
		if len(rows) > 0 {
			owners := make([]CommentMention, 0, len(rows))
			for _, row := range rows {
				owners = append(owners, CommentMention{Email: row.Email, Name: row.Name, UnionID: row.UnionID})
			}
			return owners, "kr", nil
		}
	}
	if input.ObjectiveID != "" {
		var rows []domain.ObjectiveOwner
		if err := db.Where("objective_id = ?", input.ObjectiveID).Order("sort_order, owner_key, person_id").Find(&rows).Error; err != nil {
			return nil, "", fmt.Errorf("list comment objective owners: %w", err)
		}
		if len(rows) > 0 {
			owners := make([]CommentMention, 0, len(rows))
			for _, row := range rows {
				owners = append(owners, CommentMention{Email: row.Email, Name: row.Name, UnionID: row.UnionID})
			}
			return owners, "objective", nil
		}
	}
	return nil, "", nil
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
