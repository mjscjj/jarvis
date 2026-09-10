---
name: bootstrap-jarvis-world-model
description: 在 Jarvis、CC Connect 与 lark-cli 已安装绑定并运行后，首次建立或按用户决定重建 Jarvis 世界模型。以飞书身份、本人创建的 OKR 文档、最近 7 天本人撰写或编辑的文档、消息和群聊为证据，推断并写入 Principal、项目、关键人物、资料、重点事项、关系、事实和群监听；源码安装时更新传入清单，DMG 初始化在原后台 Task 记录进展与结果。适用于“建立世界模型”“重建 Jarvis 背景”“补全人事物群”；企业策略下不调用 OKR API、不申请缺失的高级通讯录权限，已有业务数据时先确认合并、补充或重建策略。
---

# 建立 Jarvis 世界模型

初始化只负责“Jarvis 如何理解这个用户的世界”。依赖、lark-cli 默认 App、CC Connect 和主服务在源码安装时归 `$install-jarvis`，DMG 中归应用内 onboarding 与桌面 supervisor；本 Skill 不安装或重启服务，也不配置 CC。它不进入 Jarvis M3/M5 Skill catalog，桌面初始化 Task 通过原始请求中的 `skill_path` 直接加载。

先完整读取 [ownership-map.md](references/ownership-map.md)、[evidence-sources.md](references/evidence-sources.md)、[modeling-guide.md](references/modeling-guide.md)、[worknote-guide.md](references/worknote-guide.md) 和安装 Skill 的 [feishu-capability-audit.md](../install-jarvis/references/feishu-capability-audit.md)。

## 0. 取得工作目录并预检

先按运行环境选择预检与记录位置：

- **DMG / `JARVIS_DESKTOP=1`**：切到原始请求的 `runtime_root`（即 `$JARVIS_RUNTIME_ROOT`），本 Task 的 `run_dir` 用 `var/onboarding/task-<Task ID>/`。只运行 `./scripts/jarvis-world-model preflight` 并读取本机 `/readyz` 的依赖明细，不仅看 HTTP 状态码。桌面运行目录不是源码 checkout；不做 Git 仓库、Go 工具链、launchd 或 `jarvis-install validate` 验收。预检或依赖失败就在原 Task 中如实记录原因，使用现有任务协议等待、询问或报错，不转交源码安装、不重装、不清数据，也不把建模完成作为进入应用的条件。
- **源码安装或源码工作区独立重建**：运行 `./scripts/jarvis-world-model preflight` 和 `./scripts/jarvis-install validate`；服务验收不通过才交还 `$install-jarvis`。由该安装 Skill 调用时复用它的 `run_dir` 与 `INSTALL_CHECKLIST.md`，只更新 E 区；独立重建用 `var/onboarding/<run-id>/`，不创建安装清单。

恢复时先读本 Task 的历史运行与工作稿；原始请求有 `previous_task_id` 时先读前一个 Task 的记录，复用其工作目录和已读回结果，再接续未完成部分，不把失败重试当成全新实例。所有场景都把原始证据写入 `run_dir/evidence/`，工作稿写入 `run_dir/world-model.md`；没有现成的飞书能力证据时按引用的能力审计说明只读补做，不发起权限申请。下文“安装清单”仅指源码安装传入的现有清单；桌面 Task 和独立重建把同类记录留在工作稿与任务结果，不创建清单。

## 1. 先检查存量

读取 `get-principal`、`list-projects`、`list-persons`、`list-key-matters`、`query-resources` 和人工群背景。

- 全新实例：继续。安装程序预建的仅含姓名的本人档案，以及群的机械发现记录，不算需要用户决定的存量世界模型。
- 本次初始化已写入的实体（含第二轮、恢复和 `previous_task_id` 对应的前次尝试）：按工作稿中的真实 ID 读回，继续补充，不再次询问是否重建。
- 其它已有业务数据：先展示存量，请用户选择补充、合并或重建；没有决定前不写。

有安装清单时在 `world-model.existing-data` 记录结论，否则写入工作稿。

## 执行两轮，不增加安装步骤

同一个任务内完成两轮，在 `world-model.md` 记录进度：第一轮按下文读取证据、建立实体并写入长期事实页；第二轮重新读回第一轮结果，对照证据查漏补缺，重点补职责、项目目标、关键人物分工、资料入口、事项当前进展和实体间连接。第二轮复用已取证材料，只为缺口继续查询，不全量重拉、不重复创建、不重复扫描已经成功的群。没有新增结论的对象保持不变，不为凑数量制造实体。

中断后从未完成的轮次继续。

## 2. 读取证据

使用包内 lark-cli 的内嵌说明：先运行 `lark-cli skills read lark-shared`，再按需加载并遵循 `lark-contact`、`lark-drive`、`lark-doc`、`lark-im`（例如 `lark-cli skills read lark-drive`）。引用文件也通过 `lark-cli skills read <skill> <reference路径>` 读取，不依赖新机器用户目录里另装 Skills。具体调用参数以 `lark-cli <domain> --help` 和 `lark-cli schema <service.resource.method>` 为准。

所有飞书读取使用 lark-cli 当前默认身份，需要访问 principal 资源时显式使用 user 身份。默认读取最近 7 个自然日，证据足够后停止；候选仍不确定时可以围绕它扩展关键词、章节、会话或时间范围并记录理由。

至少覆盖：

1. 本人基础身份和当前可见的部门信息；职务、直属上级和完整部门路径属于可选增强，尝试读取但缺失时记录原因并继续，不申请对应高级权限。
2. 本人原始创建的当前 OKR 文档。企业策略不支持 OKR 权限，不加载 `lark-okr`，不得改走 OKR API。
3. 最近 7 天本人创建或参与编辑的文档，保存分页和正文读取覆盖。
4. 最近 7 天必要的本人消息、@、线程回复、活跃群、候选群元数据和成员。
5. 本机 Git 身份及已确认项目仓库的近期提交，只用于确认 Git author 和项目线索。

调查阶段不发消息、不改文档、不申请权限，不把历史消息投递给 `append-clue` 或改变 M2/M3 水位。失败和空结果分开记录。核心读取能力与已有审计不一致时保留新证据，按上述运行环境记录阻塞；可选组织字段缺失不阻塞后续推断。

## 3. 推断世界模型

先识别少量稳定实体，再补重要连接：

- 人：Principal、直属上级、关键 owner、长期协作者。
- 事：持续项目、需要一段时间看护的重点事项。
- 物：后续调查会反复使用的权威文档、仓库和链接。
- 群：持续产生有价值信号的监听面。
- 连接：结构化字段尚未表达的重要关系，以及有真实时间的决策、交付、阻塞和方向变化。

在 `world-model.md` 写自然语言全景、证据引用、置信度和未知项；它是工作稿，不是运行时 schema 或审批合同。高置信且不会覆盖存量的事实可以直接应用。只有高影响歧义、低置信关键归属、存量覆盖或用户才能决定的问题才暂停询问；无需让用户为整份工作稿做 hash 审批。

## 4. 逐项写入并读回

使用现有 `scripts/jarvis-tools`，不创建 onboarding 专用表或 API：

1. `update-principal` 写身份控制位，立即 `get-principal`。
2. Project、Person、KeyMatter、ManagedResource 逐项查询业务键、创建实体、立即读回；拿到真实 ID 后再处理引用。
3. 每个实体的长期事实（它是什么、现在到哪一步）用 `update-page` 单独写入，立即 `get-page` 读回。控制位入口不接受这段内容。
4. `./scripts/jarvis-world-model discover` 触发正常 M2 群发现。`discover` 与逐群 `scan` 是同步操作，脚本允许最长 30 分钟；命令返回前不要把超时当作后台运行。按证据中的 `chat_id` 先 `get-group`，再把返回对象严格投影为 `project_id`、`related_group`、`pinned`、`include_in_memory`、`is_key_group` 五个控制字段，只修改需要的值后调用 `update-group`。不能回写带 `id/chat_id/name/...` 的完整对象，也不能省略未修改的控制字段，否则严格 API 会拒绝未知字段或把缺失布尔值清零：

   ```bash
   group="$(./scripts/jarvis-tools get-group --chat-id '<chat_id>')"
   group_id="$(jq -er '.id' <<<"$group")"
   payload="$(jq -c '. | {
     project_id,
     related_group: true,
     pinned,
     include_in_memory,
     is_key_group
   }' <<<"$group")"
   ./scripts/jarvis-tools update-group --id "$group_id" --payload "$payload"
   ```

   写回后重新 `get-group` 验证五个字段，再用 `update-page` 写群背景，最后执行 `./scripts/jarvis-world-model scan --chat-id ...` 走正常 checkpoint。
5. 结构化字段表达不了的重要关系写在相关实体的长期事实页正文里，用 `[名称](type:id)` 链接目标实体，随后 `list-backlinks` 读回确认引用解析正确；不要制造重复关系记录。
6. 只有真实发生时间的决策、交付、阻塞或方向变化才 `append-fact --source initialization`，随后 `list-facts`。

有安装清单时，每项成功后立刻更新清单 E 区。中途失败就停止，保留已经读回的结果和原始错误；恢复时先查当前世界模型，确认对象不存在再写，不靠事务、回滚或隐藏 fallback。

## 5. 验收与交付

```bash
./scripts/jarvis-world-model validate
```

再按对象逐项语义抽查。脚本只把 `related_only=true` 的人工监听群计入验收，不把机械发现的群当作已建立世界模型。项目、人物、资料、重点事项或监听群都没有固定数量下限；为零时说明这是证据结论、覆盖不足、权限缺口还是尚未决定。

不要把脚本 `ready=true` 当作取证和两轮工作已经完成。区分“成功查询后无匹配结果”“无权限”“分页或正文尚未读完”：第一种可作为结论；核心权限缺失或仍有影响结论的未读材料时保留原因，按现有任务协议等待、询问或报错，不返回 `completed`。可选组织字段缺失或已调查但证据仍不足的归属，可以作为明确未知项交付，不需要凑满。

结束时给用户一份简短、可直接阅读的结果：理解了哪些本人职责、项目、关键人物及其分工、重点事项；资料和群背景写到了哪些实体事实页；还有哪些未知或权限缺口。举出实际名称和写入位置，不只报数量或“初始化完成”。详细证据和读回 ID 留在 `world-model.md`。桌面 M5 任务把这份简短结果写入现有 `progress_summary`（遵守该字段长度限制），应用内后台进度面板直接展示；`summary` 保留本轮执行与核验依据，不增加初始化专用输出 schema。

仅由源码安装调用时，将实体/关系/事实数量、语义抽查结果、覆盖不足和未解决项交还 `$install-jarvis`，由它负责两个真实端到端验收和最终 `jarvis-install status`。桌面初始化在原 Task 交付结果；独立重建直接交付 `world-model.md` 与读回结果。
