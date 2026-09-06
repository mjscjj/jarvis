package api

import (
	"context"
	"fmt"

	"jarvis/internal/background"
	"jarvis/internal/okrreview"
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
	Enabled   func(context.Context) (bool, error)
}

// AgencyOKRModuleDependencies keeps organization-specific product behavior
// behind its own module gate. It composes the reusable OKR workspace without
// becoming a second source of truth for objectives, KRs, or formal progress.
type AgencyOKRModuleDependencies struct {
	Workspace *okrworkspace.Service
	Identity  *okrAuth.Service
	Documents MarkdownDocumentCreator
	People    *background.ResolveService
	Enabled   func(context.Context) (bool, error)
	// PreviewReview runs the advisory OKR Preview review agent.
	PreviewReview *okrreview.Service
	// UserTokens, Tokens and FeishuAppID serve the signed-in user's own Feishu
	// credentials to the chat sidecar.
	UserTokens  *okrAuth.UserTokens
	Tokens      *okrAuth.TokenStore
	FeishuAppID string
}

func RegisterOKRModuleRoutes(h *server.Hertz, deps OKRModuleDependencies) error {
	if h == nil || deps.Workspace == nil || deps.Images == nil {
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
	h.GET("/api/okr/scope", requireEnabled, GetOKRWorkspaceScope(deps.Workspace))
	h.GET("/api/okr/board", requireEnabled, GetCoreBoard(deps.Workspace))
	h.GET("/api/okr/krs/:kr_id", requireEnabled, GetCoreKR(deps.Workspace))
	h.POST("/api/okr/images", requireEnabled, UploadOKRImage(deps.Images))
	h.POST("/api/okr/objectives", requireEnabled, CreateObjective(deps.Workspace))
	h.PUT("/api/okr/objectives/order", requireEnabled, ReorderObjectives(deps.Workspace))
	h.PUT("/api/okr/objectives/:objective_id", requireEnabled, UpdateObjective(deps.Workspace))
	h.PUT("/api/okr/objectives/:objective_id/kr-order", requireEnabled, ReorderKRs(deps.Workspace))
	h.DELETE("/api/okr/objectives/:objective_id", requireEnabled, DeleteObjective(deps.Workspace))
	h.POST("/api/okr/objectives/:objective_id/krs", requireEnabled, CreateKR(deps.Workspace))
	h.PUT("/api/okr/krs/:kr_id", requireEnabled, ReplaceGenericKR(deps.Workspace))
	h.PUT("/api/okr/krs/:kr_id/definition", requireEnabled, ReplaceKRDefinition(deps.Workspace))
	h.DELETE("/api/okr/krs/:kr_id", requireEnabled, DeleteKR(deps.Workspace))
	// Formal OKR time-series data remains available without Agency OKR.
	h.GET("/api/okr/progress/scope", requireEnabled, GetWeeklyReportScope(deps.Workspace))
	h.GET("/api/okr/progress/board", requireEnabled, GetProgressBoard(deps.Workspace))
	h.GET("/api/okr/weeks", requireEnabled, GetWeeklyReportWeeks(deps.Workspace))
	h.POST("/api/okr/weeks", requireEnabled, OpenWeeklyReportWeek(deps.Workspace))
	h.DELETE("/api/okr/weeks/:week", requireEnabled, DeleteWeeklyReportWeek(deps.Workspace))
	h.GET("/api/okr/krs/:kr_id/weekly", requireEnabled, GetWeeklyReportKR(deps.Workspace))
	h.PUT("/api/okr/krs/:kr_id/weekly-core", requireEnabled, ReplaceWeeklyKRCore(deps.Workspace))
	h.POST("/api/okr/points/:point_id/progress", requireEnabled, CreateWeeklyProgressEntry(deps.Workspace))
	h.PUT("/api/okr/progress/:progress_id", requireEnabled, UpdateWeeklyProgressEntry(deps.Workspace))
	h.DELETE("/api/okr/progress/:progress_id", requireEnabled, DeleteWeeklyProgressEntry(deps.Workspace))
	return nil
}

func RegisterAgencyOKRModuleRoutes(h *server.Hertz, deps AgencyOKRModuleDependencies) error {
	if h == nil || deps.Workspace == nil || deps.Identity == nil || deps.Documents == nil || deps.People == nil {
		return fmt.Errorf("register Agency OKR module routes: required dependency is nil")
	}
	if deps.PreviewReview == nil {
		return fmt.Errorf("register Agency OKR module routes: preview review service is nil")
	}
	if deps.Enabled == nil {
		return fmt.Errorf("register Agency OKR module routes: enablement gate is nil")
	}
	requireEnabled := func(ctx context.Context, c *app.RequestContext) {
		enabled, err := deps.Enabled(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50088, err)
			c.Abort()
			return
		}
		if !enabled {
			writeAPIError(c, consts.StatusNotFound, 40488, fmt.Errorf("Agency OKR module is disabled"))
			c.Abort()
			return
		}
		c.Next(ctx)
	}
	requireIdentity := RequireOKRIdentity(deps.Identity)
	h.GET("/api/agency-okr/me", requireEnabled, GetOKRCurrentUser(deps.Identity))
	h.POST("/api/agency-okr/auth/feishu/device", requireEnabled, BeginOKRFeishuDeviceLogin(deps.Identity))
	h.POST("/api/agency-okr/auth/feishu/device/:login_id/poll", requireEnabled, PollOKRFeishuDeviceLogin(deps.Identity))
	h.POST("/api/agency-okr/auth/logout", requireEnabled, LogoutOKR(deps.Identity))
	if deps.UserTokens != nil {
		h.GET("/api/agency-okr/feishu-identity", requireEnabled, GetOKRFeishuIdentity(deps.UserTokens, deps.Tokens, deps.FeishuAppID))
	}
	h.GET("/api/agency-okr/people/search", requireEnabled, SearchWorkspacePeople(deps.People))
	h.GET("/api/agency-okr/people/avatars", requireEnabled, GetWorkspacePeopleAvatars(deps.People))
	h.GET("/api/agency-okr/plans", requireEnabled, ListOKRPlans(deps.Workspace))
	h.POST("/api/agency-okr/plans", requireEnabled, CreateOKRPlan(deps.Workspace))
	h.GET("/api/agency-okr/plans/:plan_id", requireEnabled, GetOKRPlan(deps.Workspace))
	h.PUT("/api/agency-okr/plans/:plan_id", requireEnabled, ReplaceOKRPlan(deps.Workspace))
	h.DELETE("/api/agency-okr/plans/:plan_id", requireEnabled, DeleteOKRPlan(deps.Workspace))
	h.GET("/api/agency-okr/scope", requireEnabled, GetWeeklyReportScope(deps.Workspace))
	h.GET("/api/agency-okr/board", requireEnabled, GetBoard(deps.Workspace))
	h.DELETE("/api/agency-okr/weeks/:week", requireEnabled, DeleteAgencyOKRWeek(deps.Workspace))
	h.GET("/api/agency-okr/core-board", requireEnabled, GetAgencyCoreBoard(deps.Workspace))
	h.GET("/api/agency-okr/krs/:kr_id", requireEnabled, GetAgencyCoreKR(deps.Workspace))
	h.GET("/api/agency-okr/krs/:kr_id/weekly", requireEnabled, GetAgencyWeeklyReportKR(deps.Workspace))
	h.POST("/api/agency-okr/objectives/:objective_id/krs", requireEnabled, CreateAgencyKR(deps.Workspace))
	h.PUT("/api/agency-okr/krs/:kr_id", requireEnabled, ReplaceCoreKR(deps.Workspace))
	h.PUT("/api/agency-okr/krs/:kr_id/tags", requireEnabled, ReplaceKRTags(deps.Workspace))
	h.PUT("/api/agency-okr/points/:point_id/tags", requireEnabled, ReplacePointTags(deps.Workspace))
	h.POST("/api/agency-okr/preview-review", requireEnabled, RunPreviewReview(deps.PreviewReview))
	h.GET("/api/agency-okr/comments", requireEnabled, GetComments(deps.Workspace))
	h.POST("/api/agency-okr/comments", requireEnabled, requireIdentity, CreateComment(deps.Workspace))
	h.PUT("/api/agency-okr/comments/:comment_id", requireEnabled, UpdateComment(deps.Workspace))
	h.DELETE("/api/agency-okr/comments/:comment_id", requireEnabled, DeleteComment(deps.Workspace))
	h.GET("/api/agency-okr/follow-ups", requireEnabled, GetFollowUps(deps.Workspace))
	h.GET("/api/agency-okr/follow-ups/:follow_up_id", requireEnabled, GetFollowUp(deps.Workspace))
	h.POST("/api/agency-okr/follow-ups", requireEnabled, CreateFollowUp(deps.Workspace))
	h.PUT("/api/agency-okr/follow-ups/:follow_up_id", requireEnabled, UpdateFollowUp(deps.Workspace))
	h.DELETE("/api/agency-okr/follow-ups/:follow_up_id", requireEnabled, DeleteFollowUp(deps.Workspace))
	h.GET("/api/agency-okr/reminder-preview", requireEnabled, GetReminderPreview(deps.Workspace))
	h.GET("/api/agency-okr/reminder-batches", requireEnabled, GetReminderBatches(deps.Workspace))
	h.POST("/api/agency-okr/reminder-batches/generate", requireEnabled, GenerateReminderBatch(deps.Workspace))
	h.POST("/api/agency-okr/feishu-documents", requireEnabled, CreateOKRDocument(deps.Documents))
	h.GET("/api/agency-okr/points/:point_id/meego-preview", requireEnabled, GetMeegoPreview(deps.Workspace))
	h.GET("/api/agency-okr/meego-preview", requireEnabled, GetMeegoBatchPreview(deps.Workspace))
	h.POST("/api/agency-okr/meego-observations", requireEnabled, StoreMeegoObservation(deps.Workspace))
	h.POST("/api/agency-okr/points/:point_id/meego-confirm", requireEnabled, ConfirmMeegoProgress(deps.Workspace))
	h.PUT("/api/agency-okr/scores/:target_kind/:target_id", requireEnabled, ReplaceWeeklyScore(deps.Workspace))
	h.DELETE("/api/agency-okr/scores/:target_kind/:target_id", requireEnabled, DeleteWeeklyScore(deps.Workspace))
	return nil
}
