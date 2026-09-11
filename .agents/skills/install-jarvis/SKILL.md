---
name: install-jarvis
description: 从完整 Jarvis checkout 完成源码安装：建立安装清单，检查旧实例、依赖、飞书默认身份与 CC Connect 绑定，启动服务并转交世界模型和端到端验收。适用于源码部署和检查源码安装缺项；DMG 使用应用内安装流程，不在桌面后台初始化时执行源码安装。
---

# 安装 Jarvis 整体项目

本 Skill 是从完整仓库 checkout 到最终可用的源码安装流程所有者，也是源码安装的唯一用户入口和 `INSTALL_CHECKLIST.md` 的唯一所有者。世界模型阶段内部调用 `$bootstrap-jarvis-world-model`，保留它作为独立重建世界模型时的复用模块；用户不需要再触发第二个安装 Skill。安装 Agent 负责把同一个 `run_dir` 传入、继续维护清单并完成最终验收。可执行实现都在仓库的 `scripts/` 和 `integrations/cc-connect/`，本 Skill 只根据机器事实编排，不进入 Jarvis M3/M5。

DMG / `JARVIS_DESKTOP=1` 的登录、绑定和服务生命周期由应用内 onboarding 与桌面 supervisor 管理，不执行下列源码安装步骤。登录和服务就绪即可进入应用，世界模型按原后台 Task 继续；失败保留任务与已有结果，不转为 clone 仓库、安装工具链或注册 launchd 服务。桌面世界模型的预检与交付见 `$bootstrap-jarvis-world-model`。

执行顺序：**仓库与安装运行 → 机器事实 → 全部依赖 → `validate-dependencies` → lark-cli 当前默认 App → 飞书能力只读审计 → 本机 identity 与 CC 绑定 → 启动 CC Connect/Jarvis → `validate` → `$bootstrap-jarvis-world-model` → 两个真实端到端验收 → `status`**。依赖门通过前不得启动 CC Connect 或 Jarvis；世界模型不是服务启动前置条件，但没有完成或明确标注未做原因时，整个项目安装不能宣称完整。

先完整读取统一操作说明 [operator-guide.md](references/operator-guide.md)。只有需要诊断机器边界或能力审计时，再读取 [installation-boundaries.md](references/installation-boundaries.md)、[feishu-capability-audit.md](references/feishu-capability-audit.md) 和 [cc-connect-binding.md](references/cc-connect-binding.md)；不要让用户在这些文件之间寻找下一步。

## 0. 建立整体安装运行

用户先取得完整仓库，并在仓库根目录启动能加载 repo-local Skills 的 Agent。Agent 立即记录当前 remote、branch、commit；若 `jq` 尚不存在，先补齐这个启动脚本依赖。先尝试恢复最近一次安装运行：

```bash
./scripts/jarvis-install start --resume-latest
```

返回 `resumed=true` 时先读清单和现有证据继续执行，不创建第二份状态页；确认旧运行不应继续时才显式执行不带 `--resume-latest` 的 `start`。保存返回的 `run_dir` 和 `checklist`。全过程只维护 `run_dir/INSTALL_CHECKLIST.md`：每完成一项立即打勾并附真实读回；未做、阻塞、不适用保持未勾选，并在同一行追加 `原因：未做：...`、`原因：阻塞：...` 或 `原因：不适用：...`。原始安装和世界模型证据可写入同一个 `run_dir/evidence/`。飞书身份以 lark-cli 当前默认身份为准，安装运行不再另选 Jarvis 专用 Profile。

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
./scripts/jarvis-install install-bytedcli
./scripts/jarvis-install install-traex
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install validate-dependencies
```

只有 `validate-dependencies` 返回 `ok=true` 才继续。`install-cc-connect` 从固定 upstream 应用仓库补丁，只构建并验收 binary，不配置或启动 daemon。Qdrant 是依赖服务，可以在这一阶段启动。

如果当前 Agent 既没有官方 `lark-suite`，也没有拆分布局中的 `lark-shared`、`lark-contact`、`lark-drive`、`lark-doc`、`lark-im`，首次安装用官方 lark-cli installer；已有 CLI 低于项目最低版本或 Skills/协议不完整时运行 `lark-cli update --json`，然后重新加载 Agent 能力。两种官方 Skills 布局都可验收。通过 `install-codex` 安装 CC Connect 固定调用的官方 Codex CLI；安装 bytedcli 后读回版本，其 SSO 登录可在 Jarvis Web 登录页完成，不作为服务启动前置条件。配置要求 traex 时让用户完成 SSO，再读回状态。逐项更新清单 B 区。

依赖门还必须确认 lark-cli 支持安装流程使用的卡片回调 dry-run 协议，并确认 CC Connect 固定使用的 `codex` 在 PATH 中且 `codex login status` 已通过。Codex 未登录时运行 `codex login --device-auth`，traex 未登录时运行 `traex login --sso-device`；把链接/验证码原样交给用户，完成后重新读取各自 login status。

## 3. 使用默认飞书身份、审计能力并绑定 CC Connect

加载并遵循拆分布局的 `lark-shared`，或 suite 布局中对应的 shared 章节。直接用 `auth status --json --verify` 读回 lark-cli 当前默认身份的 user open_id、Bot 和 token 状态；未配置时才初始化该默认身份；已有登录也必须检查本次安装所需权限，不能把 token 有效当作权限齐全。不为 Jarvis 再选一个 Profile，所有命令都不传 `--profile`。

飞书授权分成两个不同主体：user OAuth 用 split-flow 取得用户读取能力；Bot/App 的 `im:message:readonly`、机器人能力、`im.message.receive_v1`、`card.action.trigger` 和应用版本发布在飞书开放平台完成。不能用 user OAuth 成功冒充 Bot/App 已配置。按 [统一授权说明](references/feishu-authorization.md) 先运行 `./scripts/jarvis-lark-auth check`；未登录或缺少所需权限时，统一运行 `./scripts/jarvis-lark-auth begin` 发起 user OAuth，覆盖当前内置功能所需权限；把 URL 和二维码展示给用户并结束当前轮，用户确认后在下一轮用该次返回的 `device_code` 执行 `lark-cli auth login --device-code <device_code>`。若已过期就重新发起，不持久化长期复用授权码。

一个飞书 App/Bot 是身份根。Jarvis 直接使用 lark-cli 当前默认 App，CC Connect 绑定该 App，不再为 Jarvis 选择第二个 Bot。

绑定前必须让用户确认其他机器或进程不再消费同一 App/Bot 的 WebSocket，并单独勾选 `install.cc-exclusive-owner`。本机检查不能代替这项人工事实；未确认时保持阻塞，不启动 CC Connect。

完成登录读回后，按 `feishu-capability-audit.md` 做只读能力审计，将原始证据和 `evidence/feishu-capabilities.md` 写入当前 `run_dir`。审计本身只读；发现安装所需 OAuth 权限缺失时，由安装 Agent 返回统一授权步骤一次补齐，再复查。用户未完成授权或应用侧未开放权限时，保留原始错误和未完成项，不能宣称权限已齐全；直属上级、职务、部门路径等高级组织字段缺失只记非阻塞未知项，继续后续安装。企业策略下不加载 `lark-okr`，OKR 只走文档证据。

```bash
./scripts/jarvis-install configure-identity \
  --agent-name <name> --open-id <open_id> --git-author <author>

printf '%s\n' '<App Secret>' | ./scripts/jarvis-install bind-cc
# 已有且已验证 secret 时才可显式复用：
# ./scripts/jarvis-install bind-cc --reuse-existing-secret
# 已有 allow_from 不是 Principal 本人且用户确认替换时：
# printf '%s\n' '<App Secret>' | ./scripts/jarvis-install bind-cc --replace-allow-from

./scripts/jarvis-install validate-binding
```

App Secret 从飞书开放平台「凭证与基础信息」复制；`bind-cc` 会立即用这组 App ID/Secret 换取 tenant token 验证，错误凭证不得拖到最终端到端才暴露。已有 `allow_from` 不同于 Principal 时命令会停止，只有用户明确同意后才加 `--replace-allow-from`。

绑定校验还必须确认 CC 托管的 Agent 每轮先运行 `scripts/jarvis-tools get-context`，否则 CC 只是进入仓库的普通 Codex，不算和 Jarvis 世界模型一体化；并通过 `card.action.trigger` 的 Bot dry-run 验证 App 已申请 `im:message:readonly`、已发布该回调事件。逐项更新清单 C 区；能力审计与默认 App 绑定是两个独立验收项，不能互相代替。另一台机器是否仍消费同一 App 无法由本机证明，必须展示为人工确认项，不能伪造机器通过。

## 4. 启动并验收运行底座

再次运行 doctor，根据真实归属选择动作：

- macOS CC daemon 未注册：`./bin/cc-connect-jarvis daemon install --config "$HOME/.cc-connect/config.toml"`。
- Linux CC daemon 未注册：`./scripts/install-cc-systemd.sh "$HOME/.cc-connect/config.toml"`。
- CC daemon 已属于当前 binary：安全 restart；属于其他 binary/checkout：先取得用户是否替换的决定。
- fresh clone 的 Jarvis 主服务未注册：`./scripts/jarvis-install install-server`，保留完整依赖与绑定门禁。
- 用户已确认复用当前 checkout 和既有数据时，无论 Jarvis 服务仍在运行还是 launchd label 已丢失，都使用 `./scripts/rebuild-server.sh`；label 缺失时脚本直接复用签名安装动作恢复注册，不重新进入完整安装或升级 CC Connect。
- 主服务属于其他 checkout：停止并询问，不自动 bootout。

先启动 CC Connect，再启动 Jarvis。禁止裸 `go build` 覆盖服务 binary。

```bash
./scripts/jarvis-install validate
```

验收覆盖补丁版 CC daemon、9810/9820、Qdrant、Jarvis `/healthz`、`/readyz`、配置权限和默认 lark-cli App 绑定。逐项更新清单 D 区。

## 5. 完成世界模型阶段

把 `run_dir`、`run_dir/evidence/` 和 `evidence/feishu-capabilities.md` 交给 `$bootstrap-jarvis-world-model`。世界模型 Skill 只负责取证、推断、写入和读回人、事、物、群、重点事项；它更新 `INSTALL_CHECKLIST.md` 的 E 区，不接管整张安装清单，也不安装或重启服务，不重复发起权限申请。

世界模型没有固定实体数量下限。确实不做、证据不足或被权限阻塞时，保持对应项未勾选并写清原因；不得把“服务已启动”描述成“整体项目已安装完成”。

## 6. 真实端到端验收并交付

1. 请用户在一个监听群发送一条新消息，随后执行 `./scripts/jarvis-world-model scan --chat-id ...` 走正常 M2 扫描，并用 `query-messages --chat-id ...` 读回，不等待下一次 cron。
2. 请用户通过绑定的 Jarvis Bot 发起一次 CC Connect 对话，确认该 Agent 先读取当前 Jarvis context 后再回复。
3. 先运行一次 `status` 读回当前状态，据此更新“最终结果”和所有未完成原因，再勾选 `e2e.final-status`。勾选后必须第二次运行同一命令，并把第二次结果作为最终状态交付：

```bash
./scripts/jarvis-install status --run-dir <run_dir>
```

最终交付给出 `INSTALL_CHECKLIST.md` 路径、当前 checkout、默认飞书 App/身份、依赖/绑定/服务/世界模型/端到端结果，以及每个未勾选项的原因和下一步。`status.complete=true` 表示全部完成；`status.deliverable=true` 表示所有未完成项都已按统一格式解释，可由用户接受边界后结束。两者都为 false 时不得结束本轮安装。
