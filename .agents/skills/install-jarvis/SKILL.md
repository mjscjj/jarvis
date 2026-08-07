---
name: install-jarvis
description: 在一台新的 macOS 机器或新的 Jarvis checkout 中，由用户的 Agent 检查依赖、识别已有实例与数据、选择合适的安装方式，并编排 Qdrant、首次身份初始化、主服务注册和端到端验收。适用于“安装 Jarvis”“把仓库给其他人使用”“新机器首次部署”或“检查还缺什么”；不注入 Jarvis M3/M5，也不替用户猜飞书 app/profile、覆盖已有实例或决定包管理器。
---

# 安装 Jarvis

把安装当作 Agent 驱动的开放工作流：Agent 根据本机事实选择下一步；脚本只报告事实或执行边界清楚的原子动作。不要把某一种包管理器、飞书应用或历史数据策略写死成唯一流程。

本 Skill 随完整 Jarvis 仓库分发。用户应先 clone 仓库、在仓库根目录启动 Agent，再要求使用 `$install-jarvis`。它不是可脱离仓库单独安装的通用 Skill，也不进入 Jarvis 的 M3/M5 Skill catalog。

## 先读边界

读取 [installation-boundaries.md](references/installation-boundaries.md)。如果当前 Agent 环境没有 `lark-shared`、`lark-contact`、`lark-okr` 和 `lark-im`，先安装官方 lark-cli Skill 包并重新加载 Agent 能力；不要用临时拼接的 API 调用冒充完整初始化。

## 1. 取得机器事实

在仓库根目录运行：

```bash
./.agents/skills/install-jarvis/scripts/jarvis-install doctor
```

读取 JSON 后再决定动作：

- `machine_ready=false`：逐项处理 `toolchain.missing`、版本、CGO、Xcode Command Line Tools 或 Agent CLI 登录。根据机器已有条件选择 Homebrew、官方安装器、npm 或其他方式；执行任何系统级安装前说明将改变什么。
- `agent_capabilities.lark_skill_pack_ready=false` 只表示脚本没有在常见目录发现 Skill，不足以否定当前 Agent 已加载的插件能力。由 Agent 对照自己的可用 Skills 判断；确实缺少时再安装。
- `configuration.status.machine_configuration_ready=false` 是 fresh clone 的正常事实，不是 doctor 失败。它表示还要由 `$initialize-jarvis` 确认个人身份、profile 和 Git author。
- `repo.database_exists=true`、已有业务对象、`services.*.launchd_loaded=true` 且 program 指向其他 checkout，都是停止条件。先向用户展示现状并询问是复用、迁移还是替换；不要自动接管。
- `warnings` 必须展示给用户，但不得输出密钥值。仓库基线可能带共享的明文模型密钥；提醒使用者自行决定是否替换。

## 2. 安装运行 CLI

缺少 lark-cli 或官方 Lark Agent Skills 时运行：

```bash
./.agents/skills/install-jarvis/scripts/jarvis-install install-lark-cli
```

该动作调用官方 `npx @larksuite/cli@latest install`，同时安装/更新 CLI 与 Agent Skills；已有两者时只验证、不更新。完成后重新加载 Agent 能力，并以 `lark-cli --version` 和可用 Skills 为准。

若 `configuration.status.runtime_binaries` 包含 `traex` 且本机缺少它，运行：

```bash
./.agents/skills/install-jarvis/scripts/jarvis-install install-traex
```

该动作使用 TRAE CLI 自带 updater 指向的 Code 内网 stable 安装器，并返回版本与 `login_ready`，不替用户登录。若未登录，由 Agent 根据终端形态选择 `traex login --sso` 或 `traex login --sso-device`，让用户完成 SSO，再用 `traex login status` 读回。安装器要求使用者能访问公司 Code；访问失败就报告权限问题，不伪造公网 fallback。

重新运行 doctor，要求 CLI 路径、版本、Lark Skills 和 `agent_capabilities.traex.login_ready` 均符合当前配置；不凭安装命令成功推断运行能力已经就绪。

## 3. 选择飞书身份，不猜应用

加载并遵循 `lark-shared`。先列出现有 profiles 并读取非密钥配置：

- 只有一个明确属于本次 Jarvis app 的 profile 时可建议复用。
- 没有 profile 时，引导用户完成 `lark-cli config init` 一类的 app 配置，再做 user auth。
- 有多个 app/profile、app 权限政策不清或 bot 归属不明时，把候选和差异交给用户确认。

所有后续飞书命令显式使用选定 profile。只申请初始化实际缺少的只读权限，不用 `--domain all`，不打印 token/appSecret。

## 4. 准备本机服务

Qdrant 未健康且没有待确认的其他实例时，运行：

```bash
./.agents/skills/install-jarvis/scripts/jarvis-install install-qdrant
```

该动作只支持仓库当前已验证的 macOS arm64 包，并复用仓库固定版本与校验和。其他平台不要强行执行；由 Agent 说明差异并提出适配方案。

然后显式转入 `$initialize-jarvis`。初始化 Skill 负责近 7 天飞书取证、生成草案、让用户审阅、写本机 identity 配置、初始化 M1 人物/项目/重点事项和群监听。安装 Skill 不复制这套语义，也不直接写 M1。

用户确认初始化草案后：

- 主服务尚未注册：初始化 Skill 调用 `jarvis-install install-server`；
- 当前 checkout 的主服务已注册：初始化 Skill 调用 `./scripts/rebuild-server.sh`；
- label 属于其他 checkout：停下取得用户授权，不通过 bootout 偷换实例。

`install-server` 会在配置完整、Qdrant 健康后构建前端、编译并稳定签名后端、收紧配置权限并注册 launchd。不要裸 `go build` 覆盖服务二进制。

## 5. 双层验收

先运行系统验收：

```bash
./.agents/skills/install-jarvis/scripts/jarvis-install validate
```

要求配置完整且权限为 `0600`、Qdrant 健康、Jarvis `/healthz` 正常、`/readyz` 没有 error 依赖。再按 `$initialize-jarvis` 做身份/M1/群 checkpoint 验收，并让用户本人在新监听群发送一条新消息后读回。

最终报告至少包含：仓库路径、lark-cli/traex 版本与登录状态、实际选择的飞书 profile（不含密钥）、服务状态、初始化对象数量、监听群、端到端消息结果、仍缺权限或待用户决定的事项。安装命令成功不等于验收完成。
