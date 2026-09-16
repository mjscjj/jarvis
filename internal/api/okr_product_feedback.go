package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type createProductFeedbackRequest struct {
	Title         string                      `json:"title"`
	Content       string                      `json:"content"`
	Images        []okrworkspace.CommentImage `json:"images"`
	SourceContext json.RawMessage             `json:"source_context"`
}

type productFeedbackReplyRequest struct {
	Content string `json:"content"`
}

type productFeedbackStatusRequest struct {
	ExpectedVersion int32 `json:"expected_version"`
	Resolved        *bool `json:"resolved"`
}

func ListProductFeedback(service *okrworkspace.Service, identityConfigured bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		resolved := false
		if raw := strings.TrimSpace(c.Query("resolved")); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40090, fmt.Errorf("resolved must be true or false"))
				return
			}
			resolved = value
		}
		offset, err := feedbackQueryInt(c.Query("offset"), 0)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40090, err)
			return
		}
		limit, err := feedbackQueryInt(c.Query("limit"), 50)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40090, err)
			return
		}
		result, err := service.ProductFeedbacks(ctx, currentFeedbackActor(c, identityConfigured), okrworkspace.ProductFeedbackQuery{
			Resolved: resolved, Sort: strings.TrimSpace(c.Query("sort")), Offset: offset, Limit: limit,
		})
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40090, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateProductFeedback(service *okrworkspace.Service, identityConfigured bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request createProductFeedbackRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40091, err)
			return
		}
		result, err := service.CreateProductFeedback(ctx, currentFeedbackActor(c, identityConfigured), okrworkspace.CreateProductFeedbackInput{
			Title: request.Title, Content: request.Content, Images: request.Images, SourceContext: request.SourceContext,
		})
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40091, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func ReplyProductFeedback(service *okrworkspace.Service, identityConfigured bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request productFeedbackReplyRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40092, err)
			return
		}
		result, err := service.ReplyProductFeedback(ctx, currentFeedbackActor(c, identityConfigured), c.Param("feedback_id"), request.Content)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40492, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40092, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func AddProductFeedbackPlusOne(service *okrworkspace.Service, identityConfigured bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.AddProductFeedbackPlusOne(ctx, currentFeedbackActor(c, identityConfigured), c.Param("feedback_id"))
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40493, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40093, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func RemoveProductFeedbackPlusOne(service *okrworkspace.Service, identityConfigured bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.RemoveProductFeedbackPlusOne(ctx, currentFeedbackActor(c, identityConfigured), c.Param("feedback_id"))
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40494, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40094, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func SetProductFeedbackStatus(service *okrworkspace.Service, identityConfigured bool) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request productFeedbackStatusRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40095, err)
			return
		}
		if request.Resolved == nil {
			writeAPIError(c, consts.StatusBadRequest, 40095, fmt.Errorf("resolved is required"))
			return
		}
		result, err := service.SetProductFeedbackResolved(ctx, currentFeedbackActor(c, identityConfigured), c.Param("feedback_id"), request.ExpectedVersion, *request.Resolved)
		switch {
		case errors.Is(err, okrworkspace.ErrNotFound):
			writeAPIError(c, consts.StatusNotFound, 40495, err)
			return
		case errors.Is(err, okrworkspace.ErrFeedbackForbidden):
			writeAPIError(c, consts.StatusForbidden, 40395, err)
			return
		case errors.Is(err, okrworkspace.ErrConflict):
			writeAPIConflict(c, 40995, err, nil)
			return
		case err != nil:
			writeAPIError(c, consts.StatusBadRequest, 40095, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func currentFeedbackActor(c *app.RequestContext, identityConfigured bool) okrworkspace.FeedbackActor {
	user := currentOKRIdentity(c)
	return okrworkspace.FeedbackActor{
		OpenID: user.OpenID, UnionID: user.UnionID, Email: user.Email, Name: user.Name, AvatarURL: user.AvatarURL,
		CanManage: canManageOKR(user, identityConfigured),
	}
}

func feedbackQueryInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("pagination values must be non-negative integers")
	}
	return value, nil
}
