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

挂载 DMG 后将 `Jarvis.app` 拖到 `Applications`，首次启动依次完成：

1. 字节 SSO
2. 飞书 Bot App 绑定
3. 飞书用户授权
4. Trae CLI 登录
5. 助手名称与本机身份配置
6. 世界模型初始化

## 签名

默认使用 ad-hoc 签名，仅用于内部测试。对外分发前需配置 Apple Developer ID，
并完成 notarization。

## 常见问题

- `missing Qdrant binary`：安装到 `bin/qdrant`，或设置 `JARVIS_QDRANT_BIN`。
- `missing CC Connect binary`：安装到 `bin/cc-connect-jarvis`，或设置
  `JARVIS_CC_CONNECT_BIN`。
- 授权按钮无响应：确认使用的是最新 DMG；桌面外链依赖 Tauri opener，普通旧
  `.app` 不会自动更新。
