# Jarvis macOS 打包

当前脚本生成 macOS 14 及以上、Apple Silicon (`arm64`) 的自包含 `Jarvis.app` 和 DMG。运行数据写入
`~/Library/Application Support/Jarvis`，不会写回应用包。

本文面向构建和发布人员；用户首次安装、覆盖安装、自动更新和排障见
[macOS 安装与自动更新](../../docs/reference/macos-install-and-update.md)。

## 环境要求

- macOS 14+ Apple Silicon
- Go、Node.js/npm、Rust/Cargo
- `lark-cli`、`traex`
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
runtime 使用的 jq 会从 jqlang 官方 Release 下载固定的 arm64 版本并校验 SHA256，
不复制打包机的 Homebrew jq。

lark-cli 必须支持 `skills read`，并内嵌 `lark-shared`、`lark-contact`、`lark-drive`、`lark-doc`、`lark-im` 及其参考文件。`command -v lark-cli` 通常返回官方 npm 包的启动脚本，打包脚本会将其解析为同一包内的 arm64 原生 binary，再对复制到 runtime 的实际文件做离线读取检查；这些说明随 CLI 一起进入 DMG，不复制打包机的个人 Skills，也不要求用户另装。可单独检查：

```bash
lark_cli_bin="$(packaging/macos/resolve-lark-cli-bin.sh "$(command -v lark-cli)")"
bash packaging/macos/check-lark-skills.sh "$lark_cli_bin"
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
4. 校验每个 Mach-O 的 arm64 架构、最低系统版本和动态依赖闭包，并在最小环境中
   对实际 runtime 命令做冒烟测试。
5. 构建 `Jarvis.app`，再次校验包内 runtime 和应用声明的最低系统版本。
6. 创建并校验 DMG。

产物路径：

```text
desktop/src-tauri/target/release/bundle/dmg/Jarvis_<version>_aarch64.dmg
```

## 发布自动更新

桌面应用启动成功后会通过
`https://jarvisx.bytedance.net/jarvis-updates/latest.json` 检查更新。发现更高版本时，
使用 Tauri updater 校验签名、安装并重启；用户数据仍保存在
`~/Library/Application Support/Jarvis`。

首次发布机准备一次更新签名密钥：

```bash
npm --prefix desktop exec tauri signer generate -- \
  --ci -w "$HOME/.tauri/jarvis-updater.key"
```

私钥只留在发布机。公钥正文注册在 `desktop/src-tauri/tauri.conf.json`；丢失私钥后，
已经安装的客户端无法信任另一把密钥签发的更新。

发布前同步修改并保持相同的 SemVer：

- `desktop/src-tauri/tauri.conf.json`
- `desktop/src-tauri/Cargo.toml`
- `desktop/package.json`

随后执行：

```bash
./packaging/macos/publish-update.sh "本次更新说明"
```

脚本复用完整 DMG 构建门禁，生成 `.app.tar.gz` 和 `.sig`，再将版本化更新包、DMG
与最后写入的 `latest.json` 原子发布到 DEV2。默认目标是
`chujiejie.1@10.199.197.219:/data00/home/chujiejie.1/jarvis-updates`，可用
`JARVIS_UPDATE_REMOTE`、`JARVIS_UPDATE_REMOTE_ROOT` 和
`JARVIS_UPDATE_BASE_URL` 覆盖。首个带 updater 的版本仍需手动安装一次，后续版本
才会自动更新。

## 验收

```bash
hdiutil verify desktop/src-tauri/target/release/bundle/dmg/Jarvis_<version>_aarch64.dmg
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
