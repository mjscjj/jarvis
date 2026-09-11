# Jarvis 源码安装操作说明

本文的命令、安装清单和端到端验收用于完整 checkout 的源码安装。DMG 已有应用内安装入口：打开 Jarvis 后补齐登录与连接，服务就绪即可使用，世界模型继续在后台运行并展示真实状态。DMG 用户不需要 clone 仓库或执行本页的依赖、绑定、launchd 安装命令；后台初始化中断时接续原 Task，不从源码安装重来。

用户只需在完整 checkout 根目录告诉 Agent：

```text
使用 $install-jarvis 检查这台机器并完成 Jarvis 首次安装和验收。
```

`$install-jarvis` 是源码安装的唯一用户入口。它内部调用 `$bootstrap-jarvis-world-model`，后者也供桌面后台任务和独立重建复用。

## 会要求用户处理的操作

整次安装最多在以下真实人工边界暂停。Agent 必须一次说明当前步骤、打开位置、要做什么和完成标准，不能只说“去授权”。

| 时机 | 用户操作 | 完成标准 |
|---|---|---|
| 发现旧数据库、旧 checkout 或已有 service | 选择复用、迁移或替换 | 用户明确选择；未选择前不改旧实例 |
| lark-cli user OAuth | 打开 Agent 展示的 URL 或二维码并确认授权 | 原 `device_code` 完成，user 身份有效且 `scripts/jarvis-lark-auth check` 通过；申请范围见 [统一授权说明](feishu-authorization.md) |
| 飞书 App/Bot 配置 | 在开放平台开启机器人，授予 `im:message:readonly`，订阅 `im.message.receive_v1` 与 `card.action.trigger`，发布应用版本 | Bot dry-run 逐项返回 ready；这不能由 user OAuth 代替 |
| App Secret | 从「凭证与基础信息」复制并交给 Agent | `bind-cc` 用 App ID/Secret 成功换取 tenant token；Secret 只落本机明文配置并保持 `0600` |
| Codex 登录 | 打开 `codex login --device-auth` 给出的地址并确认 | `codex login status` 返回已登录 |
| traex SSO | 打开 `traex login --sso-device` 给出的地址并输入验证码 | `traex login status` 返回已登录 |
| 已有 `allow_from` 不是 Principal | 决定是否改为只允许 Principal | 同意后 Agent 才使用 `--replace-allow-from` |
| 同一 App 可能在其他机器消费 WebSocket | 确认旧消费者已停止或决定接管 | 记录人工确认；本机不能伪造这项证明 |
| 最终真实验收 | 在监听群发一条新消息，并给 Bot 发一条自然语言测试 | Jarvis 读回群消息；Bot 回复体现当前世界模型 |

不要要求用户手工启动本可由 Agent 执行的命令、反复重新登录，或在不同文档中寻找下一步。

## 一条恢复路径

每次进入安装先执行：

```bash
./scripts/jarvis-install start --resume-latest
```

- `resumed=true`：读取原清单和证据，从第一个未完成项继续。
- `resumed=false`：这是新安装运行。
- 最新清单已完成，或其创建 commit/模板指纹与当前 checkout 不一致时，命令会 fail-fast；向用户说明事实后新建运行，不迁移旧清单状态。
- 只有用户确认旧运行不再适用时，才执行不带 `--resume-latest` 的 `start` 新建运行。

所有证据和状态都写在返回的 `run_dir`。中途失败不回滚已经验收的步骤；修复原因后重新读回并继续。不要通过重跑整套安装掩盖失败。

## 固定阶段

1. 建立或恢复安装运行。
2. `doctor` 暴露机器、旧实例、配置和服务事实。
3. 安装并验收 lark-cli 与必要 Skills、bytedcli、codex、traex、补丁版 CC Connect、Qdrant。
4. 完成 user OAuth、App/Bot 开放平台配置、Principal identity 与 CC 绑定。
5. 启动 CC Connect 和 Jarvis，验收 binary、service program、9810/9820、`/healthz`、`/readyz`。
6. 内部调用 `$bootstrap-jarvis-world-model`，复用同一 `run_dir`。
7. 完成两条真实端到端验收；第一次读取 status 后填写最终结果并勾选 `e2e.final-status`，再读取第二次 status 作为最终交付。

未完成项必须在清单同一行追加以下一种格式，不能只在聊天中解释：

```text
原因：未做：...
原因：阻塞：...
原因：不适用：...
```

`jarvis-install status` 的 `complete=true` 表示全部完成；`deliverable=true` 表示所有未完成项都有结构化说明，可交给用户决定是否接受当前边界。
