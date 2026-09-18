package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"jarvis/internal/execute"
	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/taskcreate"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const regionalAutoMatchPromptKey = "okr_agent_regional_auto_match"

func StartRegionalAutoMatch(workspace *okrworkspace.Service, submitter *taskcreate.Submitter, executor *execute.AgentExecutor, identity *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		user := currentOKRIdentity(c)
		if !canAutoMatchRegionalAlignment(user, identity.Enabled()) {
			writeAPIError(c, consts.StatusForbidden, 40391, fmt.Errorf("当前用户没有区域 OKR 自动匹配权限"))
			return
		}
		quarter, region := regionalScope(c)
		board, err := workspace.RegionalAlignmentBoard(ctx, quarter, region, user.OpenID)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40089, err)
			return
		}
		contextSnapshot := map[string]any{
			"module": "biz-okr", "skill": "okr-agent-orchestrator", "prompt_key": regionalAutoMatchPromptKey,
			"quarter": board.Alignment.Quarter, "region": board.Region.RegionCode, "match_version": board.MatchVersion,
		}
		background, err := json.Marshal(contextSnapshot)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50089, err)
			return
		}
		instruction := fmt.Sprintf("重新匹配 %s %s 区域需求与 Platform OKR；只按绑定 Prompt 判断，并通过原子匹配工具提交完整结果。", strings.ToUpper(board.Region.RegionCode), board.Alignment.Quarter)
		sourcePayload, err := json.Marshal(map[string]any{
			"instruction": instruction,
			"module":      "biz-okr", "skill": "okr-agent-orchestrator", "prompt_key": regionalAutoMatchPromptKey,
			"quarter": board.Alignment.Quarter, "region": board.Region.RegionCode,
			"regional_alignment_snapshot": board,
		})
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50089, err)
			return
		}
		task, err := submitter.Create(ctx, taskcreate.Input{
			Title:      fmt.Sprintf("OKR · %s %s 区域自动匹配", strings.ToUpper(board.Region.RegionCode), board.Alignment.Quarter),
			ActionType: "agent_task", Target: instruction, Background: background, SourcePayload: sourcePayload,
			SourceType: taskcreate.SourceManual, ActorType: "user", EventDetail: map[string]any{"channel": "okr_regional_alignment"},
		})
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50089, fmt.Errorf("create regional auto-match Task: %w", err))
			return
		}
		run, err := executor.KickExecute(ctx, execute.ExecuteInput{TaskID: task.ID})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusAccepted, map[string]any{"code": 0, "data": map[string]any{
			"task_id": task.ID, "status": run.Status, "match_version": board.MatchVersion,
		}})
	}
}

func ReplaceRegionalMatches(workspace *okrworkspace.Service, activity *okrworkspace.ActivityStore, identityConfigured bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		user := currentOKRIdentity(c)
		if user.OpenID != jarvisOKRUser.OpenID && !canAutoMatchRegionalAlignment(user, identityConfigured) {
			writeAPIError(c, consts.StatusForbidden, 40392, fmt.Errorf("当前用户没有区域 OKR 自动匹配权限"))
			return
		}
		var input okrworkspace.ReplaceRegionalMatchesInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40088, err)
			return
		}
		quarter, region := regionalScope(c)
		result, err := workspace.ReplaceRegionalMatches(ctx, quarter, region, user.OpenID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			writeAPIConflict(c, 40988, fmt.Errorf("区域对齐数据已变化，请基于最新 Part 0 重新匹配: %w", err), nil)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40088, err)
			return
		}
		if activity != nil {
			summary := fmt.Sprintf("自动匹配区域需求与 Platform OKR：%d 条需求变化，新增 %d 个、移除 %d 个关系", result.ChangedDemands, result.AddedLinks, result.RemovedLinks)
			if err := activity.Append(okrworkspace.ActivityEntry{ActorID: user.OpenID, ActorName: user.Name, Surface: "regional_alignment", Quarter: strings.TrimSpace(quarter), Action: "auto_matched", TargetID: strings.ToLower(strings.TrimSpace(region)), Summary: summary}); err != nil {
				slog.Error("append regional auto-match activity failed", "error", err)
			}
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
