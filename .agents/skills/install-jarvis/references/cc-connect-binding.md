# lark-cli、Jarvis 与 CC Connect 一体化绑定

## 所有权

一个飞书 App/Bot 是身份根；选定的 lark-cli Profile 是这台机器访问该 App 的本地入口，同时承载 user OAuth。Jarvis runtime config 和 CC Connect `jarvis-codex` 都投影这个选择，不再独立选择另一个 Bot。

同一飞书 App 的长连接事件不会广播给所有客户端。CC Connect 是该 Bot WebSocket 的唯一所有者；Jarvis 不打开第二条相同 App 的长连接，消息背景通过正常 M2 读取，审批回调由 CC Connect 转发到 Jarvis localhost。

## 安装流程

1. 用 `lark-shared` 创建或选择 Profile，完成 user OAuth，并用 `auth status --json --verify` 确认 user 与 bot。
2. 用登录 user 的 app-scoped open_id 和已确认 Git author 写机器 identity：

   ```bash
   ./scripts/jarvis-install configure-identity \
     --open-id <open_id> --profile <profile> --git-author <author>
   ```

3. 绑定 CC Connect。`bind-cc` 在 `jarvis-codex` 不存在时创建最小项目块；已有项目块时保留 model、reasoning、allow/admin 和回复策略，只更新该 App 的身份。需要复用已有有效 secret 时必须显式指定。

   ```bash
   ./scripts/jarvis-install bind-cc --profile <profile>
   # 或：./scripts/jarvis-install bind-cc --profile <profile> --reuse-existing-secret
   ./scripts/jarvis-install validate-binding --profile <profile>
   ```

4. 绑定的硬字段包括：

   - `projects.agent.type = "codex"`
   - `projects.agent.options.work_dir = <当前 Jarvis checkout>`
   - `append_system_prompt` 要求每个飞书用户 turn 先运行当前 checkout 的 `scripts/jarvis-tools get-context`，并使用选定 Profile 调用 lark-cli
   - Feishu `app_id` 来自选定 Profile
   - `thread_isolation = true`
   - `document_comments = true`
   - localhost approval URL 与 Jarvis runtime config 中同一个 relay secret

   model、reasoning、display、allow/admin 和群回复策略不属于身份绑定，由 Agent 根据使用者和现状决定。

5. `validate-binding ready=true` 后才启动 CC Connect。启动后还要检查 daemon、launchd Program、9810/9820，以及一次真实 Bot 对话；配置校验不等于端到端成功。

如果已有 daemon 指向另一 binary/checkout，或同一个 App 曾部署到其他机器，展示事实并让用户决定是否接管。不要从本机进程推断外部消费者已经停止。
