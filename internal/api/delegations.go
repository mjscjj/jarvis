package api

import (
	"context"
	"errors"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"jarvis/internal/delegation"
	"strconv"
)

func delegationID(c *app.RequestContext) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("todo_id"), 10, 64)
	if err != nil || id == 0 {
		return 0, delegation.ErrInvalid
	}
	return id, nil
}
func delegationError(c *app.RequestContext, err error) {
	status := consts.StatusInternalServerError
	switch {
	case errors.Is(err, delegation.ErrInvalid):
		status = consts.StatusBadRequest
	case errors.Is(err, delegation.ErrConflict):
		status = consts.StatusConflict
	case errors.Is(err, delegation.ErrNotFound):
		status = consts.StatusNotFound
	}
	writeAPIError(c, status, status, err)
}
func ListDelegations(s *delegation.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		page, err := positiveQueryInt(c.Query("page"), 1, "page")
		if err != nil {
			writeAPIError(c, 400, 400, err)
			return
		}
		limit, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
		if err != nil {
			writeAPIError(c, 400, 400, err)
			return
		}
		v, err := s.List(ctx, delegation.Filter{State: c.Query("state"), Query: c.Query("query"), Page: page, Limit: limit})
		if err != nil {
			delegationError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": v})
	}
}
func GetDelegation(s *delegation.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := delegationID(c)
		if err != nil {
			delegationError(c, err)
			return
		}
		v, err := s.Get(ctx, id)
		if err != nil {
			delegationError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": v})
	}
}
func UpdateDelegation(s *delegation.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := delegationID(c)
		if err != nil {
			delegationError(c, err)
			return
		}
		var in delegation.UpdateInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, 400, 400, err)
			return
		}
		v, err := s.Update(ctx, id, in)
		if err != nil {
			delegationError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": v})
	}
}
func ListDelegationTasks(s *delegation.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := delegationID(c)
		if err != nil {
			delegationError(c, err)
			return
		}
		page, err := positiveQueryInt(c.Query("page"), 1, "page")
		if err != nil {
			writeAPIError(c, 400, 400, err)
			return
		}
		limit, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
		if err != nil {
			writeAPIError(c, 400, 400, err)
			return
		}
		items, err := s.Tasks(ctx, id, page, limit)
		if err != nil {
			delegationError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}
