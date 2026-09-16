# Jarvis · 主动式任务数字分身

Jarvis 是运行在本地可信环境中的个人任务 Agent。它持续接收工作事实，维护世界状态，判断哪些事情值得推进，调用工具完成工作，并把结果沉淀回来。

## 阅读入口

1. [项目目标](goal.md)：稳定愿景与成功标准
2. [Agent 关键原则](AGENTS.md)：每次进入仓库必须携带的八条约束；详细规范见本文后半部分
3. [当前架构](docs/00-overview.md)：跨模块数据流与硬边界
4. [文档导航](docs/README.md)：当前文档、提案、研究与交付物
5. [OKR 模块当前实现](docs/modules/06-okr.md)：通用 OKR、Biz OKR 与世界模型的维护边界
6. [Emily 完整研发环境](docs/summery/emily-development-environment.md)：开发 worktree、共享 OKR 数据、实例配置与提交规则
7. [OKR 真实路径测试规范](docs/summery/okr-real-path-testing-standard.md)：本人现有授权、评论与导出验收、部署后回归和证据要求
8. [OKR 最新全量验收](docs/summery/2026-09-16-okr-full-acceptance.md)：真实站、隔离浏览器和自动化测试结果

## 核心链路

```text
飞书 Bot WebSocket ─> CC Connect
                       ├─ 当前会话可直接完成 ─> 即时回复
                       └─ 长期、多步或有副作用 ─> manual Task ─┐

飞书轮询 / 外部 Skill ─> M2 原始事实 ─> M3 准入 ─> Todo ────┤
                                                            v
                                                     M5 执行 Agent
                                         调查 ─> 动作 ─> 验证 ─> 沉淀
```

- **M2 机械采集**：保留原始事实，不解释错误、不判断价值。
- **M3 最短准入**：只调查到足以决定 `extracted` 或 `observing`。
- **Todo 固化**：`extracted` Todo 按 ID/version 幂等创建 Task，不再经过模型闸门。
- **M5 完整执行**：调查现实、调整目标、选择工具、处理等待和人工问题，直到得到真实结果。
- **世界模型**：实体页回答“现在是什么”，Fact 指向“发生过什么”的原始证据；EntityRelation 保存可查询的明确映射，WorldProgress 保存证据化周期判断。
- **主动巡视**：看护未闭环工作；需要改变外部世界时创建普通 Task 交给 M5。

请示副作用不是固定流程阶段。M5 根据即将发生的具体动作判断是否需要询问 principal；需要时统一返回 `needs_human + question`，回答恢复同一个 Agent Session。

## Source Of Truth

| 主题 | 权威来源 |
|---|---|
| 项目目标 | `goal.md` |
| Agent 关键原则 | `AGENTS.md` |
| Agent 详细开发规范 | `README.md` 的“AI Agent 详细开发规范” |
| OKR 测试与真实验收 | [OKR 真实路径测试规范](docs/summery/okr-real-path-testing-standard.md) |
| 当前跨模块架构 | `docs/00-overview.md` |
| 数据模型与迁移 | `internal/domain/`, `internal/store/sqlite.go` |
| OKR 产品模块 | `internal/okrworkspace/`, `internal/okrreview/`, `conf/okr-module.yaml`, `data/okr/`；本机应用绑定在 `conf/okr-module.runtime.yaml` |
| HTTP 路由 | `internal/api/router.go` |
| 基线与本机配置 | `conf/config.yaml`, `conf/config.runtime.yaml` |
| Agent 行为 | `conf/prompts/`, `conf/rules/`, `.agents/skills/` |
| Agent 工具 | `internal/toolcatalog/`, `scripts/jarvis-tools` |
| 飞书应用与登录身份 | [双飞书应用身份](docs/design-dual-app-identity.md)，`conf/okr-module.yaml` 与 `conf/okr-feishu-scopes.txt` |
| 页面入口 | `web/src/App.tsx` |
| 安装动作 | `scripts/jarvis-install`, `.agents/skills/install-jarvis/` |

文档不复制完整 DDL、路由、CLI help 或本机有效配置。

有效配置是 `conf/config.yaml` 与同目录 `conf/config.runtime.yaml` 的合并结果：runtime 按叶子 key 覆盖基线，未出现的 key 保留基线值，两个文件都拒绝未知字段。OKR 模块同理由 `conf/okr-module.yaml` 和本机 `conf/okr-module.runtime.yaml` 合并。**本机参数、身份和密钥写 runtime 文件，不改仓库基线。** runtime 文件不进 Git，权限保持 `600`。后台保存后需要重启；prompts、rules 和 Skills 按各自 reader 实时读取。

Jarvis 本体只服务 principal；OKR 页面登录使用独立低敏应用。**所有面向人员的外部消息、提醒和通知统一由“Jarvis通知机器人”发送**，不得使用网页登录应用、Emily Bot、目录查询应用或 principal 的用户身份代发。人员目标以完整企业邮箱为跨应用真源；某个应用产生的 `open_id` 只能交给同一应用使用，跨应用发送优先按企业邮箱投递，禁止复用 `open_id`。网页登录、白名单与授权 API 域名排查见 [网页 SSO 登录接入](docs/summery/sso-web-login.md)：当前使用 CLI 授权，已记录 CN / i18n 超时对比与实际地址配置方法；个人 JWT SDK 方案尚未实施，真实账号完整登录仍待验收。

`server.addr` 是实例后端监听地址的配置真源。主进程向 Agent 子进程导出 `JARVIS_API_BASE`、`JARVIS_CONFIG` 和仓库工具 PATH；切换工作目录不会切换实例。通用、世界模型、OKR/周报工具统一用 `scripts/jarvis-api-base`，优先采用继承地址，否则读取选定配置；配置错误直接失败，不扫描端口。模块工具的显式 `--base-url` 可指定其它实例。

服务名由配置文件绝对路径生成；改端口不改服务名，不同配置不共用服务。多实例仍需分开配置数据库与产物路径，外部账号、Qdrant collection 和 Bot 长连接不会自动隔离。给用户的页面与卡片链接优先使用 `server.public_base_url`。

## 常见修改入口

- M3 行为：`conf/prompts/m3-system-prompt.md`, `conf/rules/m3.md`, `internal/extract/`
- M5 行为：`conf/prompts/m5-system-prompt.md`, `conf/rules/m5.md`, `internal/execute/`
- 请示尺度：`conf/prompts/m5-approval-policy.md`
- CC 前台交互：`conf/prompts/cc-system-prompt.md`, `integrations/cc-connect/`
- 世界模型：`internal/background/`, `internal/progress/`, `internal/factengine/`
- 主动巡视：`conf/prompts/proactive-system-prompt.md`, `internal/proactive/`
- 插件：`internal/plugin/`, `conf/skills.yaml`, 对应 Skill
- OKR：`internal/okrworkspace/`, `internal/okrreview/`, `web/src/okr/`；生命周期由 `internal/appmodule/` 管理
- 运行配置：后台“系统设置”或 `conf/config.runtime.yaml`
- 网页身份与导航：`auth.feishu_accounts` 将已核验的飞书 union ID 绑定到企业账号；`auth.principals` 单独决定本实例私有页面准入；`server.main_workbench_accounts` 只决定导航偏好。真实人员配置写本机 runtime，不写入代码或基线；账号绑定缺省为空，没有内置人员回退。维护步骤见 [OKR 入口方案第 14 节](docs/summery/2026-09-16-okr-development-entry.md#14-人员配置与分支维护)。
- Web：`web/src/`

各模块的稳定契约见 [docs/modules](docs/README.md#当前实现)。

## 安装与运行

macOS 14+ Apple Silicon 用户优先使用 [DMG 安装与更新](docs/reference/macos-install-and-update.md)。

源码安装从完整 checkout 开始，在仓库根目录让 Agent 执行：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

该 Skill 负责依赖、飞书身份、CC Connect、服务、世界模型和真实端到端验收。不要绕过依赖门、身份和 CC 绑定，在 fresh clone 上直接注册服务。安装清单记录在 `var/install/<run-id>/INSTALL_CHECKLIST.md`。

要求 Go 1.26.4 或更高版本、C 编译器、满足 Vite engines 的 Node（`^20.19.0` 或 `>=22.12.0`）/npm、jq、git、lark-cli、有效配置选定的 Agent CLI 和 Qdrant。SQLite 驱动依赖 CGO，构建脚本通过 `scripts/check-build-toolchain.sh` 检查。通用运行数据库在本机创建；可选 OKR 模块的产品数据库及资源随仓库保存在 `data/okr/`。完整研发实例和 Dev worktree 都使用 OKR MVP worktree 的实时目录；产品数据只由 OKR MVP 分支提交一致快照，再合入 main。

`bind-cc` 会立即验证 App ID/Secret。已有 Feishu `allow_from` 不是 Principal 本人时会停止，明确确认替换后才可使用 `--replace-allow-from`；`validate-binding` 拒绝缺失或通配的白名单。首次安装动作和 CLI 参数以安装 Skill、脚本 help 为准。

### 改完代码怎么生效

两个系统统一执行：

```bash
./scripts/jarvis-deploy --skip-pull
```

它安装并构建前端、编译主服务、按当前系统重启选定实例，最后验证首页、`/healthz` 与 `/readyz`。macOS 走稳定签名与 launchd，Linux 走 user systemd。`scripts/rebuild-server.sh` 是 macOS 底层脚本，不能在 Linux 使用，也不要裸 `go build` 覆盖运行二进制后重启。

`--skip-pull` 部署当前工作树；不带时先要求工作树干净并 fast-forward pull。`--remote-okr-db` 只用于明确放弃本机 OKR 数据库改动、使用 Git 版本的场景；通常必须保留并提交 OKR 产品数据。

重启前查询 `/api/tasks?status=executing`。macOS 底层脚本会拒绝中断执行中 Task，API 不可达也会 fail-fast；Linux 没有这道检查，重启会打断当前执行子进程。Chat 与主服务共用进程和 `/api/chat/*`，重启会中断当前轮次，已保存的历史保留。

```bash
./scripts/jarvis-instance conf/config.yaml
./scripts/jarvis-api-base
curl --fail "$(./scripts/jarvis-api-base)/healthz"
curl --fail "$(./scripts/jarvis-api-base)/readyz" | jq
```

服务名、地址、日志从配置派生，不手写固定值。Linux unit 会被部署脚本重新生成，实例额外环境变量（如 OKR 登录应用密钥）放同名 `.d/` 目录的 drop-in。完整服务管理、前端开发、退出和恢复见 [运行与部署](docs/reference/operations.md)。

## 开发验证

OKR 业务修改须遵循 [OKR 真实路径测试规范](docs/summery/okr-real-path-testing-standard.md)。使用已授权的 `chujiejie.1 / 储节节` 完成受影响的真实页面、后端和飞书路径；设备登录流程单独验收。模拟测试和健康检查不能代替真实业务结果。未运行、被跳过或只模拟通过的项目必须分别报告，不能计为真实验收通过。

`scripts/okr-real-acceptance` 是 OKR 真实浏览器测试入口：它核对本人既有飞书授权、签发短时测试会话、运行 `web/test/okrRealPath.browser.mjs`，再由 `scripts/okr-real-readback` 核对文档归属、正文及通知卡片。需配置可用的 Playwright 模块和 Chromium 路径；结果保存在不入库的 `var/okr-real-run.*`。短时会话只用于登录后的业务验收，不能证明设备登录本身通过。

```bash
go test ./cmd/... ./internal/...
npm --prefix web test
npm --prefix web run typecheck
git diff --check
```

需要真实外部服务或凭证的测试单独运行：

```bash
go test -tags=integration ./internal/...
```

前端改动还需启动开发服务并执行浏览器回归。一次性迁移、扫描和抽取 flags 以 `go run ./cmd/jarvis-server -h` 为准。

## 管理后台

主导航围绕对话、工作台、任务、已启用业务模块（如 OKR）、世界、插件、工作设定和系统管理组织。导航分组默认收起，之后在当前浏览器记住选择；工作台合并当日任务态势与历史回顾，自动化收进任务的二级页。

工作设定按任务执行、线索发现维护系统提示词、阶段规则、请示策略和生效预览。系统设置包含运行、调度、Skills、共享记忆、功能模块和应用版本。对话拥有独立持久会话与默认 Agent、模型、推理档位和执行边界，不复用 M3/M5 阶段语义；每个会话可单独选择。

完整 HTTP 能力分组见 [HTTP API](docs/reference/http-api.md)。

## 目录

```text
cmd/                 进程入口
internal/            后端模块
web/                 React + Vite 管理后台
conf/                基线配置、prompts、rules、Skills 和模块配置
.agents/skills/      Jarvis 领域 Skills
integrations/        外部集成与补丁
deploy/              服务模板
scripts/             安装、部署和 Agent 工具
docs/                当前架构、参考、提案、研究和正式交付物
data/                生成报告及随仓库提交的 OKR 产品数据库与资源
runs/                Agent 执行产物
var/                 本机日志、数据库与运行状态
```

## AI Agent 详细开发规范

[AGENTS.md](AGENTS.md) 只保留每次进入仓库都必须携带的八条关键原则。本节保存这些原则的完整解释、语义所有权表、反例和操作细则；需要设计或修改相关模块时按主题查阅，不需要全部注入每次 Agent 会话。

Jarvis 的本质不是‘规则引擎填表单’，而是一个**靠上下文推断、能自己用工具查信息来补全判断**的 agent。阶段职责约束的是目标、调查深度、停止条件和输出协议，不是工具权限；当前各阶段可以为完成职责自由使用全部可用工具。

### 0. 三项总原则

1. **Agent-first，工具与提示词优先。** 优先提供原子化工具，把判断、编排和审批尺度放到对应阶段的 prompt、rules 或 Skill，由 Agent 结合上下文执行；代码只保证 MVP 运行所需的状态、幂等、调度和工具参数等机器硬边界，不把语义流程写死，也不把阶段职责做成权限系统。
2. **MVP 优先，模块化与可编排性优先。** 从整体架构和语义所有权出发拆分模块；每个模块职责单一、接口清晰、可以独立组合，不为局部需求增加专用链路、跨层依赖或过早抽象。
3. **Markdown 优先。** 能用本地 Markdown 作为真源的文本和轻量配置，就不用数据库或重型存储；只有确需结构化查询、关联、幂等、调度或状态约束时，才引入结构化存储。

### 0.1 MVP 信任模型：不做 Agent 内部权限

当前阶段是单用户、本地、可信环境中的 MVP 搭建期。Jarvis 启动的 Agent、stage、Task 和本机工具都代表同一个 principal，不互相构成安全边界：

- **能力默认开放。** M3、M5、proactive、factengine、会议巡扫、晨报和其它 Agent 都可以按真实需要使用全部可用工具，读取或修改 Task、Todo、Fact、实体页、定时任务、配置和本地文件。不要为阶段单独维护工具白名单，也不要增加 `proactive-only`、`M3 read-only` 一类代码门禁。
- **不做内部身份鉴权。** `stage`、`actor`、`source_type`、Task ID 和环境变量用于提供上下文、路由和留痕，不是安全身份。当前不建设 Agent token、capability、角色权限、Task 所有权或“A Task 不能修改 P Task”之类访问控制。
- **阶段职责不是访问权限。** M2 机械采集、M3 最短调查、M5 深入执行、proactive 主动巡视、factengine 沉淀事实，这些边界由 prompt、rules 和 Skill 指导模型，目的是控制注意力和语义所有权，不应转成 API 拒绝、工具隐藏或代码分支。
- **不要为假想攻击面加固。** 不把可信本机 Agent 能直接调用内部 HTTP API、Shell、仓库文件或其它阶段使用的工具视为当前缺陷；不为恶意本机进程、Agent 冒充其它 stage、跨 Task 操作或局域网攻击设计认证授权系统。确有部署到非可信环境的需求时，再基于真实威胁模型单独设计。
- **仍保留最小运行硬边界。** 必要的参数校验、引用关系、状态流转、幂等、调度和执行留痕继续由代码保证，但只服务于正确运行和故障恢复，不服务于 Agent 间隔离。
- **审批不等于权限。** 是否针对具体外部副作用询问 principal，仍由执行 Agent 按审批策略结合上下文判断；这不限制 Agent 调用工具或修改内部状态，也不应实现成按 stage、Task 类型或角色划分的权限系统。

### 1. 先判所有权，再改内容

任何方案都先回答"这条语义由谁负责、最终在哪里生效"，再决定改哪个文件。不要从报错点、当前文件或最小 diff 反推所有权。

#### 1.1 先还原最终生效内容

动 prompt、rules、Skills、上下文或工具说明前，必须沿调用链确认：

1. 当前 Agent 属于哪个阶段，入口在哪里；
2. 系统提示词、当前阶段工作规则、工具目录、Skills、shared memory 和运行时上下文如何组装；
3. 同一语义是否已在别处存在，最终会不会重复、冲突或跨阶段泄漏；
4. 改动后的**完整有效输入**是什么，而不只是单个文件的 diff。

例如 `workrule.Service.Block()` 只把 `m3.md` 或 `m5.md` 注入对应阶段，proactive 只使用自己的系统提示词；因此规则必须直接写入真正拥有它的阶段，不能新增跨阶段工作规则再让下游抵消。

#### 1.2 配置按语义所有权分层

| 内容 | 唯一真源 | 不应放置的位置 |
|---|---|---|
| M3 或 M5 的预置阶段行为 | `conf/rules/m3.md`、`conf/rules/m5.md` | 跨阶段工作规则文件 |
| 阶段角色、目标、停止条件、输出协议 | `conf/prompts/*-system-prompt.md` | rules、Skill、业务上下文 |
| 工具能力、命令入口、阶段使用方式 | `internal/toolcatalog/` | prompt、rules 中复制工具手册 |
| 某来源或领域的可复用操作步骤与命令 | 对应 Skill | Go 专用分支、跨阶段规则 |
| principal 明确要求长期记住的个性化行为偏好 | `data/shared-memory.md`（最多 2000 字） | 业务事实、机器状态、系统 prompt |
| principal、项目、人物、群和业务事实 | M1/context、实体事实页与 Fact | 行为规则和系统提示词 |
| 审批判断尺度 | `conf/prompts/m5-approval-policy.md` | `action_type` 分支或普通 M5 rules |
| 状态、幂等、调度、工具参数等 MVP 运行硬边界 | runtime code/schema | 用自然语言规则假装保证 |

通用原则与阶段职责相遇时，**阶段职责限制通用原则的使用范围**。例如"主动查证"在 M3 中表示补齐准入所缺的最短证据，在 M5 中才表示围绕真实目标做深入、多跳调查。不要把两者理解成两条需要靠文本顺序互相覆盖的规则；它们本来就属于不同作用域。

#### 1.3 修所有权，不做反向补丁

- 内容放错层时，优先**移动或删除错误真源**，再在正确所有者处保留一份；不得保留错误规则，再让下游写"忽略上游""本阶段不适用"或增加代码例外来抵消。
- 一个语义只保留一个权威来源。若多个阶段都需要，分别放进各阶段的正确所有者；不要新增跨阶段工作规则，也不要把某阶段的完整流程提升到其它阶段。
- 修复必须消除产生问题的概念，而不是只压住当前症状。若删掉一条错误规则即可解决，不新增兼容层、fallback、状态或分支。
- 文档、实现和测试冲突时，不默认任何一边正确；先根据 `goal.md`、架构边界和真实调用链确认设计，再让三者一致。

#### 1.4 提交方案前自检

提出或实施方案前逐项检查：

1. **问题根因**：是哪条最终生效的指令或哪段代码制造了行为，而不只是在哪里观察到症状？
2. **语义所有者**：这项行为属于全局、阶段、工具、Skill、事实上下文还是机器协议？
3. **完整链路**：生产者、组装器、消费者和持久化真源是否都看过？
4. **最终效果**：改完后每个受影响阶段实际收到什么？是否仍有重复或矛盾？
5. **更简单替代**：能否通过删除、迁移或复用现有真源解决，而不是增加例外？
6. **验证方式**：是否有测试或可检查的最终 prompt/rules 组合证明边界真的成立？

任何出现"上游继续产生错误内容，下游新增判断把它过滤掉"的方案，默认视为设计失败，除非上游是不可控外部系统且有明确证据。

### 2. 上下文全程携带，调查深度服从阶段职责

1. **一次组装、全程传递、不在下游重建。** 上下文在 M3 抽取阶段一次性组装并冻结成快照（`Todo.content` 的 `source`、`capture` 和 `annotation`），固化到 `Task.source_payload`。执行者默认读触发原文与现场摘要，其余材料按需读取；完整原文不能丢。下游可以补充新事实，不能重新查库拼一份"看起来等价"的背景替代它。
2. **能推断、能查到的，原则上不要问用户，但查询必须服务当前阶段目标。** M3 只补决定准入所缺的证据，证据足够立即停止；M5 为完成 Task 主动组合 `jarvis-tools`、`lark-cli`、`bytedcli`、`git` 和 Skills 深入调查。不能用"自己查"要求 M3 提前完成 M5 的工作。
3. **关键归属按阶段处理。** M3 在群绑定、原文或短查询足以确认时记录项目归属和仓库提示；归属仍不确定但线索值得处理时，保留不确定性并交给 M5，不为填满 payload 展开长调查。M5 再根据 `repo_ref`、项目 `repos` 和实时工具结果确认执行位置。
4. **信息不足时不要在线索层设人工闸门。** M3 可以依据现有证据准入、观察或放弃；实在只有 principal 能回答的问题，由 M5 带着已有调查结果在 Task 上询问。回退问用户是 M5 的最后手段，不是 M3 的默认补全方式。

`scripts/jarvis-tools` 的只读和写入能力应随真实需要扩充，输出 JSON 供稳定解析。能力和通用入口写入 `internal/toolcatalog`，具体领域的操作步骤与命令写入 Skill，不把工具手册复制到各阶段规则。

### 3. 流水线通用：不为特定来源开专用链路

M2 → M3 → M5 是一条通用流水线，每段只有一套协议。M3 用 `extracted` / `observing` 分流；`extracted` Todo 机械固化成 Task 后直接交给 M5 执行，全部语义判断权都在执行 Agent。来源的差异（群消息、单聊、会议妙记、邮件、日程、外部系统……）**只能通过提示词、Skill 和工作规则表达，不能在 Go 里开专用链路**。

- **M2 只做机械采集。** 把原始事实（成功的原始返回、失败的原始错误全文）写成中立证据。不分类、不下结论、不判断"要不要重试"、不按固定长度截断内容。
- **判断归模型。** "这场会有没有开录制""妙记是还没生成好还是真的没权限""该不该再等一会儿"都是结合上下文的语义判断，写进提示词，不写成 Go 的 `if`、常量或字符串匹配。
- **等待与重试归 M5。** 用 `jarvis-tools yield-until` 挂起当前 Task，由模型决定等多久、等几次、何时放弃；不在采集层写 `retryDelay` 常量和重试上限。
- **不为某个来源新增状态枚举、专用表、专用分支，也不为它开校验豁免。**

反面例子（均已在本仓库发生过，勿重犯）：

- 用 `strings.Contains(err, "permission")` 判断错误语义——把"没录制 / 还没生成好 / 真的没权限"压成一种，导致 M3 产出假线索。
- 用 `waiting_minutes` / `permission_denied` 这类枚举，把"为什么还没拿到"固化进 Go。
- 用 `if 产物 A 为空才去取产物 B` 替模型决定该取哪些产物。
- 采集期按固定字符数截断证据：原文已经落盘，却只把残缺副本交给下游。
- 在 M3 校验里按来源开豁免分支（如"证据全部来自会议时跳过 assigner 校验"）。

**接入新来源的正确做法**：走唯一通用入口 `POST /api/clues` / `jarvis-tools append-clue`。写一个定时任务 + Skill，让 agent 自己去外部世界取事实、原样投递回来；判断要点写进提示词或 Skill。M2 收下后只做三件机械动作：存进 `message`、按 `(source, external_id)` 幂等、唤醒 M3。**新来源 = 一个新** `source` **值 + 一份 Skill，不等于一个新 Go 模块。**

### 4. Agent-first：只存骨架，语义交给模型

设计存储、接口、状态和流程时先问一句：**这件事必须由程序保证，还是模型能结合上下文判断？** 只有必须由程序保证的，才做成固定字段和约束。

- **默认宽松。** 凡是进入或来自大模型的提示词、上下文、目标、计划、判断、原因、证据、过程和结果，一律用完整自然语言或宽松 JSON（`TEXT` / `json.RawMessage`）。不为它们复制一套 Go DTO，不加枚举、`additionalProperties: false`、必填键、固定嵌套层级、原因码或类型分支。新增语义应能直接扩展 JSON，而不要求同步改全链路结构体。
- **字段要薄。** 表里通常只保存 ID、关联关系、时间、版本、少量控制状态，以及幂等、调度等必要字段。
- **结构化只服务程序硬消费。** 只有字段确实被程序用于 SQL 查询/索引、关联、幂等、调度、审批载体、状态流转或执行参数时，才允许建固定字段或严格 schema——必须能指出具体消费代码。说不出消费点，就保持宽松。
- **最小投影，不丢原文。** 需要某个控制值时只投影那一个字段，原始内容完整保留并继续交给模型；不用投影字段替代、删减或重写原始上下文。
- **严格协议只包硬边界。** 工具调用参数、执行状态、审批动作、幂等键这类机器协议可以严格校验；协议里的目标、背景、理由、计划正文和执行结果仍保留为宽松文本或 JSON。
- **阶段之间只共享最小控制字段 + 完整原始语义 JSON。** 下游不依赖上游的内部 DTO、枚举或字段清单。阶段内部可以有局部类型，但不要提升成全链路公共大结构；交接优先用"稳定小外壳 + 宽松 payload"。
- **新增强结构前必须先论证**：它解决什么真实的不稳定问题、程序消费点在哪、为什么让模型推断不够。三条说不齐就不要加。

例如设计 Goal，先只保留 `objective`、`status`、关联 ID 和版本；当前计划、子目标、调整原因和进度由模型写在一段可更新的语义内容里，不一开始就建复杂表结构和状态机。等发现模型确实处理不稳定，再加字段。不要为想象中的问题提前设计复杂系统，也不要为错误抽象加兼容层或 fallback。

### 5. M5 执行期：修改权与审批判断都归模型

**M5 是灵活的决策器，不是计划的执行机器。** 它在执行期持有对任务内容的修改权，并自己判断哪些动作需要先请示委托人。代码提供载体和留痕，不替它做判断。

**修改权。** `Task.source_payload` 统一保存来源原始语义、模型说明与创建时冻结的事实；这些都是证据，不是 M5 必须照做的执行合同，也不在下游改写。M5 发现情况变化时直接调整当前目标、范围和动作，不必退回上游；调整依据和结果写进 run output、`progress_summary` 与 `task_event`。**留痕是记录事实，不是干涉判断，不得以"审批归模型"为由省掉。**

**请示判断。** 要不要先问 principal，由模型结合上下文判断，判断依据只写在 `conf/prompts/m5-approval-policy.md`。代码不枚举、不分流、不拦截：

- **不按** `action_type` **决定走不走审批**——风险在动作的具体内容里，不在类型名里。改代码可能只是改个注释，也可能是删库；发消息可能是回个"收到"，也可能是对外承诺。
- 不用哈希比对、字段变更检测来撤销或触发审批。
- **不把请示做成固定流程阶段**（"先 propose 再 apply"）。M5 可以在任何时刻直接完成无需询问的动作，也可以返回 `needs_human + question` 停下来询问。
- 代码只提供载体：`needs_human` 状态、一份可回答的 `question`、一个回答入口，以及事件流和 `effects` 记录。回答原样交回提问的同一个 Session，由模型理解；没有独立批准/驳回接口。

**代码不判断风险，但必须记录后果。** 对外产生的副作用一律原样写进 `ExecutionRun.effects`，不校验、不重写、不拒绝未知类型。

**对外人员触达身份。** 所有发给具体人员或群聊的外部消息，包括普通沟通、任务提醒、OKR/周报催填、AI 填写建议、结果通知和批量广播，统一使用“Jarvis通知机器人”作为发送身份。实现和 Skill 必须遵守以下边界：

- 收件人的业务真源是完整企业邮箱；可保存 `union_id` 辅助关联，但不能把一个飞书应用解析出的 `open_id` 交给另一个应用发送。
- 使用 Jarvis 通知应用时，优先按企业邮箱投递；只有 `open_id` 已由同一 Jarvis 通知应用解析并能证明 namespace 一致时，才允许按 `open_id` 投递。
- 禁止使用 Emily Bot、OKR 网页登录应用、目录查询应用或 principal 的 user identity 发送这类消息；发送失败必须原样暴露，不能 fallback 成用户代发或其它 Bot。
- 发送必须使用稳定幂等键，成功后以同一 Bot 回读并核对 `message_id`、目标会话和正文，再把真实外部副作用写入 `ExecutionRun.effects`。

反面例子：

- `if task.ActionType == "code_change" { 跳过审批 }`——用类型标签替模型判断风险，本质是"为特定动作类型开专用链路"，和 §3 是同一个错误。
- 按 `action_type` 把执行拆成两条流程路径（一条直通、一条必须两阶段）。
- 因为模型调整了执行方向就强制进入 `needs_human`。



### 6. MVP 优先，按整体复杂度选方案

- 早期一切以**跑通 MVP** 为先，选满足当前需求且系统概念最少的实现，不提前抽象、不为想象中的未来需求预留结构。
- **简单不等于少改文件、少写 diff。** 简单按最终系统中的状态、分支、协议、真源、例外和认知负担衡量。把一条规则从错误文件迁到正确文件，即使改两个文件，也比在下游增加反向规则更简单。
- **少写代码不等于绕开根因。** 优先删除无效概念、迁移错误归属、复用现有链路；不能为了少动一个上游文件而新增过滤器、fallback、兼容层或特殊状态。
- 改动范围以完成一个自洽修复所需的最小**语义闭包**为准：真源、组装点、消费者、文档和必要测试应一起一致；与根因无关的重构不顺手做。
- 单用户、本地、低频——避免分布式、缓存、插件化、复杂并发这类与当前规模不匹配的方案。
- 当前 MVP 不建设 Agent、stage 或 Task 之间的访问控制。除非 principal 明确改变信任模型，否则不要把内部 API 可调用、跨阶段使用工具、跨 Task 读写或 actor 可由调用方提供列为缺陷，也不要提出 token、RBAC、capability 或工具白名单方案。
- 某个需求的合理实现**确实比较复杂**（架构改动、多模块联动、明显的性能/一致性权衡）时，**先停下来和用户确认**，讲清复杂点和更简单的替代方案，由用户决定。
- **不用数据库事务。** 多步写入按顺序直接写，中途出错就 fail-fast 报错退出、下次重跑，不追求跨步骤原子一致性。某处确有强一致需求，先和用户确认。



### 7. 简单文本配置使用本地 Markdown

系统提示词、工作规则、模板和策略正文等"通过稳定 key 读写一段文本"的配置使用仓库内 Markdown。**文件是唯一真源，不写入数据库，也不在代码里复制正文当 fallback。** 文件放在哪个目录由 §1 的语义所有权决定，不把所有文本都塞进 prompt。

新增系统提示词或策略正文的标准步骤：

1. 在 `internal/textstore/defaults.go` 注册稳定 key、展示名和文件名（只允许访问注册文件，不接受任意路径）。
2. 在 `conf/prompts/` 提交对应 Markdown 正文。
3. 运行时通过注入的 `textstore.Reader.Content(ctx, key)` 实时读取；文件缺失或正文为空直接报错，不加静默 fallback。
4. 需要后台编辑就调通用 `/api/text-files`（list/get/update），前端固定该 key。系统依赖文件不允许从后台创建或删除。
5. 测试至少覆盖读写、未知 key 拒绝、缺失/空正文 fail-fast。

工作规则使用 `conf/rules/` 和 `internal/workrule` 的固定注册与阶段组合机制，不经 `textstore` 冒充系统提示词。修改前必须检查组合逻辑，确认 `all.md` 和阶段文件的实际受众。

提示词文件只描述 Agent 的角色、目标、停止条件和稳定行为。工具说明由 `internal/toolcatalog` 和 Skills 维护；阶段标识、上下文、问题卡和 JSON Schema 由运行时代码动态组装。

**什么时候才建表**：数据确实需要结构化字段、关联关系、索引查询、独立生命周期或硬状态约束时。

### 8. 其它既有约定

- **fail-fast**：暴露问题而非掩盖，尤其单测；不乱加兜底 fallback。
- 展示列表优先行内编辑，见 [.cursor/rules/list-inline-edit.mdc](.cursor/rules/list-inline-edit.mdc)。
- 写组件/代码前优先复用已有官方包和仓库内已有实现。
- 让大模型填写的语义字段尽量使用自然语言或宽松 JSON，不用枚举限制模型发挥；Jarvis 的目标不是通用 Agent 平台。
- 构建或重启主服务必须执行 `./scripts/jarvis-deploy --skip-pull`；禁止裸 `go build` 覆盖 `bin/jarvis-server` 后直接 `launchctl kickstart`，否则会破坏 macOS TCC 稳定签名。
- 重要架构图、绘制说明、可编辑源文件、最终导出和文章写作统一放在 `docs/summery/`；该目录只用于需要进入 Git 的正式表达与交付，架构事实真源仍是 `goal.md`、`docs/00-overview.md` 和当前代码。
- Agent 调查和交付过程中的原始 API 返回、证据快照、下载材料、生成草稿、创建/更新回读、通知 payload 与回执等临时运行产物，建议写入用户目录 `~/tmp/jarvis/<skill-or-task>/`，不进入 Git。模块已有明确持久化真源时继续写入模块目录，例如 OKR 产品数据仍写入并提交 `data/okr/`；用户指定路径和需要提交的正式交付也遵循各自权威目录。需要长期保留的临时结论应提炼后再进入 `docs/summery/`，不得直接提交整份运行工作区。
