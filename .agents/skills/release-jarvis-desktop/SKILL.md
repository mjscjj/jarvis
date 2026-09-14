---
name: release-jarvis-desktop
description: 从确定的 Jarvis 源码提交构建 macOS 桌面候选包，记录产物身份和安装升级验收，并发布同一份已验收产物；用于打 DMG、发布桌面新版本、继续候选验收或排查发布失败。源码安装和日常服务重启不走此流程。
---

# Jarvis 桌面发布

管理一条链路：确定提交 → 隔离构建 → 封存候选 → 安装升级验收 → 发布原产物 → 公开地址读回。
复用 `packaging/macos/`；不重新实现打包、签名或上传，不把完整包构建作为日常服务重启。

## 入口与真源

先定位包含 `desktop/`、`packaging/macos/` 的完整仓库，遵循其中 AGENTS.md。
阅读 [构建和发布操作](references/workflow.md)；首次验收或发布前再读 [验收与续跑记录](references/acceptance.md)。

工具链、依赖来源、签名与托管参数的唯一操作说明是仓库 `packaging/macos/README.md`；
版本和摘要以相应 manifest 为准，不从 Skill 复制常量。用户安装说明是
`docs/reference/macos-install-and-update.md`。

| 工作 | 现有入口 |
| --- | --- |
| 机器与工具链检查 | `scripts/check-build-toolchain.sh` |
| 构建 App、DMG、更新归档 | `packaging/macos/build-dmg.sh` |
| 准备、检查 runtime | 构建自动调用 `prepare-runtime.sh`、`validate-runtime.sh` |
| 记录和核对候选身份 | `node packaging/macos/release-candidate.mjs seal / verify` |
| 发布已有候选 | `packaging/macos/publish-update.sh --candidate <目录> "更新说明"` |

## 执行约定

1. **确定本次目标。** 用户只要打包就交付候选；要验收就执行验收；已明确要求发布时继续发布，不重复询问。创建或修改本 Skill 不构成发布请求。不要擅自升级版本、纳入其他人的未提交代码或给别人发送通知。
2. **复用已有候选。** 用户要求续跑时先读取 `candidate.json`、`acceptance.md` 和日志，运行 `verify`；哈希不同即为新产物，不能复用旧验收。没有候选才从确定 commit 的独立 detached checkout 开始构建。
3. **构建一次再封存。** 锁定输入路径，记录实际依赖身份和构建日志；构建成功后 seal 到新目录。`seal` 只记录身份，不证明构建或业务验收通过，也不锁定机器上依赖的来源。
4. **验收与包绑定。** 从封存 DMG/归档取出 App 验证，不拿另一个工作区的 server 或 Web 代替。每项写实际结果、环境与证据；未执行就是未执行，模拟 updater 不算真实升级。按改动影响选择专项场景并写明理由。
5. **发布不再构建。** 核对必需验收完成且没有未解决的发布阻断，再调用 `--candidate`。旧的无参数发布模式会重新构建，不用于这条验收后发布链路。机器脚本保证版本和文件身份；验收是否充分由执行 Agent 根据记录判断，脚本成功不代表人工验收通过。
6. **失败如实续跑。** 保留候选和日志，记录失败阶段；不要循环上传、覆盖已发布同版本文件、跳过签名、自动清空用户目录或猜测迁移旧数据库。上传结果不确定时先读回远端文件和 latest，再决定下一步。

## 交付

`candidate.json` 保存机器身份；`acceptance.md` 保存流程进度、验收证据和下一步，两者都随候选目录保留。
重要交付摘要写入 `docs/summery/`；大型候选放仓库外，不提交 App、DMG、密钥或测试用户数据。

最终说明版本、commit、候选路径/下载地址、实际通过的验收、未验证项，以及是否已经发布。
只有“文件已上传”不能称为“用户升级已验收”。桌面恢复页面、资源恢复及业务缺陷由相应产品代码实现，本 Skill 不把设计方案冒充已有能力。
