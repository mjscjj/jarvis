package api

import (
	"context"
	"errors"
	"strings"

	"jarvis/internal/okrworkspace"
	"jarvis/internal/okrworkspace/domain"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type weeklyScoreRequest struct {
	Quarter         string  `json:"quarter"`
	Week            string  `json:"week"`
	Score           float64 `json:"score"`
	ExpectedVersion int32   `json:"expected_version"`
}

type deleteWeeklyScoreRequest struct {
	Quarter         string `json:"quarter"`
	Week            string `json:"week"`
	ExpectedVersion int32  `json:"expected_version"`
}

func ReplaceWeeklyScore(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		kind := domain.WeeklyScoreTargetKind(strings.TrimSpace(c.Param("target_kind")))
		targetID := strings.TrimSpace(c.Param("target_id"))
		var request weeklyScoreRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40058, err)
			return
		}
		input := okrworkspace.WeeklyScoreInput{
			Quarter: request.Quarter, Week: request.Week, TargetKind: kind, TargetID: targetID,
			Score: request.Score, ExpectedVersion: request.ExpectedVersion, UpdatedBy: currentOKRIdentity(c).OpenID,
		}
		result, err := service.ReplaceWeeklyScore(ctx, input)
		if writeWeeklyScoreError(ctx, c, service, kind, targetID, request.Week, 58, err) {
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteWeeklyScore(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		kind := domain.WeeklyScoreTargetKind(strings.TrimSpace(c.Param("target_kind")))
		targetID := strings.TrimSpace(c.Param("target_id"))
		var request deleteWeeklyScoreRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40059, err)
			return
		}
		input := okrworkspace.DeleteWeeklyScoreInput{
			Quarter: request.Quarter, Week: request.Week, TargetKind: kind, TargetID: targetID,
			ExpectedVersion: request.ExpectedVersion, UpdatedBy: currentOKRIdentity(c).OpenID,
		}
		result, err := service.DeleteWeeklyScore(ctx, input)
		if writeWeeklyScoreError(ctx, c, service, kind, targetID, request.Week, 59, err) {
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func writeWeeklyScoreError(ctx context.Context, c *app.RequestContext, service *okrworkspace.Service, kind domain.WeeklyScoreTargetKind, targetID, week string, suffix int, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, okrworkspace.ErrConflict) {
		var current okrworkspace.KRView
		var currentErr error
		switch kind {
		case domain.WeeklyScoreTargetKR:
			current, currentErr = service.GetKR(ctx, targetID, week)
		case domain.WeeklyScoreTargetPoint:
			current, currentErr = service.GetKRByPoint(ctx, targetID, week)
		default:
			writeAPIError(c, consts.StatusBadRequest, 40000+suffix, err)
			return true
		}
		if currentErr != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50000+suffix, currentErr)
			return true
		}
		writeAPIConflict(c, 40900+suffix, err, current)
		return true
	}
	if errors.Is(err, okrworkspace.ErrNotFound) {
		writeAPIError(c, consts.StatusNotFound, 40400+suffix, err)
		return true
	}
	writeAPIError(c, consts.StatusBadRequest, 40000+suffix, err)
	return true
}
