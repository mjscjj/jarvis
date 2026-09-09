package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	okrAuth "jarvis/internal/okrworkspace/auth"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type okrPlanAccess string

const (
	okrPlanAccessNone   okrPlanAccess = "none"
	okrPlanAccessViewer okrPlanAccess = "viewer"
	okrPlanAccessEditor okrPlanAccess = "editor"
)

const (
	okrPlanEditorUnionID = "on_94b5aa46ca92b7aecd01031e5b2f0dc4"
	okrPlanEditorEmail   = "chujiejie.1@bytedance.com"
)

// The Plan share audience is deliberately an explicit code-owned permission
// boundary. Feishu open_id values are scoped to one app, so authorization
// primarily uses union_id. Enterprise email is retained as a compatibility
// key for sessions created by an identity provider that returns it.
var okrPlanViewers = []struct {
	Name    string
	UnionID string
	Email   string
}{
	{Name: "吴拓", UnionID: "on_22765cca655d49a60d30e57829fea6b3", Email: "wu.tuo@bytedance.com"},
	{Name: "苏穆辰", UnionID: "on_33997edcabca96f8334179f5ef9fae09", Email: "sumuchen.001@bytedance.com"},
	{Name: "张月仁", UnionID: "on_25f0c17fe3684c2ec0b19b70ba552d8e", Email: "zhangyueren@bytedance.com"},
	{Name: "罗沙", UnionID: "on_3d1e63a3f2e3c3a80ff4cbe48dcfd2da", Email: "luosha.sha@bytedance.com"},
	{Name: "崔建勋", UnionID: "on_6bf0acbf786ca7db033d32cce08212d1", Email: "cuijianxun@bytedance.com"},
	{Name: "刘力华", UnionID: "on_f5cf7eed86a8b68f97248d51782f6ab5", Email: "liulihua.1728@bytedance.com"},
	{Name: "耿馨妍", UnionID: "on_debe16cac24f379e572bc027243941c2", Email: "gengxinyan@bytedance.com"},
	{Name: "刘洋", UnionID: "on_9b233082fd4d04708ef380d39c14efc2", Email: "liuyang.816@bytedance.com"},
	{Name: "刘寅", UnionID: "on_5a9ba5363a8a65741f600baa439d3543", Email: "liuyin.01@bytedance.com"},
}

func okrPlanAccessForUser(user okrAuth.User, identityConfigured bool) okrPlanAccess {
	if !identityConfigured {
		return okrPlanAccessEditor
	}
	unionID := strings.TrimSpace(user.UnionID)
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if unionID == okrPlanEditorUnionID || email == okrPlanEditorEmail {
		return okrPlanAccessEditor
	}
	for _, viewer := range okrPlanViewers {
		if unionID == viewer.UnionID || email != "" && email == viewer.Email {
			return okrPlanAccessViewer
		}
	}
	return okrPlanAccessNone
}

func RequireOKRPlanAccess(service *okrAuth.Service, required okrPlanAccess) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		user := jarvisOKRUser
		if service.Enabled() {
			session, err := service.Current(ctx, string(c.Cookie(okrAuth.CookieName)))
			if errors.Is(err, okrAuth.ErrUnauthenticated) {
				writeAPIError(c, consts.StatusUnauthorized, 40180, fmt.Errorf("请先使用飞书登录"))
				c.Abort()
				return
			}
			if err != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50083, err)
				c.Abort()
				return
			}
			user = session.User
		}

		access := okrPlanAccessForUser(user, service.Enabled())
		if access == okrPlanAccessNone || required == okrPlanAccessEditor && access != okrPlanAccessEditor {
			writeAPIError(c, consts.StatusForbidden, 40380, fmt.Errorf("你不在 OKR Plan 的%s名单中", map[okrPlanAccess]string{okrPlanAccessViewer: "查看", okrPlanAccessEditor: "编辑"}[required]))
			c.Abort()
			return
		}
		c.Set(okrIdentityContextKey, user)
		c.Next(ctx)
	}
}
