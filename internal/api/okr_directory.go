package api

import (
	"context"
	"fmt"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"jarvis/internal/larkcli"
	"strings"
	"sync"
)

func SearchOKRDirectory(directory *larkcli.Directory) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if directory == nil {
			writeAPIError(c, 503, 50371, fmt.Errorf("OKR 人员目录未配置"))
			return
		}
		people, more, err := directory.SearchPage(ctx, c.Query("q"))
		if err != nil {
			hlog.CtxErrorf(ctx, "OKR directory search: %v", err)
			writeAPIError(c, 502, 50271, fmt.Errorf("暂时无法查询人员，请检查目录权限或输入更完整的邮箱"))
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"candidates": people, "has_more": more}})
	}
}

func GetOKRDirectoryAvatars(directory *larkcli.Directory) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if directory == nil {
			writeAPIError(c, 503, 50371, fmt.Errorf("OKR 人员目录未配置"))
			return
		}
		emails := []string{}
		seen := map[string]bool{}
		for _, email := range strings.Split(c.Query("emails"), ",") {
			email = strings.TrimSpace(email)
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			emails = append(emails, email)
		}
		people := make([]larkcli.DirectoryPerson, len(emails))
		var wait sync.WaitGroup
		for index, email := range emails {
			wait.Add(1)
			go func() {
				defer wait.Done()
				person, err := directory.Avatar(ctx, email)
				if err == nil {
					people[index] = person
				}
			}()
		}
		wait.Wait()
		resolved := people[:0]
		for _, person := range people {
			if person.Email != "" {
				resolved = append(resolved, person)
			}
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"people": resolved}})
	}
}
