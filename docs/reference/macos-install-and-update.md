# macOS 安装与自动更新

> Status: current
> Authority: macOS 安装与更新操作入口
> Last verified: 2026-09-11

本文面向使用 DMG 安装 Jarvis 的内部用户。源码安装、开发环境和 Linux 部署见
[运行与部署](operations.md)；构建和发布更新见
[macOS 打包说明](../../packaging/macos/README.md)。

## 支持范围

- macOS 14 或更高版本
- Apple Silicon（arm64）
- 当前使用 ad-hoc 应用签名，尚未接入 Apple Developer ID 和 notarization
- 更新包始终使用独立的 Tauri 签名校验，不能关闭

## 首次安装

当前首个支持自动更新的版本是
[Jarvis 0.1.1 DMG](https://jarvisx.bytedance.net/jarvis-updates/Jarvis_0.1.1_aarch64.dmg)。

1. 下载并打开 DMG。
2. 将 `Jarvis.app` 拖入 `Applications`。
3. 从“应用程序”启动 Jarvis。
4. 按页面完成飞书登录、配置检查和首次初始化。

因为当前没有 Apple Developer ID 正式签名，macOS 可能拦截首次启动。内部确认下载地址
无误后，可在“系统设置 → 隐私与安全性”中选择仍要打开。不要通过删除用户数据来解决
签名提示。

## 数据位置与覆盖安装

应用程序位于 `/Applications/Jarvis.app`，用户状态位于：

```text
~/Library/Application Support/Jarvis
```

覆盖安装新版应用不会删除该目录。配置、SQLite、世界模型、运行产物和日志继续复用；
新版启动时同步包内配置、Skills、脚本和 Web 资源，并保留用户改过的文件。数据库执行
当前代码声明的迁移；无法安全推断的历史 schema 会 fail-fast，不会静默重建数据。

## 自动更新

从 0.1.1 开始，Jarvis 在本地服务成功启动后读取：

```text
https://jarvisx.bytedance.net/jarvis-updates/latest.json
```

发现更高 SemVer 后，Tauri updater 会下载对应的 `.app.tar.gz`，使用应用内置公钥验证
`.sig`，安装并重启。更新源由 DEV2 的 `jarvisx.bytedance.net` HTTPS gateway 提供。

0.1.0 及更早的安装包没有 updater，必须先手动覆盖安装 0.1.1；后续版本才能走自动
更新。退出 Jarvis 会停止其本机子服务，不会删除数据。

## 当前线上入口

| 内容 | 地址 |
|---|---|
| 更新清单 | <https://jarvisx.bytedance.net/jarvis-updates/latest.json> |
| 0.1.1 DMG | <https://jarvisx.bytedance.net/jarvis-updates/Jarvis_0.1.1_aarch64.dmg> |
| 0.1.1 updater 包 | <https://jarvisx.bytedance.net/jarvis-updates/Jarvis_0.1.1_aarch64.app.tar.gz> |

更新清单是客户端判断最新版本的真源；表中的版本化下载链接用于首次安装和人工恢复。

## 验证与排障

```bash
curl -fsS https://jarvisx.bytedance.net/jarvis-updates/latest.json | jq
curl -fsSI https://jarvisx.bytedance.net/jarvis-updates/Jarvis_0.1.1_aarch64.dmg
```

- 清单或安装包返回非 2xx：检查 DEV2 gateway 和
  `/data00/home/chujiejie.1/jarvis-updates`。
- 已是最新版本：updater 不执行下载和重启。
- 签名校验失败：停止发布，不允许绕过；检查发布机私钥是否与
  `desktop/src-tauri/tauri.conf.json` 的公钥成对。
- 更新后启动失败：查看 `~/Library/Application Support/Jarvis/logs`，保留原数据目录
  排查，不自动清库。
