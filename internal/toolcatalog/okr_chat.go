package toolcatalog

import (
	"net/http"
	"path"
	"strings"
)

// OKRChatRoutes is deliberately independent of StageChat: trusted pipeline
// agents keep their complete catalog. These exact routes never invoke a host
// agent, read principal messages, or dispatch external notifications.
func OKRChatRoutes() []string {
	return []string{
		"GET /api/okr/enums", "GET /api/okr/scope", "GET /api/okr/board",
		"POST /api/okr/objectives", "PUT /api/okr/objectives/order",
		"PUT /api/okr/objectives/{id}", "DELETE /api/okr/objectives/{id}",
		"POST /api/okr/objectives/{id}/krs", "PUT /api/okr/objectives/{id}/kr-order",
		"GET /api/okr/krs/{id}", "PUT /api/okr/krs/{id}", "DELETE /api/okr/krs/{id}",
		"PUT /api/okr/krs/{id}/definition", "PATCH /api/okr/points/{id}/definition",
		"GET /api/okr/progress/scope", "GET /api/okr/progress/board",
		"GET /api/okr/weeks", "POST /api/okr/weeks", "GET /api/okr/krs/{id}/weekly",
		"PUT /api/okr/krs/{id}/weekly-core", "POST /api/okr/points/{id}/progress",
		"PUT /api/okr/progress/{id}", "DELETE /api/okr/progress/{id}",
		"GET /api/biz-okr/scope", "GET /api/biz-okr/board", "GET /api/biz-okr/core-board",
		"GET /api/biz-okr/krs/{id}", "GET /api/biz-okr/krs/{id}/weekly",
		"POST /api/biz-okr/objectives/{id}/krs", "PUT /api/biz-okr/krs/{id}",
		"PUT /api/biz-okr/krs/{id}/tags", "PUT /api/biz-okr/points/{id}/tags",
		"GET /api/biz-okr/plans", "POST /api/biz-okr/plans",
		"GET /api/biz-okr/plans/{id}", "DELETE /api/biz-okr/plans/{id}",
		"GET /api/biz-okr/plans/{id}/comments", "POST /api/biz-okr/plans/{id}/objectives",
		"PATCH /api/biz-okr/plans/{id}/objectives/{objective}", "DELETE /api/biz-okr/plans/{id}/objectives/{objective}",
		"PUT /api/biz-okr/plans/{id}/objectives/order", "PATCH /api/biz-okr/plans/{id}/points/{point}/definition",
		"GET /api/biz-okr/comments", "GET /api/biz-okr/meego-preview", "GET /api/biz-okr/points/{id}/meego-preview",
		"GET /api/biz-okr/follow-ups", "POST /api/biz-okr/follow-ups",
		"GET /api/biz-okr/follow-ups/{id}", "PUT /api/biz-okr/follow-ups/{id}", "DELETE /api/biz-okr/follow-ups/{id}",
		"PUT /api/biz-okr/scores/{kind}/{id}", "DELETE /api/biz-okr/scores/{kind}/{id}",
	}
}

func OKRChatAllowed(method, resource string) bool {
	// No normalization after checking: Hertz and net/http must see one spelling.
	if path.Clean(resource) != resource || strings.ContainsAny(resource, "%\\\x00") {
		return false
	}
	for _, route := range OKRChatRoutes() {
		verb, pattern, _ := strings.Cut(route, " ")
		if verb != method {
			continue
		}
		want, got := strings.Split(pattern, "/"), strings.Split(resource, "/")
		if len(want) != len(got) {
			continue
		}
		match := true
		for i := range want {
			if want[i] != got[i] && !(strings.HasPrefix(want[i], "{") && got[i] != "") {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	if method == http.MethodGet {
		for _, key := range []string{"okr_agent_principles", "okr_agent_quarterly_draft", "okr_agent_region_alignment", "okr_agent_meego_alignment", "okr_agent_report_a", "okr_agent_report_b", "okr_agent_report_c", "okr_agent_weekly_reminder", "okr_agent_progress_sync", "okr_agent_plan_review", "okr_agent_progress_review"} {
			if resource == "/api/text-files/"+key {
				return true
			}
		}
	}
	return false
}

func OKRChatBlock() string {
	return "## 前端开发\n/opt/jarvis/web 是当前站点完整 web/ 目录的可写挂载，包含源码、node_modules 和线上 dist；所有 OKR 对话共同修改同一目录，不是副本。可新增页面、Tab 和组件。检查：npm --prefix /opt/jarvis/web run typecheck；构建：npm --prefix /opt/jarvis/web run build。构建直接更新线上静态文件，无需重启后端。沿用已有依赖；当前网络不开放 npm 下载。新增 OKR Tab 的入口是 src/okr/navigation.ts、src/okr/OKRModule.tsx，页面组件在 src/okr/。后端源码、配置和 Git 元数据未挂载，不能修改后端或执行 Git 提交。\n\n## OKR 工具入口\n使用 /opt/jarvis/scripts/okr-module-tools、biz-okr-tools、okr-agent-tools；JARVIS_API_BASE 已设置。可自由运行挂载的脚本和 JavaScript，但数据入口只开放以下 method/path（其余返回 403）。业务 Prompt 可通过 okr-agent-tools prompt 读取固定 OKR key。\n\n" + strings.Join(OKRChatRoutes(), "\n")
}
