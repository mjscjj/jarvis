package api

import (
	"strings"

	okrAuth "jarvis/internal/okrworkspace/auth"
)

const (
	okrPlanEditorUnionID = "on_94b5aa46ca92b7aecd01031e5b2f0dc4"
	okrPlanEditorEmail   = "chujiejie.1@bytedance.com"
)

// The management audience controls whether the definition-management and
// tagging workspace is shown. Feishu open_id values are scoped to one app, so
// identity matching primarily uses union_id. Enterprise email is retained as a
// compatibility key for sessions created by an identity provider that returns
// it. Plan, Review and weekly collaboration are deliberately not restricted by
// this list.
var okrManagementUsers = []struct {
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
	{Name: "张若怡", UnionID: "on_833914b05fbbe2eb6623865af52d984f", Email: "ruoyizhang@bytedance.com"},
}

func canManageOKR(user okrAuth.User, identityConfigured bool) bool {
	if !identityConfigured {
		return true
	}
	unionID := strings.TrimSpace(user.UnionID)
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if unionID == okrPlanEditorUnionID || email == okrPlanEditorEmail {
		return true
	}
	for _, manager := range okrManagementUsers {
		if unionID == manager.UnionID || email != "" && email == manager.Email {
			return true
		}
	}
	return false
}
