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

const maxOKRAvatarBatch = 8

type okrAvatarDirectory interface {
	Avatar(context.Context, string) (larkcli.DirectoryPerson, error)
}

type okrAvatarFailure struct {
	email string
	err   error
}

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
	if directory == nil {
		return getOKRDirectoryAvatars(nil)
	}
	return getOKRDirectoryAvatars(directory)
}

func getOKRDirectoryAvatars(directory okrAvatarDirectory) app.HandlerFunc {
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
		if len(emails) == 0 || len(emails) > maxOKRAvatarBatch {
			writeAPIError(c, 400, 40071, fmt.Errorf("emails must contain 1-%d unique addresses", maxOKRAvatarBatch))
			return
		}
		people := make([]larkcli.DirectoryPerson, len(emails))
		failures := make(chan okrAvatarFailure, len(emails))
		var wait sync.WaitGroup
		for index, email := range emails {
			wait.Add(1)
			go func() {
				defer wait.Done()
				person, err := directory.Avatar(ctx, email)
				if err != nil {
					failures <- okrAvatarFailure{email: email, err: err}
					return
				}
				people[index] = person
			}()
		}
		wait.Wait()
		close(failures)
		failedEmails := []string{}
		for failure := range failures {
			hlog.CtxErrorf(ctx, "OKR directory avatar email=%s: %v", failure.email, failure.err)
			failedEmails = append(failedEmails, failure.email)
		}
		if len(failedEmails) == len(emails) {
			writeAPIError(c, 502, 50272, fmt.Errorf("暂时无法读取人员头像，请稍后重试"))
			return
		}
		resolved := people[:0]
		for _, person := range people {
			if person.Email != "" {
				resolved = append(resolved, person)
			}
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"people": resolved, "failed_emails": failedEmails}})
	}
}
