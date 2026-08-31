#!/bin/zsh
# 把 deploy/<label>.plist.template 渲染成 ~/Library/LaunchAgents/<label>.plist。
# launchd 不接受相对路径，所以仓库里只存占位符模板，安装时才按本机实际位置展开。
# 成功时把渲染结果的路径打印到 stdout，供安装脚本继续 bootstrap。
set -euo pipefail

if (( $# < 1 || $# > 2 )); then
  print -u2 "Usage: ${0:t} <template-label> [CONFIG_PATH]"
  exit 2
fi

label=$1
script_dir=${0:A:h}
repo_dir=${script_dir:h}
template="$repo_dir/deploy/$label.plist.template"
agents_dir="$HOME/Library/LaunchAgents"
instance=''
if [[ $label == com.bytedance.jarvis.server || $label == com.bytedance.jarvis.web || $label == com.bytedance.jarvis.chat ]]; then
  template_label=$label
  instance=$("$script_dir/jarvis-instance" "${2:-$repo_dir/conf/config.yaml}")
  label=$(jq -er .launchd_label <<<"$instance")
  [[ $template_label != com.bytedance.jarvis.web ]] || label="$label.web"
  [[ $template_label != com.bytedance.jarvis.chat ]] || label=$(jq -er .chat_launchd_label <<<"$instance")
fi
target="$agents_dir/$label.plist"

if [[ ! -f $template ]]; then
  print -u2 "launchd template not found: $template"
  exit 1
fi

mkdir -p "$agents_dir"
# 旧版本把仓库里的 plist 软链到这里；跟随软链写入会改坏仓库文件。
python3 - "$template" "$target" "$repo_dir" "$HOME" "$instance" <<'PY'
import json, pathlib, plistlib, sys
template, target, root, home, instance = sys.argv[1:]
def expand(value):
    if isinstance(value, str): return value.replace('__JARVIS_ROOT__', root).replace('__HOME__', home)
    if isinstance(value, list): return [expand(x) for x in value]
    if isinstance(value, dict): return {k: expand(v) for k, v in value.items()}
    return value
with open(template, 'rb') as f: config = expand(plistlib.load(f))
if instance:
    settings = json.loads(instance)
    if config['Label'] == 'com.bytedance.jarvis.web':
        config['Label'] = settings['launchd_label'] + '.web'
        config['EnvironmentVariables']['JARVIS_CONFIG'] = settings['config_path']
    elif config['Label'] == 'com.bytedance.jarvis.chat':
        config['Label'] = settings['chat_launchd_label']
        config['ProgramArguments'][-1] = settings['config_path']
    else:
        config['Label'] = settings['launchd_label']
        config['ProgramArguments'][-1] = settings['config_path']
        logs = settings['log_files']
        if len(logs) < 2: raise SystemExit('launchd requires server.log_files for stdout and stderr')
        for key, path in zip(('StandardOutPath', 'StandardErrorPath'), logs):
            path = pathlib.Path(root) / path
            path.parent.mkdir(parents=True, exist_ok=True)
            config[key] = str(path)
p = pathlib.Path(target)
if p.is_symlink(): p.unlink()
with p.open('wb') as f: plistlib.dump(config, f)
PY
plutil -lint "$target" >/dev/null
print -r -- "$target"
