package api

import (
	"context"
	"fmt"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"jarvis/internal/larkcli"
	"strings"
)

func SearchOKRDirectory(directory *larkcli.Directory) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if directory == nil {
			writeAPIError(c, 503, 50371, fmt.Errorf("OKR 人员目录未配置"))
			return
		}
		people, err := directory.Search(ctx, c.Query("q"))
		if err != nil {
			hlog.CtxErrorf(ctx, "OKR directory search: %v", err)
			writeAPIError(c, 502, 50271, fmt.Errorf("暂时无法查询人员，请检查目录权限或输入更完整的邮箱"))
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"candidates": people, "has_more": false}})
	}
}

func GetOKRDirectoryAvatars(directory *larkcli.Directory) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if directory == nil {
			writeAPIError(c, 503, 50371, fmt.Errorf("OKR 人员目录未配置"))
			return
		}
		people := []larkcli.DirectoryPerson{}
		for _, email := range strings.Split(c.Query("emails"), ",") {
			if strings.TrimSpace(email) == "" {
				continue
			}
			person, err := directory.Avatar(ctx, email)
			if err != nil {
				continue
			}
			people = append(people, person)
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"people": people}})
	}
}
