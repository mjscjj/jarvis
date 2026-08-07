# 后台配置文件化设计

> Status: current
> Authority: normative design
> Last verified: 2026-08-02 @ `89fa24b`

## 目标

把后台配置从数据库迁到本地文件。运行时代码和管理后台读写同一份文件，文件是唯一真源，不保留数据库或代码常量兜底。

## 文件与职责

目录固定为配置文件同级的 `prompts/`。当前 `conf/config.yaml` 对应：

| key | 文件 | 用途 |
| --- | --- | --- |
| `m3_system_prompt` | `conf/prompts/m3-system-prompt.md` | M3 抽取系统提示词 |
| `m5_system_prompt` | `conf/prompts/m5-system-prompt.md` | M5 执行环节系统提示词 |
| `m5_approval_policy` | `conf/prompts/m5-approval-policy.md` | 执行期"要不要先请示 principal"的判定策略 |
| `fact_extract_system_prompt` | `conf/prompts/fact-extract-system-prompt.md` | 持续世界建模系统提示词 |

M3、M5 的系统提示词文件是稳定指令模板：M3 必须且只能包含一个 `{{WORK_RULES}}`；M5 必须且只能包含一个 `{{WORK_RULES}}` 和一个 `{{APPROVAL_POLICY}}`。运行时与后台生效预览共用同一个严格渲染器。未知、缺失或重复占位符直接拒绝启动或保存，不在代码末尾兜底追加规则。

审批策略只回答“下一项具体副作用是否需要先审批”，包括代码修改。批准后的 proposal 必须原样落地、不得重复副作用等协议属于执行状态机的硬约束，保留在代码中，不能由后台关闭。

## 通用能力

`internal/textstore.Service` 维护固定 key 到固定文件名的白名单，提供：

- `List`：读取全部受控文件。
- `Get` / `Content`：按 key 实时读取。
- `Update`：按 key 原子覆盖正文。

管理接口统一为：

- `GET /api/text-files`
- `GET /api/text-files/:text_file_key`
- `PUT /api/text-files/:text_file_key`

接口不支持任意路径、创建和删除。系统依赖的文件缺失或正文为空时，服务启动或运行直接失败；后台保存使用同目录临时文件加原子重命名，避免读到半份内容。

## 运行与迁移

1. 先把当前有效的 M3、M5 执行内容写入 Markdown，并新增审批策略文件。
2. M3、M5 执行和后台全部切换到文件服务。
3. 删除 `TextStorage` 数据模型、种子逻辑和旧 CRUD。
4. 新版本启动并验证文件读写后，删除 `text_storage` 表。

历史软删除数据不迁移；用户已明确要求删除旧数据。

## 配置页全面文件化

配置页不再把配置正文或扫描缓存写入数据库：

- 工作规则：`conf/rules/m3.md` 与 `m5.md`。M3、M5 运行时只读取各自阶段文件；其它 Agent 不复用这两份规则。
- Skills：正文继续来自 `.agents/skills/*/SKILL.md`，启用状态和阶段范围来自 `conf/skills.yaml`。
- 共享记忆：`data/shared-memory.md`，该目录被 Git 忽略，允许保存本机凭据和长期记忆。
- 运行配置：继续使用现有 `conf/config.runtime.yaml`。

文件服务统一采用固定路径、实时读取和原子覆盖，不接受任意路径。对应的 `shared_memory`、`work_rule`、`agent_skill` 表在文件验证通过后删除。

管理后台将 M3/M5 配置放在最外层「Agent 设置」页面。可编辑模板保留占位符；只读生效预览展开工作规则和审批规则，并明确列出仍由运行时注入的 phase、工具、Skills、上下文和输出协议，不能把预览冒充某次真实运行的完整 Prompt。
