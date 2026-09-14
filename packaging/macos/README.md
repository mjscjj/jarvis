# Jarvis macOS 打包与发布指引

> Status: current
> Authority: macOS 应用打包与自动更新发布操作入口
> Last verified: 2026-09-14

本文说明如何从源码生成 Apple Silicon 版 `Jarvis.app`、DMG 和 Tauri 自动更新包，
以及如何发布自动更新。用户安装、覆盖升级和客户端排障见
[macOS 安装与自动更新](../../docs/reference/macos-install-and-update.md)。

Agent 执行打包、验收、发布或续跑时，统一使用
[release-jarvis-desktop Skill](../../.agents/skills/release-jarvis-desktop/SKILL.md)。
Skill 负责流程和验收记录，本文负责脚本、依赖及部署参数；不维护第二套打包实现。

当前产物支持 macOS 14 及以上、Apple Silicon（arm64）。应用使用 ad-hoc 签名，
尚未接入 Apple Developer ID 和 notarization；自动更新包始终使用独立的 Tauri
私钥签名。

macOS 最低版本，以及 Lark CLI、jq 的版本、下载地址和摘要由
`packaging/macos/runtime-manifest.sh` 定义；CC Connect 的集成版本由
`integrations/cc-connect/manifest.sh` 定义。修改这些机器约束时应更新对应 manifest，
本文只说明操作方式。

## 最短打包流程

在仓库根目录执行：

```bash
./packaging/macos/build-dmg.sh
```

成功后生成：

```text
desktop/src-tauri/target/release/bundle/macos/Jarvis.app
desktop/src-tauri/target/release/bundle/macos/Jarvis.app.tar.gz
desktop/src-tauri/target/release/bundle/dmg/Jarvis_<version>_aarch64.dmg
```

本地构建不需要 updater 私钥。`build-dmg.sh` 生成最终签名的应用、DMG 和更新压缩包；
发布脚本在构建完成后单独读取私钥，为更新压缩包生成 `.sig`。

## 构建前准备

### 机器与工具链

- macOS 14 或更高版本、Apple Silicon（arm64）。
- Go 1.26.4 或更高版本。
- 满足 `web/package-lock.json` 所锁定 Vite 版本要求的 Node.js 和 npm；当前要求为
  `^20.19.0` 或 `>=22.12.0`。
- Rust 和 Cargo；`build-dmg.sh` 会将 `~/.cargo/bin` 加入 `PATH`。
- Xcode Command Line Tools，提供 C 编译器、`codesign`、`lipo`、`vtool`、
  `otool` 和 `hdiutil`。
- `curl`、`shasum`；发布时还需要本机 `jq`、`ssh` 和 `scp`。

建议先检查：

```bash
./scripts/check-build-toolchain.sh
command -v cargo npm node traex
```

### runtime 输入

打包脚本需要以下 arm64 可执行文件：

- Qdrant：默认 `bin/qdrant`，可用 `JARVIS_QDRANT_BIN` 覆盖。
- CC Connect：默认 `bin/cc-connect-jarvis`，可用 `JARVIS_CC_CONNECT_BIN` 覆盖。
- Trae CLI：默认从 `PATH` 或 `~/.local/bin/traex` 查找，可用
  `JARVIS_TRAEX_BIN` 覆盖。
- Node.js：默认使用 `PATH` 中的 `node`，可用 `JARVIS_NODE_BIN` 覆盖。

缺少仓库内依赖时可执行：

```bash
./scripts/jarvis-install install-qdrant
./scripts/jarvis-install install-cc-connect
```

runtime 还会在构建时：

- 从官方 GitHub Release 下载 manifest 固定的 Lark CLI arm64 压缩包，先校验
  SHA-256，再提取原生 binary 和许可证。打包不读取本机 `lark-cli` 或
  `JARVIS_LARK_CLI_BIN`，不要求打包者或最终用户预装 Lark CLI。
- 从 jqlang GitHub Release 下载固定版本的 jq，并校验 SHA-256。
- 从 npm 安装固定版本的 BytedCLI，默认版本为 `0.147.0`，可用
  `JARVIS_BYTEDCLI_VERSION` 覆盖。
- 将 Trae CLI 同时作为 `traex` 和 `codex` 入口打包。

桌面运行时优先使用包内 Lark CLI；包内文件缺失或不可执行时直接报错。Lark CLI
和内嵌 Skills 随 Jarvis 更新，不在已签名的应用内单独执行 `lark-cli update`。
源码开发仍可通过 `lark_cli.bin` 选择本机 CLI。二进制由安装包提供，飞书账号仍由
用户授权；当前继续使用 Lark CLI 默认配置，不额外创建凭据存储。

升级 Lark CLI 时一起修改 manifest 的版本、官方 URL 和压缩包摘要，运行打包测试与
最小环境校验，再验收首次授权、消息采集、文档读取和卡片事件协议。压缩包摘要在
签名前校验；应用签名会修改 Mach-O 内容，不拿签名后的 binary 与压缩包摘要比较。

Web 依赖的 lockfile 指向 npmjs，Desktop 依赖的 lockfile 和 BytedCLI 默认指向
`http://bnpm.byted.org`；Go 模块按本机 `GOPROXY` 下载。完整打包通常需要公司网络
以及访问 GitHub Release 的能力。`NPM_CONFIG_REGISTRY` 可覆盖 BytedCLI 的 registry，
但目标 registry 必须包含该内部包。

### updater 签名密钥

发布机首次准备密钥时执行：

```bash
mkdir -p "$HOME/.tauri"
npm --prefix desktop exec tauri signer generate -- \
  --ci -w "$HOME/.tauri/jarvis-updater.key"
```

将生成的公钥正文配置到 `desktop/src-tauri/tauri.conf.json` 的
`plugins.updater.pubkey`。已有客户端信任当前公钥后，不得重新生成并替换密钥；否则旧
客户端无法验证后续更新。私钥以明文文件保存在发布机，不要提交到仓库。

## 完整构建链路

入口是 `./packaging/macos/build-dmg.sh`，流程如下：

1. 检查 macOS、arm64、Cargo、Node.js 和 npm，并执行
   `packaging/macos/*.test.mjs`。
2. 通过 `npm --prefix desktop ci` 安装固定的 Tauri CLI 依赖。
3. 执行 `npm --prefix desktop run dmg`。该命令先运行
   `tauri build --bundles app`，再运行 `bundle-dmg.mjs`。
4. Tauri 根据 `beforeBuildCommand` 调用 `prepare-runtime.sh`：
   - 安装并构建 Web。
   - 构建 `jarvis-server`、`jarvis-app-service` 和 `jarvis-config`。
   - 复制 Qdrant、CC Connect、Trae CLI 和 Node.js。
   - 下载并校验固定版本的 Lark CLI、jq，安装 BytedCLI，并创建 `bytedcli`、`codex` launcher。
   - 复制 `conf/`、`.agents/`、`scripts/`、项目说明和 Web 产物；
     `conf/config.runtime.yaml` 不进入安装包。
   - 校验 runtime 后逐个签名原生 binary，再以 staging 目录替换最终 runtime。
5. Tauri 编译 release 版桌面壳，将 runtime 放入
   `Jarvis.app/Contents/Resources/runtime`；不在 Tauri 阶段生成 updater 包。
6. `bundle-dmg.mjs` 再次校验包内 runtime 和应用最低系统版本，对最终应用执行
   ad-hoc deep signing，并验证签名，再将这份应用归档为 `.app.tar.gz`。
7. DMG staging 目录加入 `Jarvis.app`、`Applications` 软链接，以及
   `web/src/helpDocuments.json` 声明的网页快捷方式。
8. `hdiutil` 生成 HFS+、UDZO、zlib level 9 的 DMG，并执行完整性校验。

DMG 和 updater 包均来自完成最终签名校验的同一份 `Jarvis.app`。构建过程不读取
updater 私钥；发布时单独调用 Tauri signer 生成 `.sig`。

runtime 校验会 fail-fast 检查：

- 所有原生程序都是单一 arm64 Mach-O。
- 每个程序声明的最低 macOS 版本不高于 14.0。
- 动态链接只依赖 `/usr/lib` 和 `/System/Library`。
- lark-cli 内嵌的 `lark-shared`、`lark-contact`、`lark-drive`、`lark-doc`、
  `lark-im` 及其 references 均可离线读取。
- Qdrant、CC Connect、lark-cli、Trae CLI、Node.js、jq、BytedCLI 和 codex
  在最小环境中可以启动并返回预期版本。
- CC Connect 和 Lark CLI 版本与仓库 manifest 一致，Lark CLI 许可证随包分发。

### 启动时的资源同步

`internal/appservice/assets.go` 将包内资源同步到用户状态目录下的 `runtime/`。
`conf/config.yaml`、`scripts/` 和 `web/dist/` 归安装包所有，始终同步；本机设置的
`conf/config.runtime.yaml` 始终跳过。其他有历史哈希的资源继续保留用户修改。

manifest 缺失时不能推断文件是否被修改：对新包中存在且内容不同的每个旧文件，先复制到
状态目录下按 UTC 时间命名的 `asset-backup-*/`，再原子覆盖，并向启动诊断流逐项记录备份和
恢复路径。备份失败直接返回错误；同步完成后才写新 manifest。内容相同的文件、新安装和
不冲突的重启不产生备份；用户自建且包中不存在的文件不参与同步，manifest 损坏仍明确报错。

修改同步代码后运行以下测试，并在发布前验证实际安装包的升级和首次安装：

```bash
go test ./internal/appservice ./internal/config ./internal/onboarding ./internal/store -count=1
```

## 独立验收

完整构建已经包含 runtime、应用签名和 DMG 校验。需要独立复核时执行：

```bash
version="$(jq -r '.version' desktop/src-tauri/tauri.conf.json)"
app="desktop/src-tauri/target/release/bundle/macos/Jarvis.app"
dmg="desktop/src-tauri/target/release/bundle/dmg/Jarvis_${version}_aarch64.dmg"

packaging/macos/validate-runtime.sh \
  "$app/Contents/Resources/runtime" "$app"
codesign --verify --deep --strict --verbose=2 "$app"
hdiutil verify "$dmg"
shasum -a 256 "$dmg"
```

手工安装验收时挂载 DMG，将 `Jarvis.app` 拖入 `/Applications`，再按
[安装指引](../../docs/reference/macos-install-and-update.md)检查首次启动与数据保留。

## 发布自动更新

### 更新版本

每次发布必须使用新的 SemVer，并同步以下文件：

- `desktop/src-tauri/tauri.conf.json`
- `desktop/src-tauri/Cargo.toml`
- `desktop/package.json`
- `desktop/src-tauri/Cargo.lock`
- `desktop/package-lock.json`

`publish-update.sh` 会硬校验前三处版本一致；两个 lockfile 也应随版本修改提交。脚本不会
自动递增版本。远端已存在同版本安装包时直接失败，必须提升版本后发布；不能覆盖
带 immutable 缓存的旧文件。

### 发布命令

推荐从确定提交的独立 detached checkout 构建，封存并验收后发布同一份产物：

```bash
# 在专用 checkout 完成 build-dmg.sh 后；候选父目录须已存在，候选目录须不存在。
node packaging/macos/release-candidate.mjs seal /absolute/checkout /absolute/release/candidate
node packaging/macos/release-candidate.mjs verify /absolute/release/candidate
# 按 Skill 在候选目录维护 acceptance.md，完成实际安装升级验收后执行：
./packaging/macos/publish-update.sh --candidate /absolute/release/candidate "本次更新说明"
```

`candidate.json` 记录版本、commit、DMG/更新归档大小和 SHA-256；`verify` 只检查身份，
不证明人工验收完成。`--candidate` 从候选记录读取版本，核对原文件及上传副本摘要，
单独签名并上传，不重新构建。保留原构建 checkout 的 Tauri signer 供发布使用。
`acceptance.md` 保存验收环境、结果、证据、阻断和下一步，由发布 Agent 审核。

原来的完整构建并发布命令仍可执行，但会重新构建，不能用于发布已经验收的候选：

```bash
./packaging/macos/publish-update.sh "本次更新说明"
```

脚本默认读取 `~/.tauri/jarvis-updater.key`，也可通过
`TAURI_SIGNING_PRIVATE_KEY_PATH` 指定其他文件。加密私钥的密码通过
`TAURI_SIGNING_PRIVATE_KEY_PASSWORD` 传入。

默认发布配置：

```text
SSH 目标：chujiejie.1@10.199.197.219
远端目录：/data00/home/chujiejie.1/jarvis-updates
下载地址：https://jarvisx.bytedance.net/jarvis-updates
```

可分别使用 `JARVIS_UPDATE_REMOTE`、`JARVIS_UPDATE_REMOTE_ROOT` 和
`JARVIS_UPDATE_BASE_URL` 覆盖。

发布脚本会：

1. 检查远端版本文件尚不存在；`--candidate` 校验已有候选，无此参数则执行完整 DMG 构建门禁。
2. 独立签名最终 `.app.tar.gz`，生成版本化的安装包、DMG 和 `latest.json`。
3. 上传到托管目录旁的临时目录。
4. 先移动版本化安装包和 DMG，最后替换 `latest.json`。
5. 从下载地址检查 `latest.json` 版本及更新包可访问性。

这组远端写入不是事务。任一步失败都会直接退出并保留错误；修复后根据远端实际文件决定
是否重跑，不使用旧版本号掩盖失败。

### 托管配置

Jarvis 主服务仅在发布机配置 `JARVIS_UPDATE_ROOT` 时托管更新文件。目录必须已存在；
未配置时不注册更新路由；配置成不存在的目录会导致主服务启动失败。DEV2 配置位置和
网关边界见[运行与部署](../../docs/reference/operations.md#自动更新文件托管)。修改主
服务配置后的构建或重启使用 `./scripts/rebuild-server.sh`。

## 常见失败

- `missing updater private key`：发布时没有找到私钥文件。检查默认密钥路径或设置
  `TAURI_SIGNING_PRIVATE_KEY_PATH`；只做本地构建不需要密钥。
- `missing Qdrant binary`：安装 `bin/qdrant` 或设置 `JARVIS_QDRANT_BIN`。
- `missing CC Connect binary`：安装 `bin/cc-connect-jarvis` 或设置
  `JARVIS_CC_CONNECT_BIN`。
- Lark CLI 下载、摘要或 Skills 检查失败：检查官方发布地址和 manifest；不得跳过
  校验或改用本机 CLI。升级版本需连同二进制和 Skills 一起验收。
- `not a Mach-O executable` 或架构不是 arm64：覆盖变量指向了 launcher、脚本或其他
  架构 binary；改为原生 arm64 binary。
- `non-system dynamic dependency`：输入 binary 依赖包外动态库，不满足自包含要求。
- BytedCLI 安装失败：检查公司网络和 npm registry。
- notarization warning：当前 ad-hoc 内部分发流程的预期提示，不代表 DMG 构建失败。

## 签名边界

当前存在两套独立签名：

- Tauri updater 私钥签名 `.app.tar.gz`，客户端用
  `tauri.conf.json` 中的公钥验签。这是生成和发布更新包的硬要求。
- macOS `codesign` 当前使用 ad-hoc identity `-`。`JARVIS_APP_SIGN_IDENTITY` 只影响
  runtime 内原生 binary；`bundle-dmg.mjs` 最终仍会以 `-` deep-sign 整个应用。

因此当前脚本只适合内部安装。外部分发需要单独改造最终应用签名、Developer ID、
entitlements 和 notarization 流程，不能只设置 `JARVIS_APP_SIGN_IDENTITY`。
