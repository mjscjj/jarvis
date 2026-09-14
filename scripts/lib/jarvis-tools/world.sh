# world commands: help, accepted flags and handlers.

world_help() {
  case "$1" in
    list-relations) cat <<'EOF'
usage: jarvis-tools list-relations [--source-type TYPE] [--source-id ID]
                                   [--relation-type TYPE]
                                   [--target-type TYPE] [--target-id ID]
                                   [--node-type TYPE --node-id ID]
                                   [--node-types TYPE[,TYPE...]]
                                   [--limit N]
List generic cross-module relation edges, following API cursors until the
filtered result is complete. --node-type plus --node-id reads both incoming
and outgoing one-hop neighbors; --node-types keeps edges touching any listed
type. The command never infers a relation.
EOF
      ;;
    create-relation) world_payload_intro "create-relation"; printf '%s\n' "Create or refresh one evidence-backed generic relation edge." ;;
    delete-relation) delete_id_help "delete-relation" "Delete one generic relation edge." ;;
    purge-world-entities) world_payload_intro "purge-world-entities"; printf '%s\n' "Physically delete an explicit reviewed set of Project, KeyMatter and Person entities with their relations, facts and page revisions. This does not touch OKR data." ;;
    get-world-progress) cat <<'EOF'
usage: jarvis-tools get-world-progress (--id ID |
       --subject-type okr_point --subject-id POINT_ID --period-key PERIOD)


Read Jarvis's persisted evidence-backed assessment for one OKR point and
period. It is separate from the official human-authored weekly progress.
Specify either the assessment ID or the complete subject-period key.
EOF
      ;;
    create-world-progress) cat <<'EOF'
usage: jarvis-tools create-world-progress [--payload JSON|-]

Create Jarvis's first assessment for an OKR point and period. The payload must
contain expected_version=0, subject_type=okr_point, subject_id, period_key,
signal (unknown|green|yellow|red), summary, evidence object and evidence_until.
Use - or omit --payload to read the JSON object from stdin.
EOF
      ;;
    update-world-progress) cat <<'EOF'
usage: jarvis-tools update-world-progress --id ID [--payload JSON|-]

Update one assessment. The payload must contain the version returned by the
last read as expected_version, plus signal, summary, evidence and
evidence_until. Subject and period are immutable. Use - or omit --payload to
read stdin. An unchanged assessment is a no-op and keeps its version.
EOF
      ;;
    resolve-world-node) cat <<'EOF'
usage: jarvis-tools resolve-world-node --type TYPE --id ID
Read one world-graph node from its authoritative module. Reality entity types
resolve to their Page; OKR and Biz OKR plan node types resolve to their module
definition. This command does not infer or create relations.
EOF
      ;;
    list-page-revisions) cat <<'EOF'
usage: jarvis-tools list-page-revisions --type TYPE --id N [--limit N]
Read complete archived versions of one long-term fact page, newest first. API
cursor pages are followed automatically. To restore text, read the live page
and use update-page with its current CAS timestamp.
EOF
      ;;
    list-projects) cat <<'EOF'
usage: jarvis-tools list-projects [--code CODE] [--keyword TEXT] [--page N] [--limit N]
List compact project summaries. Use get-project for full project background.
EOF
      ;;
    get-project) cat <<'EOF'
usage: jarvis-tools get-project (--id ID | --code CODE)
Use --id for a positive database id; --code is an exact match, failing on zero
or multiple matches. Long-term facts are on get-page; use list-facts for history detail.
EOF
      ;;
    create-project)
      world_payload_intro "$1"
      cat <<'EOF'
Required: name (nonblank string), role (owner|participant), status
(planning|active|paused|archived|done), priority (integer 1..5).
Optional: code (nonblank string or null). Omission clears code on update.
Neither description nor summary is a ProjectInput field.
Returns the saved project including its numeric database id.
Read back: jarvis-tools get-project --id ID (or exact --code CODE).

example:
  jarvis-tools create-project --payload '{"name":"Project","role":"owner","status":"active","priority":3}'
EOF
      ;;
    update-project)
      world_payload_intro "$1"
      cat <<'EOF'
Required: name (nonblank string), role (owner|participant), status
(planning|active|paused|archived|done), priority (integer 1..5).
Optional: code (nonblank string or null). Omission clears code on update.
Neither description nor summary is a ProjectInput field.
Returns the saved project including its numeric database id.
Read back: jarvis-tools get-project --id ID (or exact --code CODE).

example:
  jarvis-tools update-project --id 7 --payload '{"name":"Project","role":"owner","status":"active","priority":3}'
EOF
      ;;
    archive-project) delete_id_help "archive-project" "Archive one project; this does not hard-delete it." ;;
    list-key-matters) cat <<'EOF'
usage: jarvis-tools list-key-matters [--all] [--keyword TEXT] [--page N] [--limit N]
List compact open key matters; --all includes closed ones. Use get-key-matter for full background.
EOF
      ;;
    get-key-matter) cat <<'EOF'
usage: jarvis-tools get-key-matter --id ID
Get one key matter. Long-term facts are on get-page; use list-facts for history detail.
EOF
      ;;
    create-key-matter)
      world_payload_intro "$1"
      cat <<'EOF'
Required: title (nonblank string).
Optional: status (free text, defaults to empty; not an enum), project_id
(existing positive project database id or null), due_at (RFC3339 string or null).
Omitted status/project_id/due_at become empty/null on replacement.
Returns the saved key matter. Closed/open lifecycle is separate from status text;
use close-key-matter to close and --all when listing closed matters.
Read back: jarvis-tools get-key-matter --id ID.

example:
  jarvis-tools create-key-matter --payload '{"title":"Follow up"}'
EOF
      ;;
    update-key-matter)
      world_payload_intro "$1"
      cat <<'EOF'
Required: title (nonblank string).
Optional: status (free text, defaults to empty; not an enum), project_id
(existing positive project database id or null), due_at (RFC3339 string or null).
Omitted status/project_id/due_at become empty/null on replacement.
Returns the saved key matter. Closed/open lifecycle is separate from status text;
use close-key-matter to close and --all when listing closed matters.
Read back: jarvis-tools get-key-matter --id ID.

example:
  jarvis-tools update-key-matter --id 7 --payload '{"title":"Follow up"}'
EOF
      ;;
    touch-key-matter) delete_id_help "touch-key-matter" "Mark one open key matter as freshly active without changing its content." ;;
    close-key-matter) delete_id_help "close-key-matter" "Close one key matter; this does not hard-delete it." ;;
    list-groups) cat <<'EOF'
usage: jarvis-tools list-groups [--chat-id CHAT_ID] [--keyword TEXT] [--page N] [--limit N]
Search compact Feishu group summaries. Use get-group for full background.
EOF
      ;;
    get-group) cat <<'EOF'
usage: jarvis-tools get-group --chat-id CHAT_ID
Get the group binding by exact Feishu chat_id, including unmonitored groups.
Fails on zero or multiple matches. The returned id is the numeric database id.
EOF
      ;;
    get-world-overview) cat <<'EOF'
usage: jarvis-tools get-world-overview [--section TYPE] [--query TEXT] [--offset N] [--limit N] [--id CURRENT_TASK_ID]
Live partial directory. Details are read with get-page/get-task/get-todo.
EOF
      ;;
    get-context) cat <<'EOF'
usage: jarvis-tools get-context [--chat-id CHAT_ID] [--project-id ID]
Assemble the canonical current context. An explicit project wins; otherwise a
chat resolves through its Jarvis group binding. With neither flag, returns the
principal-level global context.
EOF
      ;;
    update-group)
      world_payload_intro "$1"
      cat <<'EOF'
Payload fields: project_id (existing positive project database id or null),
related_group, pinned, include_in_memory, is_key_group (booleans).
All five control fields are required; read get-group first to preserve current values. Enabling
related_group starts monitoring and automatically pins; disabling it unpins.
--id is the numeric Group row id from get-group, NOT Feishu chat_id.
Capture-owned fields chat_id/name/description/tier cannot be replaced here.
Returns the saved group. Read back: jarvis-tools get-group --chat-id CHAT_ID.

example:
  jarvis-tools update-group --id 7 --payload '{"project_id":null,"related_group":true,"pinned":true,"include_in_memory":true,"is_key_group":false}'
EOF
      ;;
    get-principal) cat <<'EOF'
usage: jarvis-tools get-principal
Get the profile of the person Jarvis assists.
EOF
      ;;
    update-principal)
      world_payload_intro "$1"
      cat <<'EOF'
Required: name (nonblank string).
Optional: department, title, leader_open_id, leader_name (string or null).
Omitted optional fields are cleared. open_id is fixed by instance configuration
and cannot be supplied here; no --id is needed for the single principal profile.
Returns the saved profile (including id, open_id and saved).
Read back: jarvis-tools get-principal.

example:
  jarvis-tools update-principal --payload '{"name":"Principal"}'
EOF
      ;;
    list-persons) cat <<'EOF'
usage: jarvis-tools list-persons [--open-id OPEN_ID] [--role leader|key|colleague|other]
                                 [--keyword TEXT] [--page N] [--limit N]
List compact person summaries. Use get-person for full details.
EOF
      ;;
    get-person) cat <<'EOF'
usage: jarvis-tools get-person --open-id OPEN_ID
Get one person by exact Feishu open_id; zero/multiple matches fail. The returned
id is the numeric database id used for update/delete. Long-term facts are on get-page; use
list-facts for history detail.
EOF
      ;;
    create-person)
      world_payload_intro "$1"
      cat <<'EOF'
Required for create/update: name (nonblank), role (leader|key|colleague|other).
Optional editable fields: department/title (string or null), priority_weight
(number 0..1, defaults to 0), is_active (boolean or null; omitted/null means true).
Create additionally requires open_id (nonblank Feishu identity), and accepts
union_id, feishu_user_id, en_name, avatar_url, p2p_chat_id (string or null).
Update does NOT accept those identity fields: use --id with the numeric Person
row id returned by get-person, NOT the Feishu open_id. Omitted department/title
are cleared, priority_weight resets to 0 and is_active defaults to true.
Returns the saved person with id and open_id.
Read back: jarvis-tools get-person --open-id OPEN_ID.

example:
  jarvis-tools create-person --payload '{"open_id":"ou_person","name":"Person","role":"key"}'
EOF
      ;;
    update-person)
      world_payload_intro "$1"
      cat <<'EOF'
Required for create/update: name (nonblank), role (leader|key|colleague|other).
Optional editable fields: department/title (string or null), priority_weight
(number 0..1, defaults to 0), is_active (boolean or null; omitted/null means true).
Create additionally requires open_id (nonblank Feishu identity), and accepts
union_id, feishu_user_id, en_name, avatar_url, p2p_chat_id (string or null).
Update does NOT accept those identity fields: use --id with the numeric Person
row id returned by get-person, NOT the Feishu open_id. Omitted department/title
are cleared, priority_weight resets to 0 and is_active defaults to true.
Returns the saved person with id and open_id.
Read back: jarvis-tools get-person --open-id OPEN_ID.

example:
  jarvis-tools update-person --id 7 --payload '{"name":"Person","role":"key"}'
EOF
      ;;
    delete-person) delete_id_help "delete-person" "Delete one person." ;;
    query-resources) cat <<'EOF'
usage: jarvis-tools query-resources [--project-id ID] [--person-open-id OPEN_ID] [--principal-only] [--keyword TEXT] [--page N] [--limit N]
Query active managed resources such as project repos, documents, links and notes.
EOF
      ;;
    get-resource) cat <<'EOF'
usage: jarvis-tools get-resource --id ID
Get one managed resource in full.
EOF
      ;;
    create-resource)
      world_payload_intro "$1"
      cat <<'EOF'
Required: title (nonblank), resource_type (doc|link|repo|note|other).
Optional: url (nonblank string or null), person_id/project_id (existing positive
numeric database ids or null), link_principal (boolean, default false), is_active
(boolean or null; omitted/null means true). Omitted links/url are cleared on
replacement. A resource can link to one person and one project simultaneously.
This is a managed resource id, NOT a captured-resource id; person_id is NOT an
open_id. Returns the saved managed resource.
Read back: jarvis-tools get-resource --id ID.

example:
  jarvis-tools create-resource --payload '{"title":"Reference","resource_type":"note"}'
EOF
      ;;
    update-resource)
      world_payload_intro "$1"
      cat <<'EOF'
Required: title (nonblank), resource_type (doc|link|repo|note|other).
Optional: url (nonblank string or null), person_id/project_id (existing positive
numeric database ids or null), link_principal (boolean, default false), is_active
(boolean or null; omitted/null means true). Omitted links/url are cleared on
replacement. A resource can link to one person and one project simultaneously.
This is a managed resource id, NOT a captured-resource id; person_id is NOT an
open_id. Returns the saved managed resource.
Read back: jarvis-tools get-resource --id ID.

example:
  jarvis-tools update-resource --id 7 --payload '{"title":"Reference","resource_type":"note"}'
EOF
      ;;
    touch-resource) delete_id_help "touch-resource" "Mark one enabled managed resource as freshly active without changing its content." ;;
    delete-resource) delete_id_help "delete-resource" "Delete one managed resource." ;;
    get-page) cat <<'EOF'
usage: jarvis-tools get-page --type TYPE --id N
Read one entity's long-term fact page. TYPE is principal, person, project,
key_matter, group or resource. Returns the full summary, character count and
limit, updated_at for CAS, outbound and back links, and the subject's fact
count without fact content. Use get-page-guidance before the first page edit in
an Agent session; use list-facts for history detail.
EOF
      ;;
    get-page-guidance) cat <<'EOF'
usage: jarvis-tools get-page-guidance
Read the single shared content contract for every entity's current cognition
page. Use it before the first update-page call in an Agent session. Returns the
registered Markdown guidance and its metadata.
EOF
      ;;
    update-page) cat <<'EOF'
usage: jarvis-tools update-page --type TYPE --id N --content -|TEXT
                               --if-unchanged-since TS
Replace one entity's long-term fact page. TYPE is principal, person, project,
key_matter, group or resource. --content - reads stdin. --if-unchanged-since is
the updated_at from get-page; a 409 response returns the current page so the
caller can re-merge.
EOF
      ;;
    list-pages) cat <<'EOF'
usage: jarvis-tools list-pages [--type TYPE] [--all] [--stale-days N]
                              [--over-limit]
List long-term fact page indexes. TYPE is principal, person, project,
key_matter, group or resource. Default is active entities only; --all includes
inactive ones. --stale-days and --over-limit are inspection filters.
EOF
      ;;
    list-backlinks) cat <<'EOF'
usage: jarvis-tools list-backlinks --type TYPE --id N
List pages that reference this entity. TYPE is principal, person, project,
key_matter, group or resource.
EOF
      ;;
    get-agent-identity) cat <<'EOF'
usage: jarvis-tools get-agent-identity
Return display_name and principal_open_id for this running instance (no secrets).
This is the configured agent identity, not the editable principal profile.
EOF
      ;;
    *) return 1 ;;
  esac
  case "$1" in
    archive-project)
      printf '\n--id is the numeric Project database id. Returns {id,archived:true}.\nExample: jarvis-tools archive-project --id 7\nRead back: jarvis-tools get-project --id 7 (status=archived).\n' ;;
    close-key-matter)
      printf '\n--id is the numeric KeyMatter database id. Returns {id,closed:true}.\nExample: jarvis-tools close-key-matter --id 7\nRead back: jarvis-tools get-key-matter --id 7; list-key-matters --all includes it.\n' ;;
    touch-key-matter|touch-resource)
      printf '\n--id is the numeric entity database id. Returns the saved entity with last_active_at.\nExample: jarvis-tools %s --id 7\nRead back with the corresponding get-key-matter/get-resource --id 7.\n' "$1" ;;
    delete-person|delete-resource)
      printf '\n--id is the numeric database id, not a Feishu/captured-resource id.\nReturns {id,deleted:true}; later exact get fails as not found.\nExample: jarvis-tools %s --id 7\n' "$1" ;;
    update-page)
      printf '\n--id is the numeric entity database id. Returns the saved page with updated_at.\nRead back: jarvis-tools get-page --type project --id 7\nExample: jarvis-tools update-page --type project --id 7 --content "Current facts" --if-unchanged-since TIMESTAMP_FROM_GET_PAGE\n' ;;
    list-projects|list-persons|list-key-matters|list-groups|query-resources)
      printf '\nOne server-filtered page only: --page defaults to 1, --limit to 20 (max 100).\nReturns total/page/page_size and projects/persons/key_matters/items/resources respectively.\n' ;;
  esac
}

world_payload_intro() {
  local cmd="$1" id_flag=""
  case "$cmd" in update-principal|create-*) ;; *) id_flag=" --id ID" ;; esac
  printf 'usage: jarvis-tools %s%s [--payload JSON|-]\n' "$cmd" "$id_flag"
  cat <<'EOF'
Payload must be a JSON object; - or omission reads stdin.
Updates REPLACE editable control fields, not PATCH: read the object first and
include every editable value you want to keep. --id is a positive database row
id, not an external Feishu id. Server validates fields, enum values and links.
These payloads do not accept summary; use get-page/update-page for long-term facts.
Errors are nonzero with the raw API body on stderr; no write is retried.

EOF
}

world_flags() {
  case "$1" in
    list-relations) printf '%s' '--source-type --source-id --relation-type --target-type --target-id --node-type --node-id --node-types --limit' ;;
    create-relation) printf '%s' '--payload' ;;
    delete-relation) printf '%s' '--id' ;;
    purge-world-entities) printf '%s' '--payload' ;;
    get-world-progress) printf '%s' '--id --subject-type --subject-id --period-key' ;;
    create-world-progress) printf '%s' '--payload' ;;
    update-world-progress) printf '%s' '--id --payload' ;;
    resolve-world-node) printf '%s' '--type --id' ;;
    list-page-revisions) printf '%s' '--type --id --limit' ;;
    list-projects) printf '%s' '--keyword --limit --page --code' ;;
    get-project) printf '%s' '--id --code' ;;
    create-project) printf '%s' --payload ;;
    update-project) printf '%s' '--id --payload' ;;
    archive-project) printf '%s' --id ;;
    list-key-matters) printf '%s' '--keyword --limit --page --all' ;;
    get-key-matter) printf '%s' --id ;;
    create-key-matter) printf '%s' --payload ;;
    update-key-matter) printf '%s' '--id --payload' ;;
    touch-key-matter) printf '%s' --id ;;
    close-key-matter) printf '%s' --id ;;
    list-groups) printf '%s' '--keyword --limit --page --chat-id' ;;
    get-group) printf '%s' --chat-id ;;
    get-world-overview) printf '%s' '--section --query --offset --limit --id' ;;
    get-context) printf '%s' '--chat-id --project-id' ;;
    update-group) printf '%s' '--id --payload' ;;
    get-principal) printf '%s' '' ;;
    update-principal) printf '%s' --payload ;;
    list-persons) printf '%s' '--role --keyword --limit --page --open-id' ;;
    get-person) printf '%s' --open-id ;;
    create-person) printf '%s' --payload ;;
    update-person) printf '%s' '--id --payload' ;;
    delete-person) printf '%s' --id ;;
    query-resources) printf '%s' '--project-id --person-open-id --principal-only --keyword --limit --page' ;;
    get-resource) printf '%s' --id ;;
    create-resource) printf '%s' --payload ;;
    update-resource) printf '%s' '--id --payload' ;;
    touch-resource) printf '%s' --id ;;
    delete-resource) printf '%s' --id ;;
    get-page) printf '%s' '--type --id' ;;
    get-page-guidance) printf '%s' '' ;;
    update-page) printf '%s' '--type --id --content --if-unchanged-since' ;;
    list-pages) printf '%s' '--type --all --stale-days --over-limit --query --limit' ;;
    list-backlinks) printf '%s' '--type --id' ;;
    get-agent-identity) printf '%s' '' ;;
  esac
}

world_list_query() {
  require_limit "$SUBCOMMAND" "$LIMIT" 100
  [[ "$PAGE" =~ ^[1-9][0-9]*$ ]] || fail "--page must be positive"
  printf 'page=%s&page_size=%s' "$PAGE" "$LIMIT"
  if [[ -n "$KEYWORD" ]]; then
    printf '&keyword=%s' "$(jq -rn --arg s "$KEYWORD" '$s|@uri')"
  fi
}

# The server applies the exact filter and reports uniqueness across all pages.
world_get_exact() {
  local path="$1" field="$2" value="$3" extra="${4:-}" body total
  body="$(api_get "${path}?${field}=$(jq -rn --arg s "$value" '$s|@uri')&page=1&page_size=1${extra}")"
  total="$(printf '%s' "$body" | jq -er '.data.total')"
  [[ "$total" == 0 ]] && fail "not found"
  [[ "$total" == 1 ]] || fail "multiple matches for ${field}=${value}: ${total}"
  printf '%s' "$body" | jq -e --arg field "$field" --arg value "$value" '.data.items | length == 1 and .[0][$field] == $value' >/dev/null ||
    fail "invalid exact lookup response for ${field}=${value}"
  printf '%s' "$body" | json_data data items 0
}

cmd_list_projects() {
  local query body
  query="$(world_list_query)"
  [[ -z "$CODE" ]] || query="${query}&code=$(jq -rn --arg s "$CODE" '$s|@uri')"
  body="$(api_get "/api/projects?${query}")"
  printf '%s' "$body" | json_data --project-items projects 'total,page,page_size' 'id,code,name,role,status,priority,description,updated_at'
}

cmd_get_project() {
  local has_id=0 has_code=0
  [[ -n "$ID" ]] && has_id=1
  [[ -n "$CODE" ]] && has_code=1
  (( has_id + has_code == 1 )) || fail "get-project requires exactly one of --id or --code"
  if (( has_id )); then
    [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-project --id must be a positive integer"
    emit_api_data "/api/projects/${ID}"
  else
    world_get_exact /api/projects code "$CODE"
  fi
}

cmd_create_project() { cmd_payload_create create-project POST /api/projects; }

cmd_update_project() { cmd_payload_by_id update-project PUT /api/projects; }

cmd_archive_project() { cmd_delete_by_id archive-project /api/projects; }

cmd_list_key_matters() {
  local query body
  query="$(world_list_query)"
  [[ "$ALL" != true ]] || query="${query}&include_closed=true"
  body="$(api_get "/api/key-matters?${query}")"
  printf '%s' "$body" | json_data --project-items key_matters 'total,page,page_size' 'id,title,status,summary,project_id,due_at,last_progress_at,last_active_at'
}

cmd_get_key_matter() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-key-matter requires positive --id"
  local body
  body="$(api_get "/api/key-matters/${ID}")"
  printf '%s' "$body" | json_data
}

cmd_create_key_matter() { cmd_payload_create create-key-matter POST /api/key-matters; }

cmd_update_key_matter() { cmd_payload_by_id update-key-matter PUT /api/key-matters; }

cmd_touch_key_matter() { cmd_touch_by_id touch-key-matter /api/key-matters; }

cmd_close_key_matter() { cmd_delete_by_id close-key-matter /api/key-matters; }

cmd_list_groups() {
  require_limit list-groups "$LIMIT" 100
  local query
  query="$(world_list_query)"
  [[ -z "$CHAT_ID" ]] || query="${query}&chat_id=$(jq -rn --arg s "$CHAT_ID" '$s|@uri')"
  local body
  body="$(api_get "/api/groups?${query}")"
  printf '%s' "$body" | jq -c '.data | {
    total,page,page_size,broadened,
    items:[.items[] | {id,chat_id,name,description,project_id,include_in_memory,project:(if .project == null then null else {id:.project.id,code:.project.code,name:.project.name} end),tier,pinned,is_key_group,related_group,last_active_at,message_count}]
  }'
}

cmd_get_group() {
  [[ -n "$CHAT_ID" ]] || fail "get-group requires --chat-id"
  world_get_exact /api/groups chat_id "$CHAT_ID" '&related_only=false'
}

cmd_get_context() {
  if [[ -n "$PROJECT_ID" && ! "$PROJECT_ID" =~ ^[1-9][0-9]*$ ]]; then
    fail "get-context --project-id must be a positive integer"
  fi
  local payload body identity_body identity
  payload="$(jq -nc --arg chat_id "$CHAT_ID" --arg project_id "$PROJECT_ID" '{
    chat_id: $chat_id,
    project_id: (if $project_id == "" then null else ($project_id | tonumber) end)
  }')"
  body="$(api_write POST /api/context "$payload")"
  identity_body="$(api_get /api/agent-identity)"
  identity="$(printf '%s' "$identity_body" | json_data)"
  printf '%s' "$body" | json_data | json_data --set agent_identity "$identity"
}

cmd_update_group() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "update-group requires positive --id"
  local payload
  payload="$(read_payload_object update-group)"
  forbid_summary_payload update-group "$payload"
  require_complete_group_payload update-group "$payload"
  api_write PUT "/api/groups/${ID}" "$payload" | json_data
}

cmd_get_principal() {
  local body
  body="$(api_get /api/profile)"
  printf '%s' "$body" | json_data
}

cmd_update_principal() { cmd_payload_create update-principal PUT /api/profile; }

cmd_list_persons() {
  if [[ -n "$ROLE" && ! "$ROLE" =~ ^(leader|key|colleague|other)$ ]]; then
    fail "list-persons --role must be leader, key, colleague or other"
  fi
  local query body
  query="$(world_list_query)"
  [[ -z "$ROLE" ]] || query="${query}&role=$(jq -rn --arg s "$ROLE" '$s|@uri')"
  [[ -z "$OPEN_ID" ]] || query="${query}&open_id=$(jq -rn --arg s "$OPEN_ID" '$s|@uri')"
  body="$(api_get "/api/persons?${query}")"
  printf '%s' "$body" | json_data --project-items persons 'total,page,page_size' 'id,open_id,name,en_name,department,title,role,is_active,updated_at'
}

cmd_get_person() {
  [[ -n "$OPEN_ID" ]] || fail "get-person requires --open-id"
  world_get_exact /api/persons open_id "$OPEN_ID"
}

cmd_create_person() { cmd_payload_create create-person POST /api/persons; }

cmd_update_person() { cmd_payload_by_id update-person PUT /api/persons; }

cmd_delete_person() { cmd_delete_by_id delete-person /api/persons; }

cmd_query_resources() {
  if [[ -n "$PROJECT_ID" && ! "$PROJECT_ID" =~ ^[1-9][0-9]*$ ]]; then
    fail "query-resources --project-id must be a positive integer"
  fi
  local query body resources metadata
  query="$(world_list_query)&active_only=true"
  [[ -z "$PROJECT_ID" ]] || query="${query}&project_id=${PROJECT_ID}"
  [[ -z "$PERSON_OPEN_ID" ]] || query="${query}&person_open_id=$(jq -rn --arg s "$PERSON_OPEN_ID" '$s|@uri')"
  [[ "$PRINCIPAL_ONLY" != true ]] || query="${query}&principal_only=true"
  body="$(api_get "/api/resources?${query}")"
  resources="$(printf '%s' "$body" | json_data data items)"
  metadata="$(printf '%s' "$body" | jq -c '.data | {total,page,page_size,count:(.items|length)}')"
  printf '%s,"resources":%s}\n' "${metadata%\}}" "$resources"
}

cmd_get_resource() {
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-resource requires positive --id"
  api_get "/api/resources/${ID}" | json_data
}

cmd_create_resource() { cmd_payload_create create-resource POST /api/resources; }

cmd_update_resource() { cmd_payload_by_id update-resource PUT /api/resources; }

cmd_touch_resource() { cmd_touch_by_id touch-resource /api/resources; }

cmd_delete_resource() { cmd_delete_by_id delete-resource /api/resources; }

cmd_get_page() {
  require_page_type get-page "$TYPE"
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-page requires a positive --id"
  local body
  body="$(api_get "/api/pages/${TYPE}/${ID}")"
  printf '%s' "$body" | json_data
}

cmd_get_page_guidance() {
  emit_api_data /api/text-files/entity_page_guidance
}

cmd_update_page() {
  require_page_type update-page "$TYPE"
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "update-page requires a positive --id"
  [[ -n "$IF_UNCHANGED_SINCE" ]] || fail "update-page requires --if-unchanged-since"
  local content
  if [[ "$CONTENT" == "-" ]]; then
    content="$(cat)"
  else
    content="$CONTENT"
  fi
  content="${content%$'\n'}"
  local payload body
  payload="$(jq -cn --arg content "$content" --arg if_unchanged_since "$IF_UNCHANGED_SINCE" \
    '{content:$content,if_unchanged_since:$if_unchanged_since}')"
  body="$(api_write PUT "/api/pages/${TYPE}/${ID}" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_list_pages() {
  [[ "$LIMIT_EXPLICIT" == true ]] || LIMIT="100"
  require_limit list-pages "$LIMIT" 200
  if [[ -n "$TYPE" ]]; then
    require_page_type list-pages "$TYPE"
  fi
  if [[ -n "$STALE_DAYS" ]]; then
    [[ "$STALE_DAYS" =~ ^[1-9][0-9]*$ ]] || fail "list-pages --stale-days must be a positive integer"
  fi
  local query="page_size=${LIMIT}" value body cursor="" items_file items
  items_file="$(mktemp "${TMPDIR:-/tmp}/jarvis-pages.XXXXXX")"
  if [[ -n "$TYPE" ]]; then
    query="${query}&type=$(jq -rn --arg s "$TYPE" '$s|@uri')"
  fi
  if [[ "$ALL" == "true" ]]; then
    query="${query}&all=true"
  fi
  if [[ -n "$STALE_DAYS" ]]; then
    query="${query}&stale_days=${STALE_DAYS}"
  fi
  if [[ "$OVER_LIMIT" == "true" ]]; then
    query="${query}&over_limit=true"
  fi
  if [[ -n "$QUERY" ]]; then
    value="$(jq -rn --arg s "$QUERY" '$s|@uri')"
    query="${query}&q=${value}"
  fi
  while true; do
    local page_query="$query"
    if [[ -n "$cursor" ]]; then
      value="$(jq -rn --arg s "$cursor" '$s|@uri')"
      page_query="${page_query}&cursor=${value}"
    fi
    body="$(api_get "/api/pages?${page_query}")"
    printf '%s\n' "$body" | jq -c '.data.items[]' >>"$items_file"
    cursor="$(printf '%s' "$body" | jq -r '.data.next_cursor // empty')"
    [[ -n "$cursor" ]] || break
  done
  items="$(jq -cs '.' "$items_file")"
  rm -f "$items_file"
  printf '%s\n' "$items"
}

cmd_list_backlinks() {
  require_page_type list-backlinks "$TYPE"
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "list-backlinks requires a positive --id"
  local body
  body="$(api_get "/api/pages/${TYPE}/${ID}/backlinks")"
  printf '%s' "$body" | json_data
}

cmd_get_agent_identity() { emit_api_data /api/agent-identity; }


require_page_type() {
  local cmd="$1" value="$2"
  case "$value" in
    principal|person|project|key_matter|group|resource) ;;
    *) fail "${cmd} --type must be principal, person, project, key_matter, group or resource" ;;
  esac
}

cmd_payload_create() {
  local command_name="$1" method="$2" path="$3"
  local payload body
  payload="$(read_payload_object "$command_name")"
  forbid_summary_payload "$command_name" "$payload"
  body="$(api_write "$method" "$path" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_payload_by_id() {
  local command_name="$1" method="$2" path_prefix="$3"
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "${command_name} requires positive --id"
  local payload body
  payload="$(read_payload_object "$command_name")"
  forbid_summary_payload "$command_name" "$payload"
  body="$(api_write "$method" "${path_prefix}/${ID}" "$payload")"
  printf '%s' "$body" | json_data
}

cmd_delete_by_id() {
  local command_name="$1" path_prefix="$2"
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "${command_name} requires positive --id"
  local body
  body="$(api_write DELETE "${path_prefix}/${ID}" '{}')"
  printf '%s' "$body" | json_data
}

cmd_touch_by_id() {
  local command_name="$1" path_prefix="$2"
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "${command_name} requires positive --id"
  local body
  body="$(api_write POST "${path_prefix}/${ID}/touch" '{}')"
  printf '%s' "$body" | json_data
}

forbid_summary_payload() {
  local command_name="$1" payload="$2"
  if printf '%s' "$payload" | jq -e 'has("summary")' >/dev/null 2>&1; then
    fail "${command_name} does not accept summary; use update-page for long-term facts"
  fi
}

cmd_get_world_overview() {
 local query
 [[ "$LIMIT_EXPLICIT" == "true" ]] || LIMIT=0
 query="$(jq -rn --arg section "$WORLD_SECTION" --arg query "$QUERY" --arg offset "${READ_OFFSET:-0}" --arg limit "$LIMIT" --arg task_id "$ID" '"?section="+($section|@uri)+"&query="+($query|@uri)+"&offset="+$offset+"&limit="+$limit+"&task_id="+$task_id')"
 api_get "/api/world-overview${query}" | json_data
}

cmd_get_world_progress() {
  if [[ -n "$ID" ]]; then
    [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "get-world-progress --id must be a positive integer"
    [[ -z "$SUBJECT_TYPE" && -z "$SUBJECT_ID" && -z "$PERIOD_KEY" ]] ||
      fail "get-world-progress --id cannot be combined with subject-period flags"
    emit_api_data "/api/world-progress/${ID}"
    return
  fi
  [[ -n "$SUBJECT_TYPE" ]] || fail "get-world-progress requires --id or --subject-type"
  [[ -n "$SUBJECT_ID" ]] || fail "get-world-progress requires --subject-id with --subject-type"
  [[ -n "$PERIOD_KEY" ]] || fail "get-world-progress requires --period-key with --subject-type"
  local enc_type enc_id enc_period
  enc_type="$(jq -rn --arg s "$SUBJECT_TYPE" '$s|@uri')"
  enc_id="$(jq -rn --arg s "$SUBJECT_ID" '$s|@uri')"
  enc_period="$(jq -rn --arg s "$PERIOD_KEY" '$s|@uri')"
  emit_api_data "/api/world-progress?subject_type=${enc_type}&subject_id=${enc_id}&period_key=${enc_period}"
}

cmd_write_world_progress() {
  local command_name="$1" method="$2"
  local path="/api/world-progress"
  if [[ "$command_name" == "update-world-progress" ]]; then
    [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "update-world-progress requires positive --id"
    path="${path}/${ID}"
  fi
  local payload body
  payload="$(read_payload_object "$command_name")"
  body="$(api_write "$method" "$path" "$payload")"
  command -v node >/dev/null 2>&1 || fail "node is required for exact JSON evidence reads"
  printf '%s' "$body" | node "${SCRIPT_DIR}/json-api-data.mjs"
}

cmd_resolve_world_node() {
  [[ -n "$TYPE" ]] || fail "resolve-world-node requires --type"
  [[ -n "$ID" ]] || fail "resolve-world-node requires --id"
  local enc_type enc_id body
  enc_type="$(jq -rn --arg s "$TYPE" '$s|@uri')"
  enc_id="$(jq -rn --arg s "$ID" '$s|@uri')"
  body="$(api_get "/api/world-nodes/${enc_type}/${enc_id}")"
  printf '%s' "$body" | json_data
}

cmd_list_page_revisions() {
  [[ "$LIMIT_EXPLICIT" == true ]] || LIMIT="20"
  require_page_type list-page-revisions "$TYPE"
  [[ "$ID" =~ ^[1-9][0-9]*$ ]] || fail "list-page-revisions requires a positive --id"
  require_limit list-page-revisions "$LIMIT" 100
  local query="limit=${LIMIT}" body cursor="" items_file items
  items_file="$(mktemp "${TMPDIR:-/tmp}/jarvis-page-revisions.XXXXXX")"
  while true; do
    local page_query="$query"
    [[ -z "$cursor" ]] || page_query="${page_query}&cursor=${cursor}"
    body="$(api_get "/api/pages/${TYPE}/${ID}/revisions?${page_query}")"
    printf '%s\n' "$body" | jq -c '.data.items[]' >>"$items_file"
    cursor="$(printf '%s' "$body" | jq -r '.data.next_cursor // empty')"
    [[ -n "$cursor" ]] || break
  done
  items="$(jq -cs '.' "$items_file")"
  rm -f "$items_file"
  printf '%s\n' "$items"
}

cmd_list_relations() {
  [[ "$LIMIT_EXPLICIT" == true ]] || LIMIT="100"
  require_limit list-relations "$LIMIT" 200
  if [[ -n "$NODE_TYPE" || -n "$NODE_ID" ]]; then
    [[ -n "$NODE_TYPE" && -n "$NODE_ID" ]] || fail "list-relations --node-type and --node-id must be provided together"
  fi
  local query="limit=${LIMIT}" value body cursor="" items_file items
  items_file="$(mktemp "${TMPDIR:-/tmp}/jarvis-relations.XXXXXX")"
  if [[ -n "$SOURCE_TYPE" ]]; then
    value="$(jq -rn --arg s "$SOURCE_TYPE" '$s|@uri')"
    query="${query}&source_type=${value}"
  fi
  if [[ -n "$SOURCE_ID" ]]; then
    value="$(jq -rn --arg s "$SOURCE_ID" '$s|@uri')"
    query="${query}&source_id=${value}"
  fi
  if [[ -n "$RELATION_TYPE" ]]; then
    value="$(jq -rn --arg s "$RELATION_TYPE" '$s|@uri')"
    query="${query}&relation_type=${value}"
  fi
  if [[ -n "$TARGET_TYPE" ]]; then
    value="$(jq -rn --arg s "$TARGET_TYPE" '$s|@uri')"
    query="${query}&target_type=${value}"
  fi
  if [[ -n "$TARGET_ID" ]]; then
    value="$(jq -rn --arg s "$TARGET_ID" '$s|@uri')"
    query="${query}&target_id=${value}"
  fi
  if [[ -n "$NODE_TYPE" ]]; then
    value="$(jq -rn --arg s "$NODE_TYPE" '$s|@uri')"
    query="${query}&node_type=${value}"
    value="$(jq -rn --arg s "$NODE_ID" '$s|@uri')"
    query="${query}&node_id=${value}"
  fi
  if [[ -n "$NODE_TYPES" ]]; then
    value="$(jq -rn --arg s "$NODE_TYPES" '$s|@uri')"
    query="${query}&node_types=${value}"
  fi
  while true; do
    local page_query="$query"
    if [[ -n "$cursor" ]]; then
      page_query="${page_query}&cursor=${cursor}"
    fi
    body="$(api_get "/api/relations?${page_query}")"
    printf '%s\n' "$body" | jq -c '.data.items[]' >>"$items_file"
    cursor="$(printf '%s' "$body" | jq -r '.data.next_cursor // empty')"
    [[ -n "$cursor" ]] || break
  done
  items="$(jq -cs '.' "$items_file")"
  rm -f "$items_file"
  printf '%s\n' "$items"
}

cmd_create_relation() { cmd_payload_create create-relation POST /api/relations; }
cmd_delete_relation() { cmd_delete_by_id delete-relation /api/relations; }
cmd_purge_world_entities() { cmd_payload_create purge-world-entities POST /api/world/purge; }
cmd_create_world_progress() { cmd_write_world_progress create-world-progress POST; }
cmd_update_world_progress() { cmd_write_world_progress update-world-progress PUT; }

require_complete_group_payload() {
  local command_name="$1" payload="$2"
  if ! printf '%s' "$payload" | jq -e '
    has("project_id") and (.project_id == null or ((.project_id | type) == "number" and .project_id > 0 and .project_id == (.project_id | floor))) and
    has("related_group") and ((.related_group | type) == "boolean") and
    has("pinned") and ((.pinned | type) == "boolean") and
    has("include_in_memory") and ((.include_in_memory | type) == "boolean") and
    has("is_key_group") and ((.is_key_group | type) == "boolean")
  ' >/dev/null 2>&1; then
    fail "${command_name} replaces the complete group control state; first use get-group, then provide project_id (positive integer or null), related_group, pinned, include_in_memory and is_key_group (booleans)"
  fi
}
