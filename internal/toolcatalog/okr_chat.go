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

// EmilyDevelopmentBlock describes the full development instance, not the
// production OKR-only HTTP whitelist used by the original runtime.
func EmilyDevelopmentBlock() string {
	return `## Emily 完整研发环境
/opt/jarvis 是共享的完整源码 worktree，可以修改前后端、配置、测试和构建脚本。当前在独立开发容器内。
开发站点地址读取 conf/config.yaml 的 server.public_base_url；路径由部署配置生成，不假定固定前缀。源码改动通过 ./scripts/jarvis-deploy --skip-pull 构建并重启开发实例，不修改生产主服务。
JARVIS_API_BASE=http://127.0.0.1:18812，jarvis-tools、okr-module-tools、biz-okr-tools 均访问开发实例，所有开发 API 可用。
主库 var/development.db 是开发专用库，可放任务和消息的测试数据；不含生产任务、消息、普通会话或世界模型。
data/okr/ 是线上 OKR 数据、图片和业务资源的共享目录，允许直接查询、修改和迁移；修改立即影响线上。feishu-tokens 使用开发实例自己的目录。
Go、Node、npm、SQLite 已安装，可运行完整项目测试。npm 下载和 Go 模块下载走受限网络出口。
lark-cli 可使用现有通知机器人发送和回读通知，并查询通讯录；不提供 principal 的个人消息和私有文件授权。网页登录用户自己的 OKR 授权由开发实例保存。
容器没有宿主 Docker socket、生产主库、生产日志或宿主个人凭证。不要请求宿主 Agent 代跑。开发实例的任务/消息测试与生产私有数据是两回事。
Git worktree 的正式提交和合并由宿主开发流程处理；尊重目录中已有修改。
`
}
