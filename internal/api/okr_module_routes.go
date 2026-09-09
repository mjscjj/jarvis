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
	Activity  *okrworkspace.ActivityStore
	Enabled   func(context.Context) (bool, error)
}

// BizOKRModuleDependencies keeps organization-specific product behavior
// behind its own module gate. It composes the reusable OKR workspace without
// becoming a second source of truth for objectives, KRs, or formal progress.
type BizOKRModuleDependencies struct {
	Workspace *okrworkspace.Service
	Activity  *okrworkspace.ActivityStore
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
	h.PATCH("/api/okr/points/:point_id/definition", requireEnabled, PatchPointDefinition(deps.Workspace))
	h.DELETE("/api/okr/krs/:kr_id", requireEnabled, DeleteKR(deps.Workspace))
	// Formal OKR time-series data remains available without Biz OKR.
	h.GET("/api/okr/progress/scope", requireEnabled, GetWeeklyReportScope(deps.Workspace))
	h.GET("/api/okr/progress/board", requireEnabled, GetProgressBoard(deps.Workspace))
	h.GET("/api/okr/weeks", requireEnabled, GetWeeklyReportWeeks(deps.Workspace))
	h.POST("/api/okr/weeks", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "weekly", Action: "week_opened"}, OpenWeeklyReportWeek(deps.Workspace)))
	// Deleting a whole week is Biz-owned. See okrworkspace.DeleteWeek.
	h.GET("/api/okr/krs/:kr_id/weekly", requireEnabled, GetWeeklyReportKR(deps.Workspace))
	h.PUT("/api/okr/krs/:kr_id/weekly-core", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "weekly", Action: "weekly_core_saved", TargetParam: "kr_id"}, ReplaceWeeklyKRCore(deps.Workspace)))
	h.POST("/api/okr/points/:point_id/progress", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "weekly", Action: "progress_created", TargetParam: "point_id"}, CreateWeeklyProgressEntry(deps.Workspace)))
	h.PUT("/api/okr/progress/:progress_id", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "weekly", Action: "progress_updated", TargetParam: "progress_id"}, UpdateWeeklyProgressEntry(deps.Workspace)))
	h.DELETE("/api/okr/progress/:progress_id", requireEnabled, recordOKRActivity(deps.Activity, progressDeleteActivitySpec(deps.Workspace), DeleteWeeklyProgressEntry(deps.Workspace)))
	return nil
}

func RegisterBizOKRModuleRoutes(h *server.Hertz, deps BizOKRModuleDependencies) error {
	if h == nil || deps.Workspace == nil || deps.Identity == nil || deps.Documents == nil || deps.People == nil {
		return fmt.Errorf("register Biz OKR module routes: required dependency is nil")
	}
	if deps.PreviewReview == nil {
		return fmt.Errorf("register Biz OKR module routes: preview review service is nil")
	}
	if deps.Enabled == nil {
		return fmt.Errorf("register Biz OKR module routes: enablement gate is nil")
	}
	requireEnabled := func(ctx context.Context, c *app.RequestContext) {
		enabled, err := deps.Enabled(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50088, err)
			c.Abort()
			return
		}
		if !enabled {
			writeAPIError(c, consts.StatusNotFound, 40488, fmt.Errorf("OKR module is disabled"))
			c.Abort()
			return
		}
		c.Next(ctx)
	}
	requireIdentity := RequireOKRIdentity(deps.Identity)
	h.GET("/api/biz-okr/me", requireEnabled, GetOKRCurrentUser(deps.Identity))
	h.POST("/api/biz-okr/auth/feishu/device", requireEnabled, BeginOKRFeishuDeviceLogin(deps.Identity))
	h.POST("/api/biz-okr/auth/feishu/device/:login_id/poll", requireEnabled, PollOKRFeishuDeviceLogin(deps.Identity))
	h.POST("/api/biz-okr/auth/logout", requireEnabled, LogoutOKR(deps.Identity))
	if deps.UserTokens != nil {
		h.GET("/api/biz-okr/feishu-identity", requireEnabled, GetOKRFeishuIdentity(deps.UserTokens, deps.Tokens, deps.FeishuAppID))
	}
	// Deprecated browser-cache compatibility route; see SearchWorkspacePeople.
	h.GET("/api/biz-okr/people/search", requireEnabled, SearchWorkspacePeople(deps.People))
	h.GET("/api/biz-okr/people/avatars", requireEnabled, GetWorkspacePeopleAvatars(deps.People))
	h.GET("/api/biz-okr/activity", requireEnabled, GetOKRActivities(deps.Activity))
	h.GET("/api/biz-okr/plans", requireEnabled, ListOKRPlans(deps.Workspace))
	h.POST("/api/biz-okr/plans", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "plan", Action: "plan_created"}, CreateOKRPlan(deps.Workspace)))
	h.GET("/api/biz-okr/plans/:plan_id", requireEnabled, GetOKRPlan(deps.Workspace))
	h.GET("/api/biz-okr/plans/:plan_id/comments", requireEnabled, GetOKRPlanComments(deps.Workspace))
	h.POST("/api/biz-okr/plans/:plan_id/comments", requireEnabled, requireIdentity, CreateOKRPlanComment(deps.Workspace))
	h.POST("/api/biz-okr/plans/:plan_id/objectives", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "plan", Action: "objective_created", TargetParam: "plan_id"}, CreateOKRPlanObjective(deps.Workspace)))
	h.PATCH("/api/biz-okr/plans/:plan_id/objectives/:objective_id", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "plan", Action: "objective_saved", TargetParam: "objective_id"}, UpdateOKRPlanObjective(deps.Workspace)))
	h.PATCH("/api/biz-okr/plans/:plan_id/points/:point_id/definition", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "plan", Action: "point_definition_saved", TargetParam: "point_id"}, PatchPlanPointDefinition(deps.Workspace)))
	h.DELETE("/api/biz-okr/plans/:plan_id/objectives/:objective_id", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "plan", Action: "objective_deleted", TargetParam: "objective_id"}, DeleteOKRPlanObjective(deps.Workspace)))
	h.PUT("/api/biz-okr/plans/:plan_id/objectives/order", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "plan", Action: "objective_reordered", TargetParam: "plan_id"}, ReorderOKRPlanObjectives(deps.Workspace)))
	h.DELETE("/api/biz-okr/plans/:plan_id", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "plan", Action: "plan_deleted", TargetParam: "plan_id"}, DeleteOKRPlan(deps.Workspace)))
	h.GET("/api/biz-okr/scope", requireEnabled, GetWeeklyReportScope(deps.Workspace))
	h.GET("/api/biz-okr/board", requireEnabled, GetBoard(deps.Workspace))
	h.DELETE("/api/biz-okr/weeks/:week", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "weekly", Action: "week_deleted", TargetParam: "week"}, DeleteBizOKRWeek(deps.Workspace)))
	h.GET("/api/biz-okr/core-board", requireEnabled, GetBizCoreBoard(deps.Workspace))
	h.GET("/api/biz-okr/krs/:kr_id", requireEnabled, GetBizCoreKR(deps.Workspace))
	h.GET("/api/biz-okr/krs/:kr_id/weekly", requireEnabled, GetBizWeeklyReportKR(deps.Workspace))
	h.POST("/api/biz-okr/objectives/:objective_id/krs", requireEnabled, CreateBizKR(deps.Workspace))
	h.PUT("/api/biz-okr/krs/:kr_id", requireEnabled, ReplaceCoreKR(deps.Workspace))
	h.PUT("/api/biz-okr/krs/:kr_id/tags", requireEnabled, ReplaceKRTags(deps.Workspace))
	h.PUT("/api/biz-okr/points/:point_id/tags", requireEnabled, ReplacePointTags(deps.Workspace))
	h.POST("/api/biz-okr/preview-review", requireEnabled, RunPreviewReview(deps.PreviewReview))
	h.GET("/api/biz-okr/comments", requireEnabled, GetComments(deps.Workspace))
	h.POST("/api/biz-okr/comments", requireEnabled, requireIdentity, CreateComment(deps.Workspace))
	h.PUT("/api/biz-okr/comments/:comment_id", requireEnabled, requireIdentity, UpdateComment(deps.Workspace))
	h.DELETE("/api/biz-okr/comments/:comment_id", requireEnabled, requireIdentity, DeleteComment(deps.Workspace))
	h.GET("/api/biz-okr/follow-ups", requireEnabled, GetFollowUps(deps.Workspace))
	h.GET("/api/biz-okr/follow-ups/:follow_up_id", requireEnabled, GetFollowUp(deps.Workspace))
	h.POST("/api/biz-okr/follow-ups", requireEnabled, CreateFollowUp(deps.Workspace))
	h.PUT("/api/biz-okr/follow-ups/:follow_up_id", requireEnabled, UpdateFollowUp(deps.Workspace))
	h.DELETE("/api/biz-okr/follow-ups/:follow_up_id", requireEnabled, DeleteFollowUp(deps.Workspace))
	h.GET("/api/biz-okr/reminder-preview", requireEnabled, GetReminderPreview(deps.Workspace))
	h.GET("/api/biz-okr/reminder-batches", requireEnabled, GetReminderBatches(deps.Workspace))
	h.POST("/api/biz-okr/reminder-batches/generate", requireEnabled, GenerateReminderBatch(deps.Workspace))
	h.POST("/api/biz-okr/feishu-documents", requireEnabled, CreateOKRDocument(deps.Documents))
	h.GET("/api/biz-okr/points/:point_id/meego-preview", requireEnabled, GetMeegoPreview(deps.Workspace))
	h.GET("/api/biz-okr/meego-preview", requireEnabled, GetMeegoBatchPreview(deps.Workspace))
	h.POST("/api/biz-okr/meego-observations", requireEnabled, StoreMeegoObservation(deps.Workspace))
	h.POST("/api/biz-okr/points/:point_id/meego-confirm", requireEnabled, ConfirmMeegoProgress(deps.Workspace))
	h.PUT("/api/biz-okr/scores/:target_kind/:target_id", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "weekly", Action: "score_saved", TargetParam: "target_id"}, ReplaceWeeklyScore(deps.Workspace)))
	h.DELETE("/api/biz-okr/scores/:target_kind/:target_id", requireEnabled, recordOKRActivity(deps.Activity, okrActivitySpec{Surface: "weekly", Action: "score_deleted", TargetParam: "target_id"}, DeleteWeeklyScore(deps.Workspace)))
	return nil
}
