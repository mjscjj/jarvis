# Jarvis macOS 打包

当前脚本生成 Apple Silicon (`arm64`) 的自包含 `Jarvis.app` 和 DMG。运行数据写入
`~/Library/Application Support/Jarvis`，不会写回应用包。

## 环境要求

- macOS Apple Silicon
- Go、Node.js/npm、Rust/Cargo
- `lark-cli`、`traex`、`jq`
- Qdrant 和 CC Connect 二进制

默认从仓库读取：

```text
bin/qdrant
bin/cc-connect-jarvis
```

若它们不在仓库中，可通过环境变量指定：

```bash
export JARVIS_QDRANT_BIN=/absolute/path/to/qdrant
export JARVIS_CC_CONNECT_BIN=/absolute/path/to/cc-connect-jarvis
export JARVIS_LARK_CLI_BIN=/absolute/path/to/lark-cli
export JARVIS_TRAEX_BIN=/absolute/path/to/traex
```

BytedCLI 会按脚本中固定的版本安装到 runtime，无需全局安装。

lark-cli 必须支持 `skills read`，并内嵌 `lark-shared`、`lark-contact`、`lark-drive`、`lark-doc`、`lark-im` 及其参考文件。打包脚本会对实际待打包的 binary 做离线读取检查；这些说明随 CLI 一起进入 DMG，不复制打包机的个人 Skills，也不要求用户另装。可单独检查：

```bash
bash packaging/macos/check-lark-skills.sh "$(command -v lark-cli)"
```

## 一键打包

在仓库根目录执行：

```bash
./packaging/macos/build-dmg.sh
```

脚本会自动：

1. 安装 Web 和 Tauri 构建依赖。
2. 构建 Web、`jarvis-server`、`jarvis-app-service` 和 `jarvis-config`。
3. 组装并签名 Qdrant、CC Connect、lark-cli、Trae CLI、BytedCLI 等 runtime。
4. 构建 `Jarvis.app`。
5. 创建并校验 DMG。

产物路径：

```text
desktop/src-tauri/target/release/bundle/dmg/Jarvis_0.1.0_aarch64.dmg
```

## 验收

```bash
hdiutil verify desktop/src-tauri/target/release/bundle/dmg/Jarvis_0.1.0_aarch64.dmg
codesign --verify --deep --strict --verbose=2 \
  desktop/src-tauri/target/release/bundle/macos/Jarvis.app
```

挂载 DMG 后将 `Jarvis.app` 拖到 `Applications`。首次启动自动检查登录和配置，只补缺项，全程复用同一个飞书 App/Bot，不另填 App ID。

点击「开始使用」后自动启动服务。登录和服务就绪即进入应用，世界模型在后台初始化，不要求建模完成才能使用，也不把进入应用当作初始化完成。侧边进度面板区分排队、执行、等待条件、需要补充和失败，展示已记录的进展、等待原因及任务运行记录入口；面板可收起，不遮断应用操作。

初始化的完整请求直接携带 Skill 路径、运行目录和恢复要求；同一个任务先建立，再查漏补缺两轮。失败或暂停后点击「从已有结果继续」复用原 Task、工作稿和已写入实体；刷新或重新打开应用只接回任务，不自动反复重试。需要回复时通过原任务的问题卡继续同一会话。取证区分空结果、权限缺失和未完成读取，不按实体数量判定质量。完成后进度面板展示本人职责、项目、关键人物、重点事项、写入位置及未知项的简短结果，关闭后仍可在原任务查看。无需保持安装页打开，但退出 Jarvis 应用会停止本机服务。

DMG 挂载窗口同时提供“插件扩展与解耦规范”和“代码提交与评审方案”两个网页快捷方式。
初始化页面的帮助和开发文档默认折叠；进入应用后可点击左侧导航下方的小问号查看。
链接统一维护在 `web/src/helpDocuments.json`，Web 与 DMG 打包共同读取。

## 签名

默认使用 ad-hoc 签名，仅用于内部测试。对外分发前需配置 Apple Developer ID，
并完成 notarization。

## 常见问题

- `missing Qdrant binary`：安装到 `bin/qdrant`，或设置 `JARVIS_QDRANT_BIN`。
- `missing CC Connect binary`：安装到 `bin/cc-connect-jarvis`，或设置
  `JARVIS_CC_CONNECT_BIN`。
- 授权按钮无响应：确认使用的是最新 DMG；桌面外链依赖 Tauri opener，普通旧
  `.app` 不会自动更新。
