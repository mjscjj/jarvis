package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

type ownerContextKey struct{}

var errOwnerRequired = errors.New("chat owner is required")

// WithOwner scopes one request to a verified browser identity. The owner is
// supplied by server middleware, never by a client field in the request body.
func WithOwner(ctx context.Context, ownerID string) context.Context {
	return context.WithValue(ctx, ownerContextKey{}, strings.TrimSpace(ownerID))
}

func (s *Service) owner(ctx context.Context) (string, error) {
	ownerID, _ := ctx.Value(ownerContextKey{}).(string)
	if s.ownerRequired && ownerID == "" {
		return "", errOwnerRequired
	}
	return ownerID, nil
}

func (s *Service) scopedSessions(ctx context.Context) (*gorm.DB, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	ownerID, err := s.owner(ctx)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx).Model(&domain.ChatSession{})
	if ownerID != "" {
		db = db.Where("owner_id = ?", ownerID)
	}
	return db, nil
}

func (s *Service) requireSessionAccess(ctx context.Context, sessionID string) error {
	db, err := s.scopedSessions(ctx)
	if err != nil {
		return err
	}
	var count int64
	if err := db.Where("id = ?", strings.TrimSpace(sessionID)).Count(&count).Error; err != nil {
		return fmt.Errorf("check chat session access: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
