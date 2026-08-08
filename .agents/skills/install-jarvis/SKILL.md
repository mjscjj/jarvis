---
name: install-jarvis
description: 在新的 macOS 机器或 Jarvis checkout 中完成整个项目安装：建立可打勾的安装运行，检查仓库与旧实例，先安装并验收全部依赖，选择并登录一个 lark-cli Profile，把同一个 App/Bot 绑定到 Jarvis 与 CC Connect，启动服务，转交世界模型初始化，并完成真实端到端验收。适用于“安装 Jarvis”“给其他人部署”“新机器首次启动”或“检查整体安装还缺什么”。
---

# 安装 Jarvis 整体项目

本 Skill 是从完整仓库 checkout 到最终可用的安装流程所有者，也是 `INSTALL_CHECKLIST.md` 的唯一所有者。世界模型阶段由 `$bootstrap-jarvis-world-model` 执行，但仍是整体安装的一部分；安装 Agent 负责把同一个 `run_dir` 传入、继续维护清单并完成最终验收。可执行实现都在仓库的 `scripts/` 和 `integrations/cc-connect/`，本 Skill 只根据机器事实编排，不进入 Jarvis M3/M5。

执行顺序：**仓库与安装运行 → 机器事实 → 全部依赖 → `validate-dependencies` → 一个飞书 App/Profile → 飞书能力只读审计 → 本机 identity 与 CC 绑定 → 启动 CC Connect/Jarvis → `validate` → `$bootstrap-jarvis-world-model` → 两个真实端到端验收 → `status`**。依赖门通过前不得启动 CC Connect 或 Jarvis；世界模型不是服务启动前置条件，但没有完成或明确标注未做原因时，整个项目安装不能宣称完整。

先完整读取 [installation-boundaries.md](references/installation-boundaries.md)、[feishu-capability-audit.md](references/feishu-capability-audit.md) 和 [cc-connect-binding.md](references/cc-connect-binding.md)。

## 0. 建立整体安装运行

用户先取得完整仓库，并在仓库根目录启动能加载 repo-local Skills 的 Agent。Agent 立即记录当前 remote、branch、commit；若 `jq` 尚不存在，先补齐这个启动脚本依赖，然后执行：

```bash
./scripts/jarvis-install start
```

保存返回的 `run_dir` 和 `checklist`。全过程只维护 `run_dir/INSTALL_CHECKLIST.md`：每完成一项立即打勾并附真实读回；未做、阻塞、不适用保持未勾选并写原因。原始安装和世界模型证据可写入同一个 `run_dir/evidence/`。选择 Profile 后把清单头部的 Profile 从“待选择”改为实际值。

## 1. 检查机器和旧实例

```bash
./scripts/jarvis-install doctor
```

- 根据 JSON 处理缺失工具、版本、CGO、Agent CLI 登录和现有服务。
- 数据库、CC Connect 或 Jarvis 已存在时，展示归属和业务对象，再让用户决定复用、迁移或替换；不要自动接管。
- 包管理器和具体安装方式由 Agent 根据本机选择。
- 把 checkout、安装运行和旧实例决策写入清单 A 区。

## 2. 安装全部依赖

按 doctor 结果安装工具链，然后使用仓库动作补齐产品依赖：

```bash
./scripts/jarvis-install install-lark-cli
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies
```

只有 `validate-dependencies` 返回 `ok=true` 才继续。`install-cc-connect` 从固定 upstream 应用仓库补丁，只构建并验收 binary，不配置或启动 daemon。Qdrant 是依赖服务，可以在这一阶段启动。

如果当前 Agent 没有 `lark-shared`、`lark-contact`、`lark-drive`、`lark-doc`、`lark-im`，用官方 lark-cli installer 补齐并重新加载 Agent 能力。配置要求 traex 时让用户完成 SSO，再读回状态。逐项更新清单 B 区。

## 3. 选择飞书身份、审计能力并绑定 CC Connect

加载并遵循 `lark-shared`。列出现有 profiles 与 appId；唯一明确候选可以建议复用，多候选交给用户选择，没有则创建。完成 user OAuth 后用 `auth status --json --verify` 读回 user open_id、Bot 和 token 状态。

一个飞书 App/Bot 是身份根；lark-cli Profile 只是本机访问入口。Jarvis 与 CC Connect 必须使用同一 Profile 对应的 App，不得再选择第二个 Bot。

完成登录读回后，按 `feishu-capability-audit.md` 做只读能力审计，将原始证据和 `evidence/feishu-capabilities.md` 写入当前 `run_dir`。安装初始化不运行 `auth login` 补权限，不打开申请流程：核心读取能力缺失就保留原始错误和未完成项；直属上级、职务、部门路径等高级组织字段缺失只记非阻塞未知项，继续后续安装。企业策略下不加载 `lark-okr`，OKR 只走文档证据。

```bash
./scripts/jarvis-install configure-identity \
  --open-id <open_id> --profile <profile> --git-author <author>

./scripts/jarvis-install bind-cc --profile <profile>
# 已有且已验证 secret 时才可显式复用：
# ./scripts/jarvis-install bind-cc --profile <profile> --reuse-existing-secret

./scripts/jarvis-install validate-binding --profile <profile>
```

绑定校验还必须确认 CC 托管的 Agent 每轮先运行 `scripts/jarvis-tools get-context`，否则 CC 只是进入仓库的普通 Codex，不算和 Jarvis 世界模型一体化。逐项更新清单 C 区；能力审计与 App/Profile 绑定是两个独立验收项，不能互相代替。

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

验收覆盖补丁版 CC daemon、9810/9820、Qdrant、Jarvis `/healthz`、`/readyz`、配置权限和同一 App/Profile 绑定。逐项更新清单 D 区。

## 5. 完成世界模型阶段

把 `run_dir`、实际 Profile、`run_dir/evidence/` 和 `evidence/feishu-capabilities.md` 交给 `$bootstrap-jarvis-world-model`。世界模型 Skill 只负责取证、推断、写入和读回人、事、物、群、重点事项；它更新 `INSTALL_CHECKLIST.md` 的 E 区，不接管整张安装清单，也不安装或重启服务，不重复发起权限申请。

世界模型没有固定实体数量下限。确实不做、证据不足或被权限阻塞时，保持对应项未勾选并写清原因；不得把“服务已启动”描述成“整体项目已安装完成”。

## 6. 真实端到端验收并交付

1. 请用户在一个监听群发送一条新消息，等待正常 M2 扫描后用 `query-messages --chat-id ...` 读回。
2. 请用户通过绑定的 Jarvis Bot 发起一次 CC Connect 对话，确认该 Agent 先读取当前 Jarvis context 后再回复。
3. 更新清单 F 区和“最终结果”，然后运行：

```bash
./scripts/jarvis-install status --run-dir <run_dir>
```

最终交付给出 `INSTALL_CHECKLIST.md` 路径、当前 checkout、Profile、依赖/绑定/服务/世界模型/端到端结果，以及每个未勾选项的原因和下一步。只有所有应做项通过，或未做项已经明确展示且用户接受当前边界时，才结束本轮安装。
