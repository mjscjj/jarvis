package security

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/domain"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAuditMiddlewareRecordsAccessMetadata(t *testing.T) {
	db := openAuditTestDB(t)
	service := &AuditService{
		db:  db,
		now: func() time.Time { return time.Date(2026, 9, 8, 10, 30, 0, 0, time.UTC) },
	}
	request := auditRequestContext("127.0.0.1", "", "POST", "/api/projects/42", "/api/projects/:project_id")
	request.SetHandlers(app.HandlersChain{func(_ context.Context, c *app.RequestContext) {
		c.Status(consts.StatusNoContent)
	}})

	service.Middleware(nil)(context.Background(), request)
	if request.Response.StatusCode() != consts.StatusNoContent {
		t.Fatalf("status = %d", request.Response.StatusCode())
	}

	var event domain.AccessAuditEvent
	if err := db.Take(&event).Error; err != nil {
		t.Fatalf("load audit event: %v", err)
	}
	if event.Operation != "write" || event.Method != "POST" {
		t.Fatalf("operation/method = %q/%q", event.Operation, event.Method)
	}
	if event.Route != "/api/projects/:project_id" || event.ResourceType != "projects" {
		t.Fatalf("route/resource_type = %q/%q", event.Route, event.ResourceType)
	}
	if event.ResourceID == nil || *event.ResourceID != "42" {
		t.Fatalf("resource_id = %#v", event.ResourceID)
	}
	if event.ActorKind != "local_agent" || event.ActorID != "localhost" {
		t.Fatalf("actor = %q/%q", event.ActorKind, event.ActorID)
	}
	if event.RemoteAddress != "127.0.0.*" {
		t.Fatalf("remote_address = %q, want redacted loopback test address", event.RemoteAddress)
	}
	if event.StatusCode != consts.StatusNoContent {
		t.Fatalf("status_code = %d", event.StatusCode)
	}
}

func TestAuditMiddlewareRecordsRemoteProxyClientAsUnknownAndRedacted(t *testing.T) {
	db := openAuditTestDB(t)
	service := &AuditService{
		db:  db,
		now: func() time.Time { return time.Date(2026, 9, 8, 10, 30, 0, 0, time.UTC) },
	}
	request := auditRequestContext("127.0.0.1", "10.20.30.40", "GET", "/api/projects/42", "/api/projects/:project_id")
	request.SetHandlers(app.HandlersChain{func(_ context.Context, c *app.RequestContext) {
		c.Status(consts.StatusOK)
	}})

	service.Middleware(nil)(context.Background(), request)

	var event domain.AccessAuditEvent
	if err := db.Take(&event).Error; err != nil {
		t.Fatalf("load audit event: %v", err)
	}
	if event.ActorKind != "unknown_remote" || event.ActorID != "10.20.30.*" {
		t.Fatalf("actor = %q/%q", event.ActorKind, event.ActorID)
	}
	if event.RemoteAddress != "10.20.30.*" {
		t.Fatalf("remote_address = %q", event.RemoteAddress)
	}
}

func TestAuditListFiltersAndPrunesExpiredEvents(t *testing.T) {
	db := openAuditTestDB(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	events := []domain.AccessAuditEvent{
		{OccurredAt: now.Add(-31 * 24 * time.Hour), ActorKind: "principal", ActorID: "old@example.com", Operation: "read", Method: "GET", Route: "/api/tasks", ResourceType: "tasks", StatusCode: 200},
		{OccurredAt: now.Add(-2 * time.Hour), ActorKind: "principal", ActorID: "alice@example.com", Operation: "read", Method: "GET", Route: "/api/tasks/:task_id", ResourceType: "tasks", ResourceID: stringPointer("42"), StatusCode: 200, RemoteAddress: "10.20.30.40:18800"},
		{OccurredAt: now.Add(-time.Hour), ActorKind: "local_agent", ActorID: "localhost", Operation: "write", Method: "POST", Route: "/api/tasks", ResourceType: "tasks", StatusCode: 201},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed audit events: %v", err)
	}
	service := &AuditService{db: db, now: func() time.Time { return now }}

	result, err := service.List(context.Background(), AuditFilter{
		Since:     now.Add(-24 * time.Hour),
		ActorKind: "principal",
		Operation: "read",
		Route:     "tasks",
		Resource:  "42",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ActorID != "alice@example.com" {
		t.Fatalf("items = %#v", result.Items)
	}
	if result.Items[0].RemoteAddress != "10.20.30.*" {
		t.Fatalf("legacy remote address was not redacted: %q", result.Items[0].RemoteAddress)
	}

	var expired int64
	if err := db.Model(&domain.AccessAuditEvent{}).
		Where("occurred_at < ?", now.Add(-retention)).
		Count(&expired).Error; err != nil {
		t.Fatalf("count expired events: %v", err)
	}
	if expired != 0 {
		t.Fatalf("expired events = %d, want 0", expired)
	}
}

func TestNewAuditServiceRedactsExistingAddresses(t *testing.T) {
	db := openAuditTestDB(t)
	event := domain.AccessAuditEvent{
		OccurredAt: time.Now().UTC(), ActorKind: "unknown_remote",
		ActorID: "10.20.30.40", Operation: "read", Method: "GET",
		Route: "/api/tasks", ResourceType: "tasks", StatusCode: 401,
		RemoteAddress: "10.20.30.40:43000",
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("seed audit event: %v", err)
	}

	if _, err := NewAuditService(db); err != nil {
		t.Fatalf("NewAuditService() error = %v", err)
	}

	var stored domain.AccessAuditEvent
	if err := db.First(&stored, event.ID).Error; err != nil {
		t.Fatalf("load redacted event: %v", err)
	}
	if stored.ActorID != "10.20.30.*" || stored.RemoteAddress != "10.20.30.*" {
		t.Fatalf("stored addresses = %q / %q", stored.ActorID, stored.RemoteAddress)
	}
}

type auditRemoteConn struct {
	*mock.Conn
	remote net.Addr
}

func (c *auditRemoteConn) RemoteAddr() net.Addr { return c.remote }

func stringPointer(value string) *string { return &value }

func auditRequestContext(peer, forwarded, method, path, route string) *app.RequestContext {
	request := app.NewContext(0)
	request.SetConn(&auditRemoteConn{
		Conn:   mock.NewConn(""),
		remote: &net.TCPAddr{IP: net.ParseIP(peer), Port: 43000},
	})
	request.Request.SetRequestURI(path)
	request.Request.Header.SetMethod(method)
	if forwarded != "" {
		request.Request.Header.Set("X-Forwarded-For", forwarded)
	}
	request.SetFullPath(route)
	return request
}

func openAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "audit.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(domain.SecurityModels()...); err != nil {
		t.Fatalf("migrate audit schema: %v", err)
	}
	return db
}
