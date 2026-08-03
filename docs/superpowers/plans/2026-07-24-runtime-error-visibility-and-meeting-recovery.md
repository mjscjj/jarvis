# Runtime Error Visibility and Meeting Recovery Implementation Plan

> Status: implemented-history / partially obsolete
> Authority: non-normative implementation plan
> Last verified: 2026-08-02 @ `89fa24b`
> Warning: 不要重新执行本计划。运行错误部分已落地；会议专用模型和 M4 路径已被通用 clue 流水线替代。

> **For Codex:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让会议妙记无权限时稳定生成唯一的权限申请 Todo，并让 M3/M4/M5 与 cron 的未恢复运行错误在“运行状态”页面和侧边栏 Badge 中可见。

**Architecture:** 保留 `meeting_ingest` 作为可变当前状态、`message` 作为不可变证据；M3 通过已有宽松抽取协议理解 `meeting_capture_result`。运行错误继续以日志为唯一真源，在 `insight.DebugService` 内统一解析 cron/pipeline 行，用执行范围判断恢复并聚合重复错误，前端复用现有 `/api/debug/*` 和 `Debug.tsx`。

**Tech Stack:** Go 1.x、GORM、Hertz、React 19、TypeScript、Ant Design 6、Vite

---

## Task 1: 让 M3 明确识别会议权限阻塞证据

**Files:**
- Modify: `internal/extract/pipeline_types.go`
- Modify: `internal/extract/pipeline_store.go`
- Modify: `internal/extract/prompt.go`
- Modify: `internal/extract/prompt_test.go`
- Modify: `conf/prompts/m3-system-prompt.md`

### Step 1: 写失败测试，要求 Prompt 携带 message_type

在 `internal/extract/prompt_test.go` 新增测试：

```go
func TestBuildPromptCarriesMeetingCaptureResultType(t *testing.T) {
	unit := ConversationUnit{Key: "meeting", Messages: []MessageContext{{
		MessageID: "meeting-capture-result:meeting-1",
		MessageType: "meeting_capture_result",
		Content: "采集结果：permission_denied\nminute_token=minute-1",
		CreateTime: 1_700_000_001_000,
		IsNew: true,
		Extractable: true,
	}}}
	prompt, err := BuildPrompt(
		ChatBatch{Group: GroupContext{ChatID: "meeting:ou_me"}},
		unit,
		nil,
		time.Unix(1_700_000_100, 0),
		PromptOptions{PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000},
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt.User, "message_type=meeting_capture_result") {
		t.Fatalf("prompt missing message_type:\n%s", prompt.User)
	}
}
```

同时把现有 `TestExtractionPromptLeavesMeetingCaptureResultDecisionToAgent` 改成新契约测试，要求系统提示词包含：

```text
meeting_capture_result
permission_denied
manual_followup
```

并移除“禁止硬编码权限跟进”的旧断言。

### Step 2: 运行测试，确认先失败

Run:

```bash
GOCACHE=/private/tmp/jarvis-go-cache go test ./internal/extract -run 'TestBuildPromptCarriesMeetingCaptureResultType|TestExtractionPrompt'
```

Expected: FAIL，原因分别是 `MessageContext.MessageType` 不存在，以及提示词尚未要求权限 Todo。

### Step 3: 做最小实现

在 `MessageContext` 增加：

```go
MessageType string
```

在 `messageContext` 从 `domain.Message.MessageType` 投影该值：

```go
MessageType: message.MessageType,
```

在 `renderConversation` 的每条消息元数据中加入：

```text
message_type=<value>
```

更新 `conf/prompts/m3-system-prompt.md`：

- `meeting_minutes` 继续只从会议正文提取 principal 的明确交办。
- `meeting_capture_result + permission_denied` 本身是可执行线索。
- 必须生成一条 `manual_followup`，目标明确到 meeting、minute_token、`view` 权限和 `minutes +apply-permission`；具体 CLI 工具说明继续由运行时 ToolCatalog 注入。
- 标题与去重身份稳定，不要求会议正文。
- 不自动申请权限，只生成 Todo 进入现有审批链路。

不新增专用 DTO、数据库列或代码分支；动作语义仍由 M3 宽松 JSON 输出。

### Step 4: 运行相关测试

Run:

```bash
GOCACHE=/private/tmp/jarvis-go-cache go test ./internal/extract ./internal/meetingcapture
```

Expected: PASS；已有“首次失败只写一条证据、重复失败不重复写”测试继续通过。

### Step 5: 小步提交

仅暂存本任务文件，避开工作区中既有改动：

```bash
git add internal/extract/pipeline_types.go internal/extract/pipeline_store.go internal/extract/prompt.go internal/extract/prompt_test.go conf/prompts/m3-system-prompt.md
git commit -m "fix: create todo for blocked meeting capture"
git push
```

---

## Task 2: 统一解析 cron 和 pipeline 运行事件

**Files:**
- Create: `internal/insight/runtime_events.go`
- Create: `internal/insight/runtime_events_test.go`
- Modify: `internal/insight/debug_modules.go`

### Step 1: 写真实日志 fixture 的失败测试

在新测试文件中覆盖：

```go
func TestFailuresParsesPipelineAndUsesScopeRecovery(t *testing.T)
```

fixture 至少包含：

```text
pipeline 2026/07/24 15:42:01.000000 logid=log-m3-error stage=m3 trigger=realtime chat_id=oc_failed status=error error=decode JSON: EOF
pipeline 2026/07/24 15:43:01.000000 logid=log-other-ok stage=m3 trigger=realtime chat_id=oc_other status=ok created=1
pipeline 2026/07/24 15:44:01.000000 logid=log-m3-ok stage=m3 trigger=realtime chat_id=oc_failed status=ok created=1
pipeline 2026/07/24 15:45:01.000000 logid=log-m5-error stage=m5 trigger=queue task_id=78 version=0 status=error error=execute Task id=78: enrichments[2] content is blank
pipeline 2026/07/24 15:46:01.000000 logid=log-queued stage=m4 trigger=realtime todo_id=99 status=queued
meeting-capture-cron 2026/07/24 15:47:01.000000 logid=log-cron job=meeting_minutes status=error error=permission check failed
```

断言：

- M3、M5 和连字符 cron 都被解析。
- `queued` 不进入错误列表。
- `oc_other` 成功不能恢复 `oc_failed`；加入同 `oc_failed` 成功后才能恢复。
- 事件返回 `stage`、`scope_type`、`scope_id`、`trigger`、`logid`。

另写：

```go
func TestFailuresMergesRepeatedSameScopeAndSummary(t *testing.T)
```

同一 scope 和错误摘要连续出现两次时，只返回一项且 `count == 2`。

### Step 2: 运行测试，确认先失败

Run:

```bash
GOCACHE=/private/tmp/jarvis-go-cache go test ./internal/insight -run 'TestFailuresParsesPipeline|TestFailuresMerges'
```

Expected: FAIL，当前解析器只接受单词模块名的 `*-cron`，也没有 pipeline、scope 和 count。

### Step 3: 实现标准化运行事件

在 `runtime_events.go` 定义包内标准结构：

```go
type runtimeEvent struct {
	Time      string
	When      time.Time
	HasTime   bool
	Module    string
	Stage     string
	Status    string
	Job       string
	Trigger   string
	ScopeType string
	ScopeID   string
	ScopeKey  string
	LogID     string
	Error     string
	Fields    map[string]string
	Raw       string
}
```

实现：

```go
func parseRuntimeEvent(line LogLine) (runtimeEvent, bool)
func pipelineScope(stage string, fields map[string]string) (scopeType, scopeID, scopeKey string)
```

规则：

- cron 正则接受 `[a-z][a-z-]*`，模块身份为 `module + job`。
- pipeline 只接受 `stage=m3|m4|m5`。
- M3 用 `chat_id`；M4 用 `todo_id`，缺失时用 `trigger`；M5 用 `task_id`，缺失时用 `trigger`。
- `status=error` 是错误；`queued/ok/stale` 不是错误。
- 已识别 pipeline/cron 行但关键身份为空时，使用阶段或模块加 trigger/job 的保守 scope。
- `panic/fatal` 仅在能归属到已识别 runtime 行时进入时间线；不猜测任意第三方日志。

### Step 4: 让 Modules 和 Failures 消费统一事件

扩展 `FailureEvent`：

```go
Stage     string `json:"stage"`
Trigger   string `json:"trigger"`
ScopeType string `json:"scope_type"`
ScopeID   string `json:"scope_id"`
LogID     string `json:"logid"`
Count     int    `json:"count"`
```

恢复判断改为：

```go
lastOK[event.ScopeKey].After(failure.When)
```

而不是原来的 `lastOK[module]`。

重复合并键使用：

```text
scope_key + normalized error summary
```

保留最新时间、首条原始日志和累计次数；排序仍按最新发生时间倒序。

`Modules` 同时展示 pipeline 的 M3/M4/M5 最近运行，不再局限于 cron。

### Step 5: 运行完整 insight 测试

Run:

```bash
GOCACHE=/private/tmp/jarvis-go-cache go test ./internal/insight
```

Expected: PASS，包括现有 cron 与系统任务日志解析测试。

### Step 6: 小步提交

```bash
git add internal/insight/runtime_events.go internal/insight/runtime_events_test.go internal/insight/debug_modules.go
git commit -m "feat: expose scoped pipeline failures"
git push
```

不要暂存既有的 `internal/insight/logs.go` 和 `debug_modules_test.go` 改动。

---

## Task 3: 扩展运行状态页面

**Files:**
- Modify: `web/src/types.ts`
- Modify: `web/src/Debug.tsx`

### Step 1: 扩展前端类型

给 `FailureEvent` 增加：

```ts
stage: string
trigger: string
scope_type: string
scope_id: string
logid: string
count: number
```

### Step 2: 更新报错时间线

把说明从“cron 报错”改成“cron 与 M3/M4/M5 运行错误”，新增列：

- 阶段/模块
- 作用范围，例如 `chat_id=oc_xxx`
- logid
- 重复次数

`rowKey` 使用 `logid`，无 logid 时回退到 `time + module + scope_id + error`。

默认 Tab 改为：

```tsx
<Tabs defaultActiveKey="failures" ... />
```

API 读取失败继续展示错误 Alert，不显示“无错误”成功态。

### Step 3: 类型检查

Run:

```bash
npm --prefix web run typecheck
```

Expected: PASS。

### Step 4: 暂存策略

`web/src/types.ts` 当前已有用户暂存改动。本任务完成前先检查：

```bash
git diff --cached -- web/src/types.ts
git diff -- web/src/types.ts web/src/Debug.tsx
```

只在能明确隔离本任务 hunks 时提交；否则保留工作区修改，不把用户原有 staged hunk 混进本次 commit。

---

## Task 4: 侧边栏 60 秒轮询未恢复错误

**Files:**
- Create: `web/src/hooks/useRuntimeFailureCount.ts`
- Modify: `web/src/App.tsx`

### Step 1: 抽出可复用计数逻辑

新 hook：

```ts
export function useRuntimeFailureCount(intervalMs = 60_000) {
  const [count, setCount] = useState(0)
  const [error, setError] = useState<string>()
  // 首次立即读取，之后 setInterval；组件卸载时 abort + clearInterval。
  // 仅统计 !event.recovered。
  return { count, error }
}
```

接口失败时保留错误状态，不把失败响应当成 `count=0`。

### Step 2: 修改菜单名称和 Badge

- 导入 Ant Design `Badge`。
- “调试”改名为“运行状态”。
- `menuProps` 移入 `AppShell` 或改为接收 `failureCount`，只给运行状态菜单渲染红色 Badge。
- 折叠侧边栏时 Badge 仍可见。
- Badge 数量只统计未恢复错误。

### Step 3: TypeScript 构建

Run:

```bash
npm --prefix web run build
```

Expected: PASS。

### Step 4: 提交前保护现有改动

`App.tsx` 当前已有用户未提交修改。先比对现有 diff，保持 `Confirmations onDetailOpen` 等改动原样；若无法干净隔离索引，不提交用户修改，待最后统一向用户说明。

---

## Task 5: 回归、重建与真实运行验收

**Files:**
- Modify only if a failing test exposes a defect in the files above.

### Step 1: Go 全量测试

Run:

```bash
GOCACHE=/private/tmp/jarvis-go-cache go test ./...
```

Expected: PASS。

### Step 2: 前端全量构建

Run:

```bash
npm --prefix web run build
```

Expected: PASS。

### Step 3: 按项目规范重建服务

Run:

```bash
./scripts/rebuild-server.sh
```

Expected: 脚本成功完成签名稳定的重建和服务重启；禁止用裸 `go build` 替代。

### Step 4: 验证健康和错误 API

Run:

```bash
curl -fsS http://127.0.0.1:18800/healthz
curl -fsS 'http://127.0.0.1:18800/api/debug/status'
curl -fsS 'http://127.0.0.1:18800/api/debug/failures?hours=24'
curl -fsS 'http://127.0.0.1:18800/api/debug/modules'
```

Expected:

- `/healthz` 成功。
- SQLite、Qdrant、mem0 为 `ok`。
- 既有 M3/M5 pipeline 错误出现在 failures。
- 不同 scope 的成功不会错误恢复。
- 返回包含 `scope_id`、`logid`、`count`。

### Step 5: 浏览器验收

打开本机后台，确认：

1. 一级菜单显示“运行状态”。
2. 未恢复错误显示红色 Badge。
3. 进入页面默认打开“报错时间线”。
4. M3/M4/M5 行能看到 scope、logid、摘要和恢复状态。
5. 刷新/接口失败的 UI 状态正确。

### Step 6: 会议权限链路验收

优先使用测试夹具验证，不对真实飞书资源发起权限申请。确认：

- 一条 `meeting_capture_result + permission_denied` Prompt 明确产生 `manual_followup` 契约。
- 同一固定 evidence message 重复写入被现有唯一约束忽略。
- 已有 meetingcapture 重试与成功导入测试通过。

如需真实会议验收，只验证 Todo 生成到待审批，不点击批准、不执行外部权限申请。

### Step 7: 最终 diff 审计与提交

Run:

```bash
git status --short
git diff --check
git diff --stat
git log -5 --oneline
```

确认没有覆盖或误提交开发前已有的“系统任务/确认弹窗”改动。可隔离的实现按任务提交并 push；重叠且不能隔离的前端 hunks保持未提交，并在交付说明中列明。
