package api

import (
	"context"
	"fmt"

	"jarvis/internal/background"
	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// OKRModuleDependencies is the single adapter between the optional OKR module
// and Jarvis HTTP hosting. Core route registration does not require it.
type OKRModuleDependencies struct {
	Workspace *okrworkspace.Service
	Images    *okrworkspace.ImageStore
	Identity  *okrAuth.Service
	Documents MarkdownDocumentCreator
	People    *background.ResolveService
	Enabled   func(context.Context) (bool, error)
}

// WeeklyReportModuleDependencies keeps the weekly collaboration surface behind
// its own module gate. Workspace is currently the compatibility adapter over
// the preserved Emily tables; callers only reach it through weekly routes.
type WeeklyReportModuleDependencies struct {
	Workspace *okrworkspace.Service
	Identity  *okrAuth.Service
	Documents MarkdownDocumentCreator
	Enabled   func(context.Context) (bool, error)
}

func RegisterOKRModuleRoutes(h *server.Hertz, deps OKRModuleDependencies) error {
	if h == nil || deps.Workspace == nil || deps.Images == nil || deps.Identity == nil || deps.People == nil {
		return fmt.Errorf("register OKR module routes: required dependency is nil")
	}
	if deps.Enabled == nil {
		return fmt.Errorf("register OKR module routes: enablement gate is nil")
	}
	requireEnabled := func(ctx context.Context, c *app.RequestContext) {
		enabled, err := deps.Enabled(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50089, err)
			c.Abort()
			return
		}
		if !enabled {
			writeAPIError(c, consts.StatusNotFound, 40489, fmt.Errorf("OKR module is disabled"))
			c.Abort()
			return
		}
		c.Next(ctx)
	}
	h.GET("/api/okr/enums", requireEnabled, Enums())
	h.GET("/api/okr/me", requireEnabled, GetOKRCurrentUser(deps.Identity))
	h.GET("/api/okr/auth/feishu/login", requireEnabled, BeginOKRFeishuLogin(deps.Identity))
	h.GET("/api/okr/auth/feishu/callback", requireEnabled, CompleteOKRFeishuLogin(deps.Identity))
	h.POST("/api/okr/auth/logout", requireEnabled, LogoutOKR(deps.Identity))
	requireIdentity := RequireOKRIdentity(deps.Identity)
	h.GET("/api/okr/scope", requireEnabled, GetOKRWorkspaceScope(deps.Workspace))
	h.GET("/api/okr/people/search", requireEnabled, SearchWorkspacePeople(deps.People))
	h.GET("/api/okr/board", requireEnabled, GetCoreBoard(deps.Workspace))
	h.GET("/api/okr/krs/:kr_id", requireEnabled, GetCoreKR(deps.Workspace))
	h.POST("/api/okr/images", requireEnabled, requireIdentity, UploadOKRImage(deps.Images))
	h.POST("/api/okr/feishu-documents", requireEnabled, requireIdentity, CreateOKRDocument(deps.Documents))
	h.POST("/api/okr/objectives", requireEnabled, requireIdentity, CreateObjective(deps.Workspace))
	h.PUT("/api/okr/objectives/:objective_id", requireEnabled, requireIdentity, UpdateObjective(deps.Workspace))
	h.DELETE("/api/okr/objectives/:objective_id", requireEnabled, requireIdentity, DeleteObjective(deps.Workspace))
	h.POST("/api/okr/objectives/:objective_id/krs", requireEnabled, requireIdentity, CreateKR(deps.Workspace))
	h.PUT("/api/okr/krs/:kr_id", requireEnabled, requireIdentity, ReplaceCoreKR(deps.Workspace))
	h.PUT("/api/okr/krs/:kr_id/tags", requireEnabled, requireIdentity, ReplaceKRTags(deps.Workspace))
	h.PUT("/api/okr/points/:point_id/tags", requireEnabled, requireIdentity, ReplacePointTags(deps.Workspace))
	h.DELETE("/api/okr/krs/:kr_id", requireEnabled, requireIdentity, DeleteKR(deps.Workspace))
	return nil
}

func RegisterWeeklyReportModuleRoutes(h *server.Hertz, deps WeeklyReportModuleDependencies) error {
	if h == nil || deps.Workspace == nil || deps.Identity == nil || deps.Documents == nil {
		return fmt.Errorf("register weekly report module routes: required dependency is nil")
	}
	if deps.Enabled == nil {
		return fmt.Errorf("register weekly report module routes: enablement gate is nil")
	}
	requireEnabled := func(ctx context.Context, c *app.RequestContext) {
		enabled, err := deps.Enabled(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50088, err)
			c.Abort()
			return
		}
		if !enabled {
			writeAPIError(c, consts.StatusNotFound, 40488, fmt.Errorf("weekly report module is disabled"))
			c.Abort()
			return
		}
		c.Next(ctx)
	}
	requireIdentity := RequireOKRIdentity(deps.Identity)
	h.GET("/api/weekly-report/scope", requireEnabled, GetWeeklyReportScope(deps.Workspace))
	h.GET("/api/weekly-report/weeks", requireEnabled, GetWeeklyReportWeeks(deps.Workspace))
	h.POST("/api/weekly-report/weeks", requireEnabled, requireIdentity, OpenWeeklyReportWeek(deps.Workspace))
	h.DELETE("/api/weekly-report/weeks/:week", requireEnabled, requireIdentity, DeleteWeeklyReportWeek(deps.Workspace))
	h.GET("/api/weekly-report/board", requireEnabled, GetBoard(deps.Workspace))
	h.GET("/api/weekly-report/comments", requireEnabled, GetComments(deps.Workspace))
	h.POST("/api/weekly-report/comments", requireEnabled, requireIdentity, CreateComment(deps.Workspace))
	h.PUT("/api/weekly-report/comments/:comment_id", requireEnabled, requireIdentity, UpdateComment(deps.Workspace))
	h.DELETE("/api/weekly-report/comments/:comment_id", requireEnabled, requireIdentity, DeleteComment(deps.Workspace))
	h.GET("/api/weekly-report/reminder-preview", requireEnabled, GetReminderPreview(deps.Workspace))
	h.GET("/api/weekly-report/reminder-batches", requireEnabled, GetReminderBatches(deps.Workspace))
	h.POST("/api/weekly-report/reminder-batches/generate", requireEnabled, requireIdentity, GenerateReminderBatch(deps.Workspace))
	h.POST("/api/weekly-report/feishu-documents", requireEnabled, requireIdentity, CreateOKRDocument(deps.Documents))
	h.GET("/api/weekly-report/points/:point_id/meego-preview", requireEnabled, GetMeegoPreview(deps.Workspace))
	h.GET("/api/weekly-report/meego-preview", requireEnabled, GetMeegoBatchPreview(deps.Workspace))
	h.POST("/api/weekly-report/meego-observations", requireEnabled, StoreMeegoObservation(deps.Workspace))
	h.POST("/api/weekly-report/points/:point_id/meego-confirm", requireEnabled, requireIdentity, ConfirmMeegoProgress(deps.Workspace))
	h.GET("/api/weekly-report/krs/:kr_id", requireEnabled, GetWeeklyReportKR(deps.Workspace))
	h.PUT("/api/weekly-report/krs/:kr_id/core", requireEnabled, requireIdentity, ReplaceWeeklyKRCore(deps.Workspace))
	h.PUT("/api/weekly-report/scores/:target_kind/:target_id", requireEnabled, requireIdentity, ReplaceWeeklyScore(deps.Workspace))
	h.DELETE("/api/weekly-report/scores/:target_kind/:target_id", requireEnabled, requireIdentity, DeleteWeeklyScore(deps.Workspace))
	h.POST("/api/weekly-report/points/:point_id/progress", requireEnabled, requireIdentity, CreateWeeklyProgressEntry(deps.Workspace))
	h.PUT("/api/weekly-report/progress/:progress_id", requireEnabled, requireIdentity, UpdateWeeklyProgressEntry(deps.Workspace))
	h.DELETE("/api/weekly-report/progress/:progress_id", requireEnabled, requireIdentity, DeleteWeeklyProgressEntry(deps.Workspace))
	return nil
}
