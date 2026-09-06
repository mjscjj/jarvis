# OKR 模块当前实现

> Status: current
> Authority: normative
> Last verified: 2026-09-06, working tree based on d917713

本文只维护 OKR 模块当前已经落地的边界和常用修改入口。长期拆分原则、双 Progress 的设计理由及后续阶段见 [通用 OKR 插件与 Agency OKR 拆分设计](../design-okr-plugin-and-agency-okr.md)。字段、路由和运行配置仍以代码与配置文件为最终真源。

## 当前结论

OKR 当前是两个内置 App Module，而不是 `internal/plugin` 采集插件：

```text
okr（通用 OKR）
└── agency-okr（Agency 业务包装，requires: okr）

Jarvis 世界模型
└── 通过原子工具、Skill、EntityRelation 和 WorldProgress 与 OKR 协作
```

- `okr` 拥有 Objective、KR、Metric、Point、结构化负责人、周次、Weekly KR Core 和正式 Progress。
- `agency-okr` 拥有标签、OKR Plan、Preview/Review、周报业务展示、评论、评分、Follow-up、催填、Meego、飞书页面身份和业务 Agent 编排。
- 当前完整 OKR 页面注册为 `Agency OKR`。通用 `okr` 已有独立数据、API 和 Agent 工具边界，但尚无单独的基础页面。
- 两个模块继续复用 `internal/okrworkspace/`、`data/okr/okr.db` 和既有 `okr_workspace_*` 表。本次拆分没有搬库、改表名或复制历史数据。
- `internal/plugin` 仍只负责 Codebase、Meego、Oncall 等外部线索采集插件；OKR 不进入这套采集器运行时。

模块注册和依赖的代码真源是 `internal/appmodule/module.go`，仓库默认开关是 `conf/modules.yaml`。`agency-okr=on, okr=off` 是非法组合，会因依赖缺失而失败；关闭模块不会删除数据。

## 数据所有权

| 语义 | 当前所有者 | 当前存储 |
|---|---|---|
| O、KR、Metric、Point、Owner | `okr` | 既有 OKR 领域表 |
| 周次、周期指标、人工正式进展 | `okr` | `okr_workspace_week`、`okr_workspace_weekly_kr_core`、`okr_workspace_progress` |
| 标签、Plan、评论、评分、Follow-up | `agency-okr` | 既有 Agency 业务表 |
| Meego 快照、催填批次 | `agency-okr` | 既有 Meego / reminder 表 |
| Agency 页面登录和用户授权 | `agency-okr` | Jarvis 运行库 session + 本地 token 文件 |
| OKR 与项目、关键事项等跨模块强关系 | Jarvis 世界模型 | `EntityRelation` |
| Jarvis 基于现实证据形成的独立进展判断 | Jarvis 世界模型 | `WorldProgress` |

正式 OKR Progress 与 `WorldProgress` 必须并存：前者是人在 OKR 中维护、组织认可的正式口径；后者是 Jarvis 根据消息、Meego、Fact、Task 等证据形成的现实判断。当前没有 Go 自动双写，也不能用 `WorldProgress` 自动覆盖人工正式进展。

## 当前读写边界

### 通用 OKR

- API 前缀：`/api/okr/*`。实际注册见 `internal/api/okr_module_routes.go`。
- 原子工具：`scripts/okr-module-tools`。
- 已支持 Objective/KR 维护、Metric/Point/Owner 完整拆解、周次、Weekly KR Core、正式 Progress CRUD、图片上传和乐观版本控制。
- `PUT /api/okr/krs/:kr_id` 与 `okr-module-tools replace-kr` 只接受通用拆解数据，不能夹带 Agency 标签、Meego 或周进展。
- 只启用 `okr` 时，不校验或初始化 Agency SSO、飞书 Secret 和 Preview Review。

### Agency OKR

- API 前缀：`/api/agency-okr/*`。
- 原子工具：`scripts/agency-okr-tools`。
- 页面入口：`web/src/modules/registry.tsx` 注册的 `Agency OKR`，复用当前 `web/src/okr/` 页面实现。
- Agency 组合视图读取通用 OKR 和正式 Progress，再叠加标签、评分、评论与 Meego 信息；它不是第二份 OKR 真源。
- 正式 Progress 的写入仍调用 `/api/okr/*`，写完再回读 Agency 组合视图，防止页面本地状态丢失 Agency 字段。

旧 `/api/weekly-report/*` 和 `scripts/weekly-report-tools` 不再保留。`weekly-report` 仍可能出现在前端页面 surface、分享 URL、Skill 名，以及 ScheduledTask 旧绑定迁移中；这些不再表示模块键。

## Agent 与世界模型

模块能力通过原子工具和 Skill 暴露，不在 Jarvis 核心流水线增加 OKR 专用分支：

| Skill | 门禁模块 | 当前职责 |
|---|---|---|
| `okr-world-projector` | `okr` | 读取稳定 OKR 结构，建立有证据的跨模块关系；不读取周进展 |
| `okr-agent-orchestrator` | `agency-okr` | 根据可编辑业务 Prompt 编排标签、Plan、Review 和报告动作 |
| `weekly-report-progress-sync` | `agency-okr` | 调查 Meego/飞书证据并维护世界模型，不自动提交正式进展 |
| `weekly-report-reminder` | `agency-okr` | 生成催填快照并提醒真实负责人 |

Objective→KR、KR→Metric/Point 和 Owner 等 OKR 内部关系由 OKR 原生结构派生，不复制进 `EntityRelation`。只有 OKR 到 Project、KeyMatter、Resource 等跨模块、有证据的强关系才进入通用关系存储。

## 配置与启动

- 模块开关：`conf/modules.yaml`。
- OKR 数据库、图片、Agency 身份和 Preview Review 配置：`conf/okr-module.yaml`。
- 启动装配、迁移和旧 ScheduledTask 绑定迁移：`cmd/jarvis-server/main.go`。
- 通用迁移集合：`internal/okrworkspace/domain/models.go` 的 `CoreModels()`。
- Agency 迁移集合：同文件的 `AgencyModels()`。
- 完整 UI 需要两个模块都开启；只启用 `okr` 时提供 API、数据和 Agent 能力。

配置修改后按仓库统一方式重启或部署，不自行拼接构建命令。关闭模块只停止路由、页面、Skill 和后台能力，不清理表或历史记录。

## 当前已知边界与后续工作

以下内容尚未完成，不应描述成当前已有能力：

1. `internal/okrworkspace.Service` 和 `web/src/okr/` 仍是共享实现目录，当前解耦发生在迁移集合、API、DTO、工具、Skill 和模块门禁层；尚未物理拆成 `internal/okr`、`internal/agencyokr` 和两套前端目录。
2. 通用 `okr` 尚无独立基础 UI。
3. Agent 尚无完整的“现实证据 + WorldProgress → 正式 Progress 候选 → 人确认 → 回填”产品闭环；当前仅具备所需的独立进展存储和正式 Progress 原子写工具。
4. 统一 World Graph 读取层尚未落地；现有图谱仍从当前世界模型接口和引用关系组装。
5. `KRPoint` 中仍保留 Meego 字段以兼容现有数据，API 已隔离其所有权，但字段尚未迁出通用模型。

这些后续项的设计依据统一维护在拆分设计文档，不在本文展开实施计划。

## 常见修改入口

| 要修改的内容 | 入口 |
|---|---|
| 模块名称、依赖、启停规则 | `internal/appmodule/`, `conf/modules.yaml` |
| 通用/Agency 数据归属与迁移 | `internal/okrworkspace/domain/models.go`, `internal/okrworkspace/migrate.go` |
| 通用/Agency 读取与写入边界 | `internal/okrworkspace/service.go` 及同目录领域文件 |
| API 所有权 | `internal/api/okr_module_routes.go`, `internal/api/okr_workspace.go` |
| Agency 登录身份 | `internal/okrworkspace/auth/`, `internal/chat/feishuidentity.go` |
| 通用/Agency Agent 工具 | `scripts/okr-module-tools`, `scripts/agency-okr-tools`, `internal/toolcatalog/` |
| Agent 语义流程 | `.agents/skills/okr-*`, `.agents/skills/weekly-report-*`, 对应业务 Prompt |
| Agency 页面与 API client | `web/src/okr/`, `web/src/modules/registry.tsx` |
| HTTP 路由速查 | `docs/reference/http-api.md`；最终以注册代码为准 |

修改时先判断语义属于通用 OKR、Agency 业务包装还是 Jarvis 世界模型，再改对应所有者；不要因为当前共用一个 Service 或目录，就把三类语义重新混回去。
