# skill commands: help, accepted flags and handlers.

skill_help() {
  case "$1" in
    list-skills) cat <<'EOF'
usage: jarvis-tools list-skills [--keyword TEXT]
List enabled and disabled Skill summaries. Use get-skill for full content.
EOF
      ;;
    get-skill) cat <<'EOF'
usage: jarvis-tools get-skill --name NAME
Read the complete content of one locally managed Skill.
EOF
      ;;
    *) return 1 ;;
  esac
}

skill_flags() {
  case "$1" in
    list-skills) printf '%s' --keyword ;;
    get-skill) printf '%s' --name ;;
  esac
}

cmd_list_skills() {
  local body
  body="$(api_get /api/skills)"
  printf '%s' "$body" | jq -c --arg keyword "$KEYWORD" '.data | {
    items:[.items[] | select($keyword == "" or (([.name,.description] | map(. // "") | join(" ")) | ascii_downcase | contains($keyword | ascii_downcase))) | {name,description,stages,is_enabled}]
  }'
}

cmd_get_skill() {
  [[ -n "$NAME" ]] || fail "get-skill requires --name"
  local enc body
  enc="$(jq -rn --arg s "$NAME" '$s|@uri')"
  body="$(api_get "/api/skills/${enc}/content")"
  printf '%s' "$body" | json_data
}
