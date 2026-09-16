package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/authn"
	"jarvis/internal/domain"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
)

const retention = 30 * 24 * time.Hour

type AuditService struct {
	db  *gorm.DB
	now func() time.Time
}

type AuditFilter struct {
	Since     time.Time
	ActorKind string
	Operation string
	Route     string
	Resource  string
	Limit     int
}

type AuditList struct {
	Items []domain.AccessAuditEvent `json:"items"`
}

func NewAuditService(db *gorm.DB) (*AuditService, error) {
	if db == nil {
		return nil, fmt.Errorf("access audit db is nil")
	}
	service := &AuditService{db: db, now: time.Now}
	if err := service.prune(context.Background()); err != nil {
		return nil, err
	}
	if err := service.redactStoredAddresses(context.Background()); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *AuditService) List(ctx context.Context, filter AuditFilter) (*AuditList, error) {
	if err := s.prune(ctx); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := s.db.WithContext(ctx).Model(&domain.AccessAuditEvent{})
	if !filter.Since.IsZero() {
		query = query.Where("occurred_at >= ?", filter.Since)
	}
	if value := strings.TrimSpace(filter.ActorKind); value != "" {
		query = query.Where("actor_kind = ?", value)
	}
	if value := strings.TrimSpace(filter.Operation); value != "" {
		query = query.Where("operation = ?", value)
	}
	if value := strings.TrimSpace(filter.Route); value != "" {
		query = query.Where("route LIKE ? ESCAPE '\\'", containsPattern(value))
	}
	if value := strings.TrimSpace(filter.Resource); value != "" {
		pattern := containsPattern(value)
		query = query.Where("(resource_type LIKE ? ESCAPE '\\' OR resource_id LIKE ? ESCAPE '\\')", pattern, pattern)
	}
	items := make([]domain.AccessAuditEvent, 0, limit)
	if err := query.Order("occurred_at DESC, id DESC").Limit(limit).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list access audit events: %w", err)
	}
	for index := range items {
		items[index].RemoteAddress = authn.RedactAddress(items[index].RemoteAddress)
		if items[index].ActorKind == "unknown_remote" {
			items[index].ActorID = authn.RedactAddress(items[index].ActorID)
		}
	}
	return &AuditList{Items: items}, nil
}

// Middleware records API access metadata after the handler completes. It never
// stores request or response bodies.
func (s *AuditService) Middleware(auth *authn.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		if !shouldAudit(path) {
			c.Next(ctx)
			return
		}
		actorKind, actorID := actorForRequest(ctx, c, auth)
		c.Next(ctx)

		route := normalizedRoute(c)
		event := domain.AccessAuditEvent{
			OccurredAt:    s.now().UTC(),
			Operation:     operationForMethod(string(c.Method())),
			Method:        string(c.Method()),
			Route:         route,
			ResourceType:  resourceType(path),
			ResourceID:    routeResourceID(route, path),
			StatusCode:    c.Response.StatusCode(),
			RequestID:     observability.LogID(observability.FromRequestContext(ctx, c)),
			RemoteAddress: authn.RedactIP(authn.ClientIP(c)),
			ActorKind:     actorKind,
			ActorID:       actorID,
		}
		if err := s.db.WithContext(context.WithoutCancel(ctx)).Create(&event).Error; err != nil {
			hlog.CtxErrorf(ctx, "record access audit event failed: %+v", err)
		}
	}
}

func (s *AuditService) prune(ctx context.Context) error {
	if err := s.db.WithContext(ctx).
		Where("occurred_at < ?", s.now().UTC().Add(-retention)).
		Delete(&domain.AccessAuditEvent{}).Error; err != nil {
		return fmt.Errorf("prune access audit events: %w", err)
	}
	return nil
}

func (s *AuditService) redactStoredAddresses(ctx context.Context) error {
	var items []domain.AccessAuditEvent
	if err := s.db.WithContext(ctx).
		Select("id", "actor_kind", "actor_id", "remote_address").
		Find(&items).Error; err != nil {
		return fmt.Errorf("load access audit addresses for redaction: %w", err)
	}
	for _, item := range items {
		updates := map[string]any{}
		if redacted := authn.RedactAddress(item.RemoteAddress); redacted != item.RemoteAddress {
			updates["remote_address"] = redacted
		}
		if item.ActorKind == "unknown_remote" {
			if redacted := authn.RedactAddress(item.ActorID); redacted != item.ActorID {
				updates["actor_id"] = redacted
			}
		}
		if len(updates) == 0 {
			continue
		}
		if err := s.db.WithContext(ctx).
			Model(&domain.AccessAuditEvent{}).
			Where("id = ?", item.ID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("redact access audit event id=%d: %w", item.ID, err)
		}
	}
	return nil
}

func shouldAudit(path string) bool {
	return strings.HasPrefix(path, "/api/") &&
		!strings.HasPrefix(path, "/api/auth/") &&
		path != "/api/security-audit-events"
}

func operationForMethod(method string) string {
	if method == "GET" || method == "HEAD" {
		return "read"
	}
	return "write"
}

func actorForRequest(ctx context.Context, c *app.RequestContext, auth *authn.Service) (string, string) {
	if auth != nil {
		if user, ok := auth.AuthenticateRequest(ctx, c); ok {
			if user.IsPrincipal {
				return "principal", user.Email
			}
			return "other_user", user.Email
		}
	}
	if authn.IsLoopbackRequest(c) {
		return "local_agent", "localhost"
	}
	return "unknown_remote", authn.RedactIP(authn.ClientIP(c))
}

func normalizedRoute(c *app.RequestContext) string {
	if route := strings.TrimSpace(c.FullPath()); route != "" {
		return route
	}
	return string(c.Path())
}

func resourceType(path string) string {
	parts := pathParts(path)
	if len(parts) < 2 {
		return "api"
	}
	return parts[1]
}

func routeResourceID(route, path string) *string {
	routeParts := pathParts(route)
	pathValues := pathParts(path)
	values := make([]string, 0, 2)
	for index, part := range routeParts {
		if strings.HasPrefix(part, ":") && index < len(pathValues) {
			values = append(values, pathValues[index])
		}
	}
	if len(values) == 0 {
		return nil
	}
	value := strings.Join(values, "/")
	return &value
}

func pathParts(path string) []string {
	raw := strings.Split(strings.Trim(path, "/"), "/")
	parts := raw[:0]
	for _, value := range raw {
		if value != "" {
			parts = append(parts, value)
		}
	}
	return parts
}

func containsPattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(value) + "%"
}
