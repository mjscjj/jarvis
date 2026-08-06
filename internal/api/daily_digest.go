package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/dailydigest"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// DailyDigestService 是 API 层依赖的每日总结能力接口，便于 handler 单测打桩。
// dailydigest.Service 满足它。
type DailyDigestService interface {
	ListByDate(ctx context.Context, date string) ([]dailydigest.DigestView, error)
	KickGenerateOne(ctx context.Context, scope, scopeID, date string) (*dailydigest.KickResult, error)
}

type generateDailyDigestRequest struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
	Date    string `json:"date"`
}

// GetDailyDigests 返回某天全部 scope 的每日总结（个人 + 各关键群）。date 缺省=今天
// （本地时区）。只返回已存在的行；前端自己知道哪些关键群应该有。
func GetDailyDigests(service DailyDigestService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		date := strings.TrimSpace(string(c.Query("date")))
		items, err := service.ListByDate(ctx, date)
		if err != nil {
			if errors.Is(err, dailydigest.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40030, err)
				return
			}
			writeAPIError(c, consts.StatusInternalServerError, 50030, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

// GenerateDailyDigest 异步触发单条生成/重算，立即返回 status=generating，前端轮询
// GetDailyDigests 拿最新状态。body: {scope, scope_id, date}，date 缺省=今天。
func GenerateDailyDigest(service DailyDigestService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request generateDailyDigestRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, err)
			return
		}
		if strings.TrimSpace(request.Scope) == "" || strings.TrimSpace(request.ScopeID) == "" {
			writeAPIError(c, consts.StatusBadRequest, 40031, fmt.Errorf("scope and scope_id are required"))
			return
		}
		result, err := service.KickGenerateOne(ctx, request.Scope, request.ScopeID, strings.TrimSpace(request.Date))
		if err != nil {
			if errors.Is(err, dailydigest.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40032, err)
				return
			}
			if errors.Is(err, dailydigest.ErrAlreadyGenerating) {
				writeAPIError(c, consts.StatusConflict, 40930, err)
				return
			}
			writeAPIError(c, consts.StatusInternalServerError, 50031, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
