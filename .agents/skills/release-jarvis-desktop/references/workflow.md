# 构建和发布操作

所有路径相对仓库根，Shell 使用 zsh。下列变量由本次真实仓库和候选确定，不复制旧任务的版本、路径或远端状态。

## 1. 确定输入

读取 `git status --short`、当前分支、远端和目标提交。用户要求 main 最新版本时查询实际远端后选择，不静默把本机落后的 main 当最新。保留其他人修改。

正式版本先同步 desktop 的 tauri.conf.json、Cargo.toml、package.json 和两个 lockfile；对应修改需属于选定提交。只要求打包时不擅自提升版本。远端已经存在相同版本不能覆盖。

在仓库外创建一个持久的本次发布目录；`checkout`、构建日志和稍后创建的 `candidate` 是兄弟目录：

```zsh
repo_root="$(git rev-parse --show-toplevel)"
release_root="$(mktemp -d "$HOME/jarvis-release.XXXXXX")"
release_commit="$(git rev-parse HEAD)"  # 仅在 HEAD 已确认是目标提交后使用
release_checkout="$release_root/checkout"
release_candidate="$release_root/candidate"
git worktree add --detach "$release_checkout" "$release_commit"
```

这个 checkout 仅供本次构建，不在那里继续开发。新提交或重新构建使用新候选目录。
候选必须包含当前发布流程工具；不要把旧源码构建产物贴上新提交身份。

根据 packaging/macos/README.md 检查工具链、网络和 runtime 输入。仓库的忽略目录 `bin/` 不会被 worktree 带过去；缺少依赖应在候选 checkout 按现有安装命令准备，或指定已核验的绝对二进制路径。显式固定 `JARVIS_QDRANT_BIN`、`JARVIS_CC_CONNECT_BIN`、`JARVIS_TRAEX_BIN`、`JARVIS_NODE_BIN`，记录来源、版本及 SHA-256。构建期间不升级或替换这些输入。

`runtime-manifest.sh` 与 CC Connect manifest 是现有版本约束；尚未被 manifest 固定的输入，必须如实记录实际版本，不能宣称已完全可复现构建。不要从 PATH 临时换一个版本来跳过校验。

## 2. 构建并封存

根据变更执行必要源码测试，构建所含检查见 packaging README。记录命令、结果和日志，管道开启 pipefail：

```zsh
set -o pipefail
(
  cd "$release_checkout"
  ./packaging/macos/build-dmg.sh
) 2>&1 | tee "$release_root/build.log"
# 上一步必须成功；不能在失败后继续 seal。
node "$release_checkout/packaging/macos/release-candidate.mjs" \
  seal "$release_checkout" "$release_candidate"
```

seal 要求 detached checkout、无 tracked 改动、三处版本一致、两个非空产物，并拒绝复用目标目录。
它复制最终 DMG/归档并记录 commit、版本、大小和 SHA-256；复制后重新核对摘要。
失败留下的目录只用于诊断，新候选换一个目录，不删除旧记录掩盖失败。

在候选目录创建 `acceptance.md`，使用 acceptance.md 参考中的模板，链接兄弟目录的构建日志。
此时状态为“已构建，待验收”，不是“可发布”。

## 3. 验收和签名

先 `verify` 核对文件，解包候选归档到独立验收目录，按 packaging README 对取出的 App 运行 runtime 和 codesign 检查，对候选 DMG 运行 hdiutil verify。执行安装升级矩阵并逐项留证。

真实 updater 测试需要签名，用同一个候选归档和已有受信任私钥：

```zsh
release_version="$(jq -er '.version' "$release_candidate/candidate.json")"
release_archive="$release_candidate/Jarvis_${release_version}_aarch64.app.tar.gz"
env -u TAURI_SIGNING_PRIVATE_KEY \
  "$release_checkout/desktop/node_modules/.bin/tauri" signer sign \
  --private-key-path "${TAURI_SIGNING_PRIVATE_KEY_PATH:-$HOME/.tauri/jarvis-updater.key}" \
  --password "${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}" "$release_archive"
```

签名生成 sidecar，不修改归档。不要轮换受信任公钥。使用隔离测试托管地址及测试用户状态完成真实更新，不为了验收先修改正式 latest。实际旧客户端是否能使用测试端点需检查其配置；做不到就记为未验收，不用模拟 IPC 顶替。

验证后若再次构建、重新签 App 或修改归档/DMG，创建新候选并重新验收；仅重新生成 updater sidecar 不改变已验收 App。

## 4. 发布同一候选

先读取记录，确认用户发布意图已明确、验收无阻断、版本未占用。缺少发布意图时交付候选及结果即可。

```zsh
node "$release_checkout/packaging/macos/release-candidate.mjs" verify "$release_candidate"
"$release_checkout/packaging/macos/publish-update.sh" \
  --candidate "$release_candidate" "本次真实变更说明"
```

从构建 checkout 调用，保留其中锁定的 Tauri signer。脚本从候选记录读取版本，不受外面开发工作区版本变化影响；核对摘要，签名，复核待上传副本，再复用原有上传和 latest 切换流程。它不会重新构建，也不会自动判断 acceptance.md 内容是否充分。

托管参数和私钥位置遵循 packaging README；没有修改托管配置的需要就不要重启服务器。
发布后从真实公开地址读回 latest，核对版本、URL、签名，并下载公开 updater/DMG 核对 candidate.json 中的摘要；将结果记入 acceptance.md。脚本当前只自动检查 manifest 版本和更新包可访问性，不能把它当成完成了公开文件摘要核对。

如果发布中断：保留日志；读取实际远端版本文件、临时目录、latest。版本文件已经存在时不能直接重跑覆盖；说明已完成和未完成的步骤，再按实际状态完成剩余发布。修复包内容则必须使用新版本和新候选。

## 5. 续跑

先核对 candidate.json 对应产物是否仍在、verify 是否通过，然后读取 acceptance.md 的当前阶段和未完成项。保留 checkout 直到发布/验收结束；不要因为换了 Agent 就重新构建。若 checkout 已删除，恢复同一 commit 和所需 signer 工具，不改变候选产物。
