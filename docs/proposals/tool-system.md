# Shell 工具系统优化方案

> Status: implemented-history
> Authority: implementation record; current contracts live in [CLI reference](../reference/cli.md)
> Last verified: 2026-09-13

## 目标与边界

保留 Bash `jarvis-tools` 和既有命令名，不重写 Go CLI，不引入 MCP、工具数据库、权限系统或通用 Agent 框架。减少默认提示词、补全按需帮助，CLI 只承担参数与传输，查询及状态约束由现有 HTTP API / Service 持有。源码与桌面运行采用同一契约。

## 当前证据

- `scripts/jarvis-tools` 有 72 个命令、2738 行；顶层帮助平铺全部命令，尚无分组发现。
- `internal/toolcatalog/catalog.go` 的公共说明约 2740 字符，混入参数、通知文案和交办流程。
- 部分创建/更新帮助只要求 JSON 对象，不说明服务端必需字段。
- 项目、人物、关键事项与资源部分搜索在 Shell 全量分页后过滤；项目过滤仍读取不存在的 description，遗漏实际 summary。
- CLI 从基线 YAML 猜 API 地址，不理解 runtime overlay 或 -addr；CC Connect 是独立进程，不能仅修 Jarvis 子进程环境。
- 可执行 scripts 与可编辑配置共用“保留本地修改”的桌面升级策略。
- 发送消息 Skill 依赖 go run 和源码，桌面包只有编译产物。

## 最终所有权

| 内容 | 所有者 |
|---|---|
| 角色、目标、调查深度、停止条件 | 各阶段系统 Prompt |
| 阶段行为、通知尺度 | 各阶段 rules；审批只归 M5 approval policy |
| 能力入口和发现方式 | internal/toolcatalog |
| 命令用途、参数、最小 payload、返回与错误 | 对应 Shell 领域模块的 --help |
| 参数解析、HTTP、必要摘要投影 | Shell CLI |
| 搜索、分页、引用、状态、CAS、幂等 | HTTP / 领域 Service |
| 跨工具领域操作 | Skill 与按需 references |
| 地址和业务时区 | 运行实例的有效配置与进程环境 |

## 批次一：帮助与提示词

- 增加 `help world|evidence|task|schedule|memory|skill|notify` 与 `help all`，顶层只展示组。
- 现有扁平命令名保持不变。知道命令时可直接使用，不强迫每次走完发现链路。
- 公共工具目录目标约 600–800 字符，不机械截断，不再展开命令参数或阶段特判。
- 单命令帮助包含用途、输入、最小有效 payload、输出、容易混淆的对象/ID、错误与读回方式。
- 通知写作与打扰尺度留在 M5 rules；交办语义留在 Skill；Chat 触发留在 Chat Prompt。
- 验证完整有效输入：M3、M5 首次及恢复、Chat、FactEngine、proactive、会议巡扫与晨报。保留 M5 恢复时刷新当前规则的行为。

## 批次二：Shell 模块化

- `scripts/jarvis-tools` 只定位自身并加载 `scripts/lib/jarvis-tools/` 的领域模块。
- 共用参数/HTTP/输入/错误函数；薄命令索引只含名称、组、简介与 handler，不复制领域 schema。
- 帮助、参数声明与命令实现按 world/evidence/task/schedule/memory/skill/notify 归属，兼容 macOS Bash 3.2。
- 合并 api_get/api_write/update-page 的传输处理，保留 204、409 原始正文、stderr 和非零退出；不自动重试写请求。
- 继续保留 `json-api-data.mjs` 的数字字面量无损提取。冻结证据与宽松 JSON 不经有损重编码，不固定截断。
- 保留符号链接和任意 cwd 调用。帮助无需服务在线，不因缺少 curl/jq 无法查看。

## 批次三：查询与运行一致性

- Project 支持 keyword/code，Person 支持 keyword/role/open_id，KeyMatter 支持 keyword/开放关闭范围，Resource 支持 keyword/person_open_id；均由现有 Service 过滤、分页。
- Group 支持明确 chat_id 过滤；CLI 不通过模糊搜索猜唯一对象。
- 领域列表提供显式 page/limit，默认一页摘要，删除 Shell fetch_all_items。
- 客户端保留参数格式、互斥和 JSON 外形校验，服务端保留最终领域校验。
- Jarvis 子进程、桌面 supervisor / CC Connect 使用实际实例 API 地址和业务时区；人工 CLI 默认读取所属工作区有效配置，可用 --api-base 或环境覆盖。不增加端口探测或连接失败后改连其它实例。
- 保留 --config，统一通过现有 show-connection 合并配置：源码独立调用使用 go run，桌面使用已有二进制；有完整连接环境时跳过解析。不新增构建或启动流程。源码 CC bind/validate 透传 --addr，同一连接用于 Agent 和回调。
- 通过最小有效身份 HTTP 查询替代发送消息 Skill 的源码配置读取，不输出 secret 或完整配置。
- 工具程序、模块和 JSON helper 随 release 同步；用户可编辑 prompts/rules/Skills 保留原策略。更新打包检查。

## 批次四：有证据才减负

- Skill enabled 表达为“自动加入阶段目录”，不建立读取权限。字段改名不作为本轮前置。
- 交办 inline Skill 保留触发条件与职责边界，长操作步骤下沉到按需 references；只在读取链路完整可用时拆分。
- model_api 仍是受支持的配置选项，不能假定无人使用而删除。只隔离 Codex 不需要的工具箱构造；删除引擎须另行确认真实部署。
- 重复 Agent runner 的共享机制另行评估，不把 Chat 流式输出和 M5 Session 恢复合并进万能 Runner。本轮不为统一外观重写 runner。

## 验证

1. 全命令帮助可发现、可离线读取；写入示例在隔离 HTTP/Service 测试验证。
2. 精简目录不携带参数手册；完整阶段 Prompt 不丢角色和恢复策略。
3. 命令与路由、payload、原始证据、stdout/stderr、分页和符号链接回归。
4. 搜索覆盖当前字段及第二页、精确 ID，列表不偷偷抓取全量。
5. 409 冲突正文、部分发送回执、大整数、小数、未知 JSON 字段不丢失。
6. 最终监听地址覆盖基线与启动参数；CC 和所有 Agent 使用同实例，没有显式连接时读取配置，解析或请求失败即报错。
7. 桌面包无源码、无 Go 工具链仍能使用业务工具；程序与服务同版本。
8. 不对生产状态执行测试写入、不发送真实通知、不重启现有服务。UI 改动须浏览器验证；环境不支持时明确记录。

## 执行状态

- [x] 方案经用户确认并落盘；开始时工作树干净。
- [x] 帮助与提示词：公共目录 657 字符，七个阶段共用；M5 首次与两种恢复保留阶段规则，Chat 专属触发仅留在 Chat Prompt。
- [x] Shell 模块化与传输回归：入口仅负责定位与分发，七个领域模块；保留 72 个命令并新增有效身份查询；73 个单命令帮助均可离线读取。
- [x] 服务端查询及 CLI 分页：覆盖摘要搜索、特殊字符、精确 ID、第二页及 total；列表不做全量拉取。
- [x] 运行连接、有效身份与发布资产：Jarvis 子进程、桌面 CC 和源码绑定使用显式连接；程序资产更新，可编辑文本保留修改。
- [x] Codex 不再构造旧工具箱；model_api 保留。Skill 界面改为目录曝光语义，并覆盖关闭目录后仍可读取正文。
- [x] Linux 验证与 current 文档同步：`go test ./...`、Web 51 项测试、类型检查、独立输出目录生产构建、Shell/Node/Zsh 语法和 diff 格式均通过；Skill UI 使用真实浏览器、全 API 拦截验证开关、正文、编辑取消、失败恢复和空列表。
- [ ] macOS 实机：Bash 3.2、完整 DMG 构建与实际升级尚未运行。打包脚本 13 项通过，2 项 AppleDouble 测试因 Linux 缺少 xattr 无法执行，未绕过。

## 保留与暂缓

- 交办 inline Skill 本轮保留原文：当前正文仍承担必要触发条件及职责边界；未引入新的 reference 读取协议，也未盲目拆分造成运行期材料不可达。
- 不重命名 Skill 配置字段，不删除 model_api，不统一各阶段 Agent runner。
- 当前契约已同步至 [CLI 参考](../reference/cli.md) 和 [总纲](../00-overview.md)。测试未对运行实例发通知或写入业务数据，前端构建输出在隔离目录，未提交、部署或重启服务。

## 生效条件

源码部署需整体更新后使用标准重建脚本重启 Jarvis，使新 Agent 获得连接环境；独立 CC Connect 需重新绑定并重启，重新生成的 Agent env 与 systemd 配置才生效。人工工具默认读取工作区配置；临时 -addr 覆盖仍须为独立工具显式指定 API 地址，为 CC 绑定和校验传入相同 --addr；本轮只修改源码并验证，不执行这些生产操作。

## 地址修复验收（2026-09-13）

已按简化方案完成：共享连接函数复用现有配置解析入口，工具默认读取工作区配置，CC 绑定与校验透传 `--addr`；安装和重建检查移除固定端口。未新增构建流程。`internal/toolcatalog`、`internal/appservice`、`internal/agentenv`、`cmd/jarvis-config` 回归通过，Bash/Zsh 逐文件语法与 diff 检查通过。隔离测试覆盖配置与环境优先级、runtime overlay、符号链接、日期时区、桌面二进制、CC 环境及回调、连接失败不改连、安装预检未知连接和重建前 Task 检查；macOS 使用脚本模拟，尚未做实机部署验收。未部署或重启当前服务。
