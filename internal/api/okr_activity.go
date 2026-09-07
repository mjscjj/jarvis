package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type okrActivitySpec struct {
	Surface      string
	Action       string
	TargetParam  string
	ResolveScope func(context.Context, *app.RequestContext) (string, string, error)
}

func GetOKRActivities(store *okrworkspace.ActivityStore) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		if store == nil {
			writeAPIError(c, consts.StatusInternalServerError, 50077, fmt.Errorf("OKR activity store is not configured"))
			return
		}
		limit := 50
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				writeAPIError(c, consts.StatusBadRequest, 40077, fmt.Errorf("limit must be between 1 and 100"))
				return
			}
			limit = parsed
		}
		entries, err := store.List(okrworkspace.ActivityQuery{
			Surface: strings.TrimSpace(c.Query("surface")), Quarter: strings.TrimSpace(c.Query("quarter")),
			Week: strings.TrimSpace(c.Query("week")), PlanID: strings.TrimSpace(c.Query("plan_id")), Limit: limit,
		})
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50077, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": entries}})
	}
}

// recordOKRActivity decorates existing write handlers without moving business
// semantics into the audit file. Only successful HTTP writes are recorded.
func recordOKRActivity(store *okrworkspace.ActivityStore, spec okrActivitySpec, next app.HandlerFunc) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		body := map[string]any{}
		_ = json.Unmarshal(c.Request.Body(), &body)
		resolvedQuarter, resolvedWeek := "", ""
		if store != nil && spec.ResolveScope != nil {
			var err error
			resolvedQuarter, resolvedWeek, err = spec.ResolveScope(ctx, c)
			if err != nil {
				slog.Warn("resolve OKR activity scope failed", "action", spec.Action, "error", err)
			}
		}
		next(ctx, c)
		status := c.Response.StatusCode()
		if store == nil || status < 200 || status >= 300 {
			return
		}
		response := struct {
			Data map[string]any `json:"data"`
		}{}
		_ = json.Unmarshal(c.Response.Body(), &response)
		if spec.Action == "week_opened" {
			if created, ok := response.Data["created"].(bool); ok && !created {
				return
			}
		}
		identity := currentOKRIdentity(c)
		entry := okrworkspace.ActivityEntry{
			ActorID: identity.OpenID, ActorName: identity.Name, Surface: spec.Surface,
			Action: spec.Action, TargetID: strings.TrimSpace(c.Param(spec.TargetParam)),
			Quarter: activityString(body["quarter"]), Week: activityString(body["week"]),
			Summary: okrActivitySummary(spec.Action, body),
		}
		if spec.Surface == "plan" {
			entry.PlanID = strings.TrimSpace(c.Param("plan_id"))
			if entry.PlanID == "" {
				entry.PlanID = activityString(response.Data["id"])
			}
		}
		if entry.Quarter == "" {
			entry.Quarter = activityString(response.Data["quarter"])
			if entry.Quarter == "" {
				entry.Quarter = resolvedQuarter
			}
			if entry.Quarter == "" {
				entry.Quarter = strings.TrimSpace(c.Query("quarter"))
			}
		}
		if entry.Week == "" {
			entry.Week = resolvedWeek
			if entry.Week == "" {
				entry.Week = strings.TrimSpace(c.Param("week"))
			}
		}
		if err := store.Append(entry); err != nil {
			slog.Error("append OKR activity failed", "action", spec.Action, "error", err)
		}
	}
}

func progressDeleteActivitySpec(service *okrworkspace.Service) okrActivitySpec {
	return okrActivitySpec{
		Surface: "weekly", Action: "progress_deleted", TargetParam: "progress_id",
		ResolveScope: func(ctx context.Context, c *app.RequestContext) (string, string, error) {
			return service.ProgressEntryScope(ctx, strings.TrimSpace(c.Param("progress_id")))
		},
	}
}

func activityString(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func okrActivitySummary(action string, body map[string]any) string {
	switch action {
	case "plan_created":
		return fmt.Sprintf("新建 Plan「%s」", activityString(body["title"]))
	case "plan_saved":
		return fmt.Sprintf("保存 Plan「%s」", activityString(body["title"]))
	case "plan_deleted":
		return "删除 Plan"
	case "week_opened":
		if activityString(body["template_key"]) == "okr_weekly_preview_v1" {
			return "新建 Review 周次"
		}
		return "新建周报周次"
	case "week_deleted":
		return "删除当前周次"
	case "weekly_core_saved":
		return "更新 KR 核心数据"
	case "progress_created":
		return "新增进展：" + activityString(body["text"])
	case "progress_updated":
		return "更新进展：" + activityString(body["text"])
	case "progress_deleted":
		return "删除进展"
	case "score_saved":
		return fmt.Sprintf("更新评分为 %v", body["score"])
	case "score_deleted":
		return "清除评分"
	default:
		return action
	}
}
