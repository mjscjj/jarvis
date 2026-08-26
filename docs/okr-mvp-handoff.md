# OKR MVP 验收与交接

> Status: current
> Authority: non-normative verification snapshot; code and routes remain authoritative
> Last verified: 2026-08-27 @ uncommitted worktree

本文给人工复核这批未提交改动使用。实现目标是把 OKR 纳入 Jarvis 世界模型，并保持 `Task` 只表示一次执行单元。当前改动没有提交、推送，也没有调用真实飞书或 Meego 写接口。

## 交付边界

已完成的 MVP 主链路：

- 世界模型层级为 `OKR → Project → KeyMatter`，OKR 关联 Person 负责人；关闭 OKR 不删除或冻结下级 Project、KeyMatter。
- Page 保存实体当前结论，Fact 追加历史变化；Page 更新使用 CAS，冲突时不覆盖远端新内容。
- 「世界 → OKR」页面支持新建、编辑、闭环、查看完整层级、维护当前结论、记录事实，以及查看一周内的变化、风险和失速信号。
- `okr-progress-sync` Skill 只读 Meego 和已采集消息。证据先进入通用 clue/message，再由 Agent 明确选择最小实体后写入 Fact/Page。
- 未关联证据可按来源、日期和稳定锚点收窄；成功关联后退出队列。系统不凭标题相似度自动猜目标。
- `jarvis-tools get-okr-weekly-view` 提供 Agent 可用的周视图回读入口。
- OKR 建模和证据写回不会创建常驻或关联 Task；定时巡检仍复用 ScheduledTask，每次执行只是一项普通 Task。

刻意保留的 MVP 限制：

- 未关联证据队列目前是 Agent CLI/API 能力，不是面向人的新页面。
- Meego 查询锚点、巡检频率和真实方向数据由部署后的受控配置提供；本次没有读取凭证或访问生产数据。
- ScheduledTask 模板已经写入 Skill，但没有替用户在运行实例中创建真实定时任务。
- 不增加自动关联、自动外发、OKR 专用执行流水线或新的 Task 外键。

## 验收证据矩阵

| 要求 | 权威实现 | 自动验收 |
|---|---|---|
| 未关联证据发现 | `internal/background/okr_evidence.go`, `GET /api/okr-evidence/unassociated` | `TestListUnassociatedOKREvidenceFiltersAndExcludesOKRAssociations`、`TestOKRProductPathThroughRealHTTPAndJarvisTools` |
| Agent 选择最小实体并写入 | `POST /api/okr-evidence/apply`, `jarvis-tools apply-okr-evidence` | `TestApplyOKREvidencePreservesFactWhenPageCASConflicts`、真实 CLI 产品路径测试 |
| Fact/Page/周视图回读 | Page/Fact API、`GET /api/okrs/:id/weekly-view`、`get-okr-weekly-view` | `TestOKRHTTPFlowKeepsHierarchyProgressAndTaskBoundary`、真实 CLI 产品路径测试 |
| 关联后退出队列 | Message 来源行与 OKR/Project/KeyMatter Fact 的幂等关联 | 真实 CLI 产品路径测试前后各读取一次队列 |
| 不创建 Task、不调用外部系统 | 世界模型服务不依赖 Task；测试 runner fail-closed | 两个全链路测试断言 `Task` 行数为 0，真实产品路径测试的外部 runner 禁止调用 |
| 页面可用 | `web/src/Background.tsx`, `web/src/api.ts`, `web/src/types.ts` | 前端 typecheck、单测、生产构建；此前浏览器验收覆盖新建/编辑、Page、Fact、层级展开与闭环 |

最新验证命令：

```bash
bash -n scripts/jarvis-tools
go test ./internal/api -run TestOKRProductPathThroughRealHTTPAndJarvisTools -v
go test ./...
npm --prefix web run typecheck
npm --prefix web test -- --run
npm --prefix web run build
git diff --check
```

上述命令在当前未提交工作树全部通过。真实产品路径测试使用临时 SQLite、真实本地 Hertz listener 和真实 `scripts/jarvis-tools`，不会访问外部系统。

## 人工页面验收入口

在已启动的本地 Jarvis 中打开 `http://127.0.0.1:18800/`：

1. 进入「世界 → OKR」，确认卡片保持简洁，只显示周期、目标、状态、负责人、当前结论和层级摘要。
2. 新建一个 OKR，填写目标、周期、自由文本状态和负责人；进入「世界 → 项目」把 Project 关联到该 OKR，再创建或关联 KeyMatter。
3. 回到 OKR 卡片打开「详情」，修改当前结论并记录一条进展 Fact；确认周变化区域同时展示当前 Page 结论与本周 Fact。
4. 刷新页面，确认层级、Page 和 Fact 均可回读；关闭 OKR 后确认 Project 和 KeyMatter 仍保留并可继续维护。
5. 若要复核 Agent 链路，直接运行上面的真实 CLI 产品路径测试；它覆盖证据发现、关联、回读、退队和零 Task 副作用，不需要生产凭证。

## 未提交改动清单

所有改动都停留在工作树中，按职责分组如下：

- 数据与领域：`internal/domain/models.go`、`internal/background/okr*.go`、`internal/background/page.go`、`internal/background/project.go`、`internal/background/reference*.go`、`internal/background/types.go`、`internal/background/views.go`、`internal/progress/*.go`。
- API 与组装：`internal/api/background.go`、`internal/api/pages.go`、`internal/api/router.go`、`cmd/jarvis-server/main.go`。
- Agent 边界：`.agents/skills/okr-progress-sync/SKILL.md`、`conf/skills.yaml`、`conf/prompts/fact-extract-system-prompt.md`、`conf/prompts/proactive-system-prompt.md`、`scripts/jarvis-tools`、`scripts/jarvis-world-model`、`internal/toolcatalog/*.go`。
- 页面：`web/src/Background.tsx`、`web/src/api.ts`、`web/src/types.ts`、`web/src/styles/review-memory.css`。
- 验收与兼容测试：`internal/api/okr_flow_test.go`、`internal/api/okr_product_path_test.go`，以及 background、domain、progress、scheduledtask、skill、factengine、proactive、contextsnap、taskcreate 的相关测试。
- 文档：`docs/design-okr-world-model.md`、`docs/okr-mvp-handoff.md`、`docs/modules/01-background.md`、`docs/reference/http-api.md`、`docs/README.md`。
- 本地状态隔离：`.gitignore` 仅补充 LoopX/工作树生成状态的忽略项。

人工审查时应重点看三个边界：关闭 OKR 后下级实体仍可独立维护；证据关联只能由 Agent 明确选择目标；任何 OKR 更新都不能隐式创建 Task。确认后再决定如何拆分提交。
