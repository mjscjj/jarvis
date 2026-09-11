#!/usr/bin/env bash

# Sourced by jarvis-deploy after resolving the selected instance. Only retire
# that instance's service; other checkouts may still use the old deployment.
retire_chat_sidecar() {
  local label="$1" repo_root="$2" unit unit_path target
  case "$(uname -s)" in
    Linux)
      systemctl --user show-environment >/dev/null
      unit="${label}.chat.service"
      unit_path="${HOME}/.config/systemd/user/${unit}"
      if [[ -f "$unit_path" ]] || systemctl --user cat "$unit" >/dev/null 2>&1; then
        systemctl --user disable --now "$unit"
        rm -f -- "$unit_path"
        systemctl --user daemon-reload
      fi
      ;;
    Darwin)
      target="gui/${UID}/${label}.chat"
      if launchctl print "$target" >/dev/null 2>&1; then
        launchctl bootout "$target"
      fi
      rm -f -- "${HOME}/Library/LaunchAgents/${label}.chat.plist"
      ;;
    *) printf 'Unsupported system for chat sidecar retirement\n' >&2; return 1 ;;
  esac
  rm -f -- "${repo_root}/bin/jarvis-chat-server" "${repo_root}/bin/jarvis-chat-server.next" \
    "${repo_root}/var/log/jarvis-chat.log" "${repo_root}/var/log/jarvis-chat.error.log"
}
