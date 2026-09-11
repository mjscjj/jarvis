# 安装时统一申请飞书权限

源码安装与桌面 onboarding 共用 `scripts/jarvis-lark-auth`。脚本是安装所需 OAuth scope 的唯一清单；申请和检查使用同一组 scope，不能在 Skill 或 Go 中再复制一份，也不能改回依赖 CLI 推荐集合。

当前内置能力包括消息与群、世界模型取证、报告文档及内嵌资源、日历、会前准备、会后回顾（视频会议、智能纪要、妙记）、晨报、个人日报/周报和待办。这些功能仍可手动调用，关闭定时巡扫不代表移除功能。新增或移除内置能力时，同步维护脚本中的对应权限与只读验收；不要为尚未启用的外部插件申请权限。

Codebase、Meego 等可选外部插件继续通过各自 provider 授权；Oncall 和我的交办依赖的飞书消息能力已包含在内置清单。企业策略不允许的 OKR API、高级组织信息和与内置功能无关的邮箱、审批等权限不加入清单。

## 源码安装

1. 用 `lark-cli auth status --json --verify` 核对当前默认 App、用户和 Bot。
2. 即使已有有效登录，也执行 `./scripts/jarvis-lark-auth check`。通过则复用，不反复登录；失败保留原始错误，区分未登录、缺 scope 与工具本身故障。
3. 未登录或缺少所需 scope 时运行 `./scripts/jarvis-lark-auth begin`，一次申请整份清单。遵循 lark-shared 的 split-flow：展示本次新链接与二维码，用户完成后由 Agent 用该次 `device_code` 执行 `lark-cli auth login --device-code <device_code>`。过期则重新 begin，不复用旧码。
4. 再次检查身份及 `./scripts/jarvis-lark-auth check`，然后执行 `feishu-capability-audit.md` 的只读能力验证。授权未完成时不能把相应清单项勾选为完成。

## 桌面安装

应用的授权按钮调用相同脚本的 `begin`，保留现有链接、二维码与后台完成 device flow 的交互。检查已有登录时调用 `check`，缺少安装所需权限则继续显示授权操作，不能直接跳过。

脚本随 runtime 的 `scripts/` 打包，不调用源码安装器，不要求桌面用户 clone 仓库。世界模型任务复用安装结果，继续只读审计，不在后台另起 OAuth 流程。

## 三种缺口分开处理

- 用户 OAuth scope 缺失：回到上述安装授权入口统一补齐。
- App/Bot 未开通权限、未发布或缺事件订阅：修复同一 App 的开放平台配置。用户 OAuth 不替代 Bot 配置。
- 单篇妙记或文档没有分享给当前用户：保留资源级错误，处理该资源的访问权限。即使 scope 齐全，也不能宣称用户可以读取所有资源。
