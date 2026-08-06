#!/bin/zsh
# 把 deploy/<label>.plist.template 渲染成 ~/Library/LaunchAgents/<label>.plist。
# launchd 不接受相对路径，所以仓库里只存占位符模板，安装时才按本机实际位置展开。
# 成功时把渲染结果的路径打印到 stdout，供安装脚本继续 bootstrap。
set -euo pipefail

if (( $# != 1 )); then
  print -u2 "Usage: ${0:t} <launchd-label>"
  exit 2
fi

label=$1
script_dir=${0:A:h}
repo_dir=${script_dir:h}
template="$repo_dir/deploy/$label.plist.template"
agents_dir="$HOME/Library/LaunchAgents"
target="$agents_dir/$label.plist"

if [[ ! -f $template ]]; then
  print -u2 "launchd template not found: $template"
  exit 1
fi

mkdir -p "$agents_dir"
# 旧版本把仓库里的 plist 软链到这里；跟随软链写入会改坏仓库文件。
rm -f "$target"
sed -e "s|__JARVIS_ROOT__|$repo_dir|g" -e "s|__HOME__|$HOME|g" "$template" >"$target"
plutil -lint "$target" >/dev/null
print -r -- "$target"
