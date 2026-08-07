---
name: install-jarvis
description: 在新的 macOS 机器或 Jarvis checkout 中完成运行底座安装：检查机器与旧实例，安装并验收依赖，选择并登录一个 lark-cli Profile，把同一个 App/Bot 绑定到 Jarvis 与 CC Connect，启动服务并做端到端验收。适用于“安装 Jarvis”“给其他人部署”“新机器首次启动”或“检查安装还缺什么”；不推断或写入项目、人物、资料、重点事项等世界模型。
---

# 安装 Jarvis

安装只负责让产品可靠运行。人物、项目、资料、重点事项和监听群由 `$initialize-jarvis` 在服务就绪后处理。所有可执行实现都在仓库的 `scripts/` 和 `integrations/cc-connect/`；本 Skill 只根据机器事实编排，不进入 Jarvis M3/M5。

执行顺序：**机器事实 → 全部依赖 → `validate-dependencies` → 一个飞书 App/Profile → 本机 identity 与 CC 绑定 → 启动 CC Connect/Jarvis → `validate` → `$initialize-jarvis`**。依赖门通过前不得启动 CC Connect 或 Jarvis；世界模型不是服务启动前置条件。

先完整读取 [installation-boundaries.md](references/installation-boundaries.md) 和 [cc-connect-binding.md](references/cc-connect-binding.md)。

## 1. 检查机器和旧实例

```bash
./scripts/jarvis-install doctor
```

- 根据 JSON 处理缺失工具、版本、CGO、Agent CLI 登录和现有服务。
- 数据库、CC Connect 或 Jarvis 已存在时，展示归属和业务对象，再让用户决定复用、迁移或替换；不要自动接管。
- 包管理器和具体安装方式由 Agent 根据本机选择。

## 2. 安装全部依赖

按 doctor 结果安装工具链，然后使用仓库动作补齐产品依赖：

```bash
./scripts/jarvis-install install-lark-cli
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies
```

只有 `validate-dependencies` 返回 `ok=true` 才继续。`install-cc-connect` 调用项目脚本 `scripts/install-cc-connect.sh`，从 `integrations/cc-connect/manifest.sh` 的固定 upstream 应用仓库补丁；它只安装 binary。Qdrant 是依赖服务，可以在这一阶段启动。

如果当前 Agent 没有 `lark-shared`、`lark-contact`、`lark-drive`、`lark-doc`、`lark-im`，先用官方 lark-cli installer 补齐并重新加载 Agent 能力。配置要求 traex 时让用户完成 SSO，再读回状态。

## 3. 选择一个飞书 App/Profile

加载并遵循 `lark-shared`。列出现有 profiles 与 appId；唯一明确候选可以建议复用，多候选交给用户选择，没有则创建。完成 user OAuth 后用 `auth status --json --verify` 读回 user open_id、Bot 和 token 状态。

一旦 Profile 确定就创建本轮状态页，并把已经完成的机器检查和依赖验收补勾上；后面的每一步完成后立即更新，不要等到结尾回忆：

```bash
./scripts/jarvis-init start --profile <profile>
```

这里的语义根是一个飞书 App/Bot；lark-cli Profile 只是本机访问它的入口。Jarvis 与 CC Connect 必须使用同一个 Profile 对应的 App，不再选择第二个 Bot。

确认本机 Git 配置与近期提交能唯一得到 Git author 后写机器 identity：

```bash
./scripts/jarvis-install configure-identity \
  --open-id <open_id> --profile <profile> --git-author <author>
```

然后创建或更新 `jarvis-codex` 绑定。新绑定从终端读取一次该 App 的 secret；已有且已验证的绑定可以显式复用：

```bash
./scripts/jarvis-install bind-cc --profile <profile>
# 或：./scripts/jarvis-install bind-cc --profile <profile> --reuse-existing-secret
./scripts/jarvis-install validate-binding --profile <profile>
```

绑定校验还必须确认 CC 托管的 Agent 每轮先运行 `scripts/jarvis-tools get-context`，否则 CC 只是进入仓库的普通 Codex，不算和 Jarvis 世界模型一体化。

## 4. 启动并验收运行底座

再次运行 doctor，根据真实归属选择动作：

- CC daemon 未注册：`./bin/cc-connect-jarvis daemon install --config "$HOME/.cc-connect/config.toml"`。
- CC daemon 已属于当前 binary：安全 restart；属于其他 binary/checkout：先取得用户是否替换的决定。
- Jarvis 主服务未注册：`./scripts/jarvis-install install-server`。
- Jarvis 已属于当前 checkout：使用 `./scripts/rebuild-server.sh`。
- 主服务属于其他 checkout：停止并询问，不自动 bootout。

先启动 CC Connect，再启动 Jarvis。禁止裸 `go build` 覆盖服务 binary。

```bash
./scripts/jarvis-install validate
```

验收必须覆盖补丁版 CC daemon、9810/9820、Qdrant、Jarvis `/healthz`、`/readyz` 和同一 App/Profile 绑定，不能只看进程存在。

## 5. 转入世界模型初始化

检查 `CHECKLIST.md` 的 A 区：完成项必须是 `[x]` 并附验证结果；未做、阻塞或不适用项保持 `[ ]` 并写原因。把同一个 `run_dir` 交给 `$initialize-jarvis`。安装 Skill 不读取业务证据、不写 M1。
