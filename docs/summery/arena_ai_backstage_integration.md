# Arena Backstage AI 接入方案

> 结论：沿用 ReuseInA 既定身份模型接入。AI Tenant 按公会隔离，AI UserID 固定为 `999999999`。当前最新 `master` 已经把 Unary AI 接口注册到 Arena 网关前缀下；需要补齐的核心是 Chat/SSE 路由、POST 放行策略和端到端链路验收。

## 1. 接入目标与既定前提

### 1.1 目标

让通过 Arena 登录且拥有目标公会权限的运营，在 Backstage in Arena 页面使用现有 Backstage AI 能力，包括：

- 会话历史、消息、配置、快捷入口、QA Case 等查询接口；
- 会话置顶、取消置顶、重命名、删除、反馈、已读等操作接口；
- Chat SSE 流式对话接口。

### 1.2 既定身份模型

| 字段 | 取值与来源 | 用途 |
|---|---|---|
| 公会身份 | 网关校验后的 `faction-id` | 权限判断和公会上下文 |
| AI Tenant | 当前公会对应的 Tenant，例如 `backstage-agency-{AgencyID}` | 公会之间的数据隔离 |
| AI UserID | 固定 `999999999` | ReuseInA 统一系统账号；同一公会内共享该账号语义 |
| 实际操作人 | Arena SSO 对应员工身份 | 权限校验、审计、排障，不作为 AI UserID |
| 流量来源 | `TCNContext.SubTrafficSource = reuse_in_arena` | 流量识别、监控和审计 |

Tenant 和 UserID 都由服务端根据已校验的公会上下文生成，不由前端决定。若请求携带同名覆盖 Header，生产链路应忽略或覆盖，确保请求始终符合上述既定模型。

## 2. 当前代码状态

### 2.1 已经生效的部分

`api_agency_console/router.go` 在 `/api/arena/backstage_biz_api` 下注册了 `api_backstage/router.RegisterRouter`。而 `api_backstage/router/router.go` 已调用 `ai.AgentRegister(w)`。

因此，下列 Wrapper/Unary AI 接口会自动获得 Arena 前缀，不需要再次重复注册：

- GET：会话历史、会话消息、AI 配置、Chip 列表、QA Case 列表和详情；
- POST：置顶、取消置顶、重命名、删除会话、消息反馈、标记已读。

网关中间件已经完成 Arena Session 解析、公会权限检查，并写入 `AgencyID`、`UserID=999999999` 和 `reuse_in_arena` 流量标识。

### 2.2 尚未打通的部分

Chat 使用 Native Hertz/SSE 注册链路。当前存在三个实际问题：

1. `ai.Register(s)` 内部硬编码原 Backstage 路径，没有使用 Arena 前缀；
2. `api_agency_console/router.go` 中 Native Hertz 的注册调用仍被注释；
3. Chat 当前使用原 Backstage/TikTok Session 中间件，不能直接用于 Arena SSO 上下文。

另外，Arena 网关对 GET 默认放行，对非 GET 默认拦截。Chat 和会话操作都是 POST，需要配置明确的路由白名单。

## 3. 请求流程

1. 用户通过 Arena SSO 登录，在页面选择目标公会。
2. 前端请求携带 `faction-id`、`x-region`，访问 `/api/arena/backstage_biz_api/...`。
3. Arena 网关校验登录态，并确认实际操作人拥有该公会权限。
4. 网关写入服务端上下文：`AgencyID=faction-id`、`UserID=999999999`、`SubTrafficSource=reuse_in_arena`。
5. AI Handler 使用 `AgencyID` 生成公会 Tenant，使用固定 UserID 调用 AI 服务。
6. Unary 接口返回普通 HTTP 响应；Chat 接口保持 SSE 流并持续返回增量内容。
7. 日志、审计和指标同时记录公会、实际操作人、系统账号、路由和 TraceID。

## 4. 具体接入方式

### 4.1 Unary 接口

保留现有注册链路，不新增一套 Arena AI Router：

```text
api_agency_console.register
  -> Group(/api/arena/backstage_biz_api)
  -> api_backstage/router.RegisterRouter
  -> ai.AgentRegister
```

这样可以继续复用 Backstage AI Handler，避免两套路由后续出现行为差异。

### 4.2 Chat/SSE 接口

将 Native Hertz 的 AI 注册方法改为接收 `pathPrefix`，最终注册路径应为：

```text
/api/arena/backstage_biz_api/creators/live/union_platform_api/agency/ai/chat
```

注册时使用 Arena 已建立的 SessionContext 和公会权限上下文，不再套用原 Backstage/TikTok 登录中间件。Handler 内部继续复用现有 Chat 处理逻辑和 AI RPC。

建议的代码形态：

```go
func Register(s *server.Hertz, pathPrefix string, opts ...app.HandlerFunc) {
    path := pathPrefix + "/creators/live/union_platform_api/agency/ai/chat"
    s.POST(path, append(opts, Chat)...)
}
```

具体签名可以按现有 Router 封装调整，但必须满足两点：路径前缀可注入，中间件由调用方选择。

### 4.3 AI Identity 组装

AI Handler 从服务端上下文组装：

```text
TenantID = backstage-agency-{AgencyID}
UserID   = 999999999
Channel  = backstage / arena_backstage（按下游统计口径确认）
```

实际员工身份继续放在审计字段或 Operator 字段中。不要用实际员工 ID 替换 AI UserID，也不增加员工级会话隔离。

### 4.4 网关放行

继续使用现有 TCC 策略：

- GET 查询接口保持默认放行，可通过黑名单紧急关闭；
- Chat 和需要开放的 POST 操作接口加入 `reuse_in_arena_white_list`；
- 白名单必须使用网关实际匹配到的标准化路径；
- 配置前先在测试环境回读命中结果，避免路径前缀或尾斜杠不一致。

建议第一批开放 GET 查询和 Chat；会话修改类 POST 在确认产品页面确实使用后按接口逐项加入白名单。

## 5. 关键工程点

### 5.1 SSE 链路

- 网关和上游代理关闭响应缓冲，支持 `text/event-stream`；
- Read/Idle Timeout 覆盖最长对话时长；
- 客户端断开后及时取消下游 RPC；
- 配置单公会并发、单请求时长和下游超时，避免长连接挤占实例；
- 透传统一 TraceID，记录首包耗时、总耗时、断流率和下游错误码。

### 5.2 下游依赖

确认 Arena 运行环境可以访问 AI RPC 及其依赖，并完成对应服务身份、ACL、区域和部署配置。代码路由注册成功不代表下游调用一定可用，这部分必须通过真实环境联调验证。

### 5.3 审计与日志

每次请求至少能关联：

- 实际操作人员工身份；
- `AgencyID` / AI Tenant；
- 固定 AI UserID `999999999`；
- 接口路径、TraceID、结果和耗时。

日志不记录完整 Prompt、完整模型响应或敏感业务字段；排障需要内容时使用受控采样。

## 6. 实施顺序

1. 确认最新 `master` 中 Unary AI 路由的实际 URL，并在测试环境验证 GET 接口。
2. 改造 `ai.Register`：支持 Arena 前缀和调用方中间件，恢复 Arena Native Hertz 注册。
3. 固化 AI Identity：Tenant 取已校验公会，UserID 固定 `999999999`。
4. 配置 Chat 和必要 POST 路由白名单。
5. 完成 AI RPC ACL、区域和部署配置。
6. 做同公会、跨公会、无权限、公会切换和 SSE 断线测试。
7. 小流量开放，观察错误率、首包耗时、断流率和 AI 下游容量，再逐步扩大。

## 7. 验收标准

- 有权限的 Arena 用户能够访问目标公会 AI 页面，无权限用户被网关拒绝；
- 同一公会始终映射到同一 AI Tenant 和固定 UserID `999999999`；
- 同公会内共享系统账号语义符合预期，跨公会会话和数据不可串用；
- GET、允许的 POST 和 Chat 路由均命中正确 Handler；
- Chat SSE 能持续输出，超时、取消、断线和错误返回可控；
- 审计中能定位实际操作人、公会、系统账号和 TraceID；
- TCC 可按接口快速关闭，代码可通过关闭 Arena Chat 注册快速回滚。

## 8. 需要修改或确认的位置

| 位置 | 动作 |
|---|---|
| `app/api_backstage/biz/handler/ai/view.go` | Chat 注册支持 `pathPrefix` 和可选中间件 |
| `app/api_backstage/router/router.go` | Native Hertz 注册把前缀传给 AI Router |
| `app/api_agency_console/router.go` | 恢复 Arena Native Hertz 注册调用 |
| `app/api_backstage/biz/handler/ai/common.go` | 明确 Tenant 按已校验公会生成、UserID 固定 `999999999` |
| `reuse_in_arena_white_list` | 加入 Chat 和确需开放的 POST 路由 |
| AI 下游配置 | 完成 ACL、区域、容量和监控确认 |

这套方案不改变 ReuseInA 的租户和用户模型，只完成 AI 路由在 Arena 网关上的正确注册、放行和运行保障。
