package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/extract"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ListTodos(reader extract.TodoReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := todoListFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40001, err)
			return
		}
		if err := extract.ValidateTodoFilter(filter); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40001, err)
			return
		}
		result, err := reader.ListTodos(ctx, filter)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50001, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetTodo(reader extract.TodoReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(c.Param("todo_id"), 10, 64)
		if err != nil || id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40002, fmt.Errorf("todo_id must be a positive integer"))
			return
		}
		result, err := reader.GetTodo(ctx, id)
		if errors.Is(err, extract.ErrTodoNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40401, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50002, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type setTodoStatusRequest struct {
	Status string `json:"status"`
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

// SetTodoStatus is shared by the Todo list (the principal parking or reviving a
// clue) and by M5 through jarvis-tools, so the caller names itself in actor.
func SetTodoStatus(writer extract.TodoStatusWriter) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(c.Param("todo_id"), 10, 64)
		if err != nil || id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40002, fmt.Errorf("todo_id must be a positive integer"))
			return
		}
		var req setTodoStatusRequest
		if err := decodeStrictJSON(c.Request.Body(), &req); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40003, err)
			return
		}
		result, err := writer.SetTodoStatus(ctx, extract.TodoStatusInput{
			TodoID: id, Status: req.Status, Actor: req.Actor, Reason: req.Reason,
		})
		if errors.Is(err, extract.ErrTodoNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40401, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40004, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func todoListFilter(c *app.RequestContext) (extract.TodoListFilter, error) {
	page, err := positiveQueryInt(c.Query("page"), 1, "page")
	if err != nil {
		return extract.TodoListFilter{}, err
	}
	pageSize, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
	if err != nil {
		return extract.TodoListFilter{}, err
	}
	statuses, err := extract.ParseStatuses(c.Query("status"))
	if err != nil {
		return extract.TodoListFilter{}, err
	}
	filter := extract.TodoListFilter{
		Statuses:   statuses,
		ActionType: strings.TrimSpace(c.Query("action_type")),
		Page:       page,
		PageSize:   pageSize,
	}
	if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value == 0 {
			return extract.TodoListFilter{}, fmt.Errorf("project_id must be a positive integer")
		}
		filter.ProjectID = &value
	}
	if raw := strings.TrimSpace(c.Query("leader_only")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return extract.TodoListFilter{}, fmt.Errorf("leader_only must be true or false")
		}
		filter.LeaderOnly = &value
	}
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		from, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return extract.TodoListFilter{}, fmt.Errorf("from must be RFC3339: %w", err)
		}
		filter.From = &from
	}
	if raw := strings.TrimSpace(c.Query("until")); raw != "" {
		until, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return extract.TodoListFilter{}, fmt.Errorf("until must be RFC3339: %w", err)
		}
		filter.Until = &until
	}
	return filter, nil
}

func positiveQueryInt(raw string, defaultValue int, name string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func writeAPIError(c *app.RequestContext, status, code int, err error) {
	ctx := observability.FromRequestContext(context.Background(), c)
	logID := observability.LogID(ctx)
	method := string(c.Request.Header.Method())
	path := string(c.Request.URI().PathOriginal())
	if status >= consts.StatusInternalServerError {
		hlog.CtxErrorf(ctx, "api request failed status=%d code=%d method=%s path=%s error=%+v", status, code, method, path, err)
	} else {
		hlog.CtxWarnf(ctx, "api request rejected status=%d code=%d method=%s path=%s error=%+v", status, code, method, path, err)
	}
	c.JSON(status, map[string]any{"code": code, "msg": err.Error(), "logid": logID})
}

// decodeStrictJSON decodes one request body and rejects anything the target does
// not declare, so a typo in a field name fails loudly instead of being ignored.
func decodeStrictJSON(body []byte, target any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing request body: %w", err)
	}
	return nil
}
