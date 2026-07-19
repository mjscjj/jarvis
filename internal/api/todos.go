package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/extract"

	"github.com/cloudwego/hertz/pkg/app"
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
	c.JSON(status, map[string]any{"code": code, "msg": err.Error()})
}
