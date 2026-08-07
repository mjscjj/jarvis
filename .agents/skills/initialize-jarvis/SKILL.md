---
name: initialize-jarvis
description: 独立执行首次初始化或经用户明确要求重建 Jarvis，不注入 M3/M5。通过 lark-cli 引导用户登录，以近 7 天飞书身份、直属上级、OKR、消息与群聊为证据，生成可审阅草案，再初始化 PrincipalProfile、项目、关键人物、重点事项和群监听。已有业务数据时必须先停下确认，不用于日常增量维护。
---

# 初始化 Jarvis

把初始化当作一次有证据、可审阅、可重跑的 Agent 工作流。Skill 负责编排和语义判断；配置、M1 CRUD、M2 群发现与 checkpoint 仍由各自现有模块负责。

本 Skill 由仓库中的 Codex 独立触发，不进入 Jarvis M3/M5 Skill catalog。初始化专用机器动作只走 Skill 自带的 `scripts/jarvis-init`；`scripts/jarvis-tools` 只承载可复用的常规 M1 查询与 CRUD。

## 不可越过的边界

- 默认只处理全新实例。发现已保存的 PrincipalProfile、Project、Person、KeyMatter、ManagedResource，或已人工标注的群背景时，立即列出存量并询问用户；不要猜测合并、覆盖或删除策略。
- 调查阶段只读飞书。不得发消息、修改 OKR、邀请群成员或创建外部对象。
- 不把初始化拉取的历史消息投递给 `append-clue`，也不改 M2/M3 水位。历史材料只服务本次推断。
- `conf/config.runtime.yaml` 是本机明文配置，权限必须为 `0600`；不得在回复、日志或证据文件里输出 appSecret、accessToken、refreshToken。
- 草案落盘不等于授权写入。必须展示摘要并取得用户明确确认，才可写 M1 或打开群监听。
- 应用阶段逐项写、逐项读回，任何一步失败就停止并保留已成功清单；不做事务、静默 fallback 或自动回滚。

## 0. 读取参考并预检

先读取：

- [ownership-map.md](references/ownership-map.md)：每类数据的唯一所有者与禁止写入点。
- [evidence-sources.md](references/evidence-sources.md)：取证范围、证据边界和授权处理。
- [draft-contract.md](references/draft-contract.md)：`draft.json` 结构与应用顺序。

在仓库根目录运行：

```bash
bash .agents/skills/initialize-jarvis/scripts/preflight.sh
./.agents/skills/install-jarvis/scripts/jarvis-install doctor
```

`preflight.ready` 只表示可以开始只读取证，不表示机器 identity 或服务已经初始化。`jarvis-install doctor` 负责完整机器事实；若 `machine_ready=false`，先按 `$install-jarvis` 修复依赖。`configuration.status.machine_configuration_ready=false` 在 fresh clone 中是正常状态，可继续取证。若本地 `var/jarvis.db` 已存在但服务不可用，也按存量实例处理并先询问用户，不能把“查不到 API”解释为空实例。

若 Jarvis 服务可用，读取 `get-principal`、`list-projects`、`list-persons`、`list-key-matters`、`query-resources` 和已人工标注的群。遇到存量业务数据，执行上述“全新实例”停止条件。群的机械发现记录本身不算业务数据。

为本次运行创建 `var/onboarding/<YYYYMMDD-HHMMSS>/`，下设 `evidence/`，并把该相对目录记作 `run_dir`。不要覆盖旧运行目录。

## 1. 确认并登录 lark-cli 用户身份

加载并遵循当前环境的 `lark-shared` Skill。先列出现有 profiles 和非密钥 app 配置，再决定复用或新建；所有后续命令都显式携带本次 `--profile <profile>`，不要依赖机器默认 profile。

1. 只有一个明确属于本次 Jarvis app 的 profile 时可建议复用；没有 profile 时按 `lark-shared` 引导用户创建 app 配置；多个候选或 app 归属不清时，把差异交给用户选择，不根据名字猜。
2. 运行带 `--verify` 的 `auth status`，记录 user 的 `openId`、`userName`、token 状态和 scope；不记录 token。
3. 如果用户身份未就绪，按 `lark-shared` 的 split-flow 发起最小只读授权。不得使用 `--domain all`。
4. 当前轮只展示原始授权 URL 和二维码，然后结束并等待用户回复已授权。不要在同一轮阻塞轮询，也不要持久化 `device_code` 或授权 URL。
5. 用户回来后重新发起一次 split-flow，亲自用新 `device_code` 完成登录，再次运行 `auth status --verify`。

缺少某个读取权限时，只增量申请错误明确给出的只读 scope。bot scope 缺失要给出开发者后台链接，禁止用用户登录替 bot 补权限。

## 2. 采集最近 7 天证据

加载并遵循 `lark-contact`、`lark-okr`、`lark-im`；只有直属上级字段无法由已封装能力取得时，才加载 `lark-openapi-explorer` 调用通讯录用户详情。

按 [evidence-sources.md](references/evidence-sources.md) 采集，并把每次成功原始 JSON 分文件写入 `evidence/`：

1. 当前登录用户身份与通讯录基本资料。
2. 本机 Git 配置与已确认仓库的近期提交，用于确认唯一的 `git log --author` 模式；无法唯一确认时必须询问用户。
3. 直属上级、部门、职务。直属上级拿不到时保留“未知 + 原始错误/权限边界”，不得猜名字。
4. 当前 OKR 周期、Objective/KR、对齐信息；没有 OKR 是合法事实。
5. 最近 7 个自然日用户参与的群聊和私聊消息，优先用户本人发言、@用户、线程回复和活跃群。
6. 候选群成员与群元数据，仅为判断关键人物、项目归属和监听范围服务。

附件正文默认不下载；只有它对项目或重点事项判断不可替代时才读取，并在草案理由中说明。

## 3. 形成可审阅草案

基于证据生成 `run_dir/draft.json`，严格使用 [draft-contract.md](references/draft-contract.md) 的薄外壳。语义内容保持自然语言，不为了“完整”编造字段。

推断规则：

- Profile 只写稳定身份、职责背景和已明确偏好；短期任务不写入 preferences。
- Project 必须有持续目标或稳定协作边界，不能把一次会议、一个群名或单条消息直接当项目。
- Person 优先直属上级、明确协作 owner、频繁且关键的决策者；不要把所有聊天对象导入。
- KeyMatter 必须是当前未闭环且值得持续看护的事项；“最近讨论过”本身不够。
- Group 只选择与候选项目或关键协作持续相关的群。`related_group` 表示进入 M2 持续监听，`is_key_group` 要更严格。
- 每个候选项都给出 `rationale`、`evidence_refs` 和 `confidence`。证据相互冲突时保留冲突，不强行归一。

向用户展示：身份、本机 Git author、直属上级、项目、关键人物、重点事项、拟监听群，以及未知项和权限缺口。然后明确询问是否按该草案写入，并结束当前轮。用户未明确同意前不得进入下一步。

## 4. 写入并建立监听

获得确认后按下列顺序执行：

1. 用已验证的 user `openId` 和本次 profile 写运行配置：

   ```bash
   ./.agents/skills/initialize-jarvis/scripts/jarvis-init configure --open-id <open_id> --profile <profile> --git-author <author>
   ```

2. 重新运行 `jarvis-install doctor`，根据真实服务状态选择机器动作：
   - `services.qdrant.healthy=false`：停止并回到 `$install-jarvis`，不得启动一个依赖不完整的主服务。
   - 当前 checkout 的主服务已注册：仅用 `./scripts/rebuild-server.sh` 构建并重启。
   - 主服务尚未注册：运行 `./.agents/skills/install-jarvis/scripts/jarvis-install install-server`。
   - launchd label 的 program 指向其他 checkout 或归属不明：停止并取得用户是否替换的明确授权，不自动 bootout。
   禁止裸 `go build` 覆盖服务二进制。
3. 再次检查实例为空；如果重启后出现存量业务数据，停止并报告，不继续合并。
4. 用 `update-principal` 写 Profile，并立即 `get-principal` 读回。
5. 逐个 `create-project`，保存返回的真实 ID；再逐个读回。
6. 逐个 `create-person`；再写 KeyMatter、ManagedResource。把草案中的逻辑引用替换为刚返回的真实 ID。
7. 运行 Skill 自带的 `jarvis-init discover` 触发一次 M2 会话发现，等待候选群出现在 `jarvis-tools list-groups`。M1 不创建群记录。
8. 对每个已发现候选群调用 `jarvis-tools update-group`，写人工背景、项目绑定和监听标记；随后调用 `jarvis-init scan --chat-id ...` 做同步首次扫描，明确暴露 checkpoint 错误。

每个成功动作追加到 `run_dir/applied.ndjson`，包含 target、返回 ID、时间和对应草案 ref；不得写 token。失败时输出最后一个成功动作和原始错误，等待用户决定是否从未完成项继续。

## 5. 验收

先运行系统安装验收：

```bash
./.agents/skills/install-jarvis/scripts/jarvis-install validate
```

它必须确认配置完整且权限为 `0600`、Qdrant 和主服务健康、`/readyz` 没有 error 依赖。然后运行初始化业务验收：

```bash
./.agents/skills/initialize-jarvis/scripts/jarvis-init validate --profile <profile>
```

它必须确认：lark 用户 token 有效、登录 open_id 与 PrincipalProfile 一致、Profile 已保存、至少一个 related 群存在、样本中至少一个群 scan checkpoint 成功；同时报告项目、人物和重点事项数量。

再做语义读回：

- `get-context` 能看到正确 Principal、项目、人物、重点事项和群绑定。
- 每个写入对象都能用对应 `get`/`list` 命令定位，内容与草案一致。
- 直属上级未知、OKR 为空或项目为零时，最终报告必须明确这是证据结论还是权限缺口。

最后请用户在一个新监听群发送一条新的测试消息。等 M2 扫描后用 `query-messages --chat-id ...` 读回该消息。这是端到端监听验收；不要由初始化 Agent 代发消息制造假阳性。

完成后给出 `run_dir`、写入数量、监听群、机器验收结果、端到端消息结果和所有未解决项。
