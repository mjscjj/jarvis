package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/background"
	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type worldNodeView struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Data any    `json:"data"`
}

// ResolveWorldNode reads one known node from its authoritative module. It does
// not copy entities into a shared graph table or infer relationships.
func ResolveWorldNode(pages *background.PageService, okr *OKRModuleDependencies, bizOKR *BizOKRModuleDependencies) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		nodeType := strings.TrimSpace(c.Param("type"))
		id := strings.TrimSpace(c.Param("id"))
		if nodeType == "" || id == "" {
			writeAPIError(c, consts.StatusBadRequest, 40094, fmt.Errorf("type and id are required"))
			return
		}
		switch nodeType {
		case background.PageTypePrincipal, background.PageTypePerson, background.PageTypeProject,
			background.PageTypeKeyMatter, background.PageTypeProjectRisk, background.PageTypeProjectChange,
			background.PageTypeGroup, background.PageTypeResource:
			pageID, parseErr := strconv.ParseUint(id, 10, 64)
			if parseErr != nil || pageID == 0 {
				writeAPIError(c, consts.StatusBadRequest, 40094, fmt.Errorf("world entity id must be a positive integer"))
				return
			}
			if pages == nil {
				writeAPIError(c, consts.StatusNotFound, 40494, fmt.Errorf("world entity pages are unavailable"))
				return
			}
			page, pageErr := pages.GetPage(ctx, nodeType, pageID)
			if pageErr != nil {
				writeBackgroundError(c, pageErr)
				return
			}
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": worldNodeView{Type: nodeType, ID: id, Name: page.Name, Data: page}})
			return
		}
		if nodeType != "okr_objective" && nodeType != "okr_kr" && nodeType != "okr_point" &&
			nodeType != "biz_okr_plan_objective" && nodeType != "biz_okr_plan_kr" && nodeType != "biz_okr_plan_point" {
			writeAPIError(c, consts.StatusBadRequest, 40094, fmt.Errorf("unsupported world node type %q", nodeType))
			return
		}
		module := okr
		if nodeType == "biz_okr_plan_objective" || nodeType == "biz_okr_plan_kr" || nodeType == "biz_okr_plan_point" {
			if bizOKR == nil {
				module = nil
			} else {
				module = &OKRModuleDependencies{Workspace: bizOKR.Workspace, Enabled: bizOKR.Enabled}
			}
		}
		if module == nil || module.Workspace == nil || module.Enabled == nil {
			writeAPIError(c, consts.StatusNotFound, 40494, fmt.Errorf("owning OKR module is unavailable"))
			return
		}
		enabled, err := module.Enabled(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50094, err)
			return
		}
		if !enabled {
			writeAPIError(c, consts.StatusNotFound, 40494, fmt.Errorf("owning OKR module is disabled"))
			return
		}
		value, err := module.Workspace.ResolveWorldNode(ctx, nodeType, id)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40494, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50094, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": worldNodeView{Type: nodeType, ID: id, Name: value.Name, Data: value.Data}})
	}
}
