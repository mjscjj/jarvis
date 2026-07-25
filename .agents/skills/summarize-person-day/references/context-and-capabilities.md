# Principal Context and Evidence Capability Map

## Contents

- Principal posture
- Known business background
- Tool selection order
- Jarvis capability map
- lark-cli capability map
- bytedcli capability map
- Local engineering capability map
- Live discovery commands

## Principal Posture

Act as the principal's digital extension, not a neutral reporting bot.

- Optimize for the principal's goals, interests, judgment preferences, and
  attention.
- Start from the current Jarvis seed and inspect only evidence paths that can
  affect the day's report.
- Use tools to resolve facts instead of asking the principal to fill a form.
- Follow multi-hop relationships: group → project → people → document → Task →
  repository → MR → deployment.
- Interrupt the principal only for an actual choice, intent, or unavailable
  private fact.
- Surface low-cost, relevant risks, blockers, mentions, commitments, and
  changes the principal may not have noticed.

This reference is derived from `conf/rules/all.md`. Read the live rule file when
the repository version may have changed.

## Known Business Background

- Service focus: overseas i18n control plane.
- Principal assistant: Jarvis, running in a trusted local environment.
- Key control-plane, gateway, and admin domains: not yet recorded in
  `conf/rules/all.md`; do not invent them.
- Additional domain terms and key systems: not yet recorded; discover from
  projects, groups, documents, repositories, and current evidence.

Background guides relevance and search. It is never evidence that a same-day
change occurred.

## Tool Selection Order

1. Use deterministic Jarvis data and durable bindings first.
2. Use lark-cli for Feishu messages, people, meetings, documents, tasks, and
   related personal work evidence.
3. Use bytedcli for ByteDance engineering, knowledge, delivery, deployment,
   runtime, data, and issue evidence.
4. Use Git and local Codex/run artifacts for exact local engineering state.
5. Use a matching installed Skill before guessing CLI parameters.

Prefer read-only commands. This daily-summary skill does not authorize external
writes or messages.

This map is a router, not a collection checklist. Pick the smallest matching
surface for a current Seed ID or bounded same-day discovery; do not enumerate
every tool domain.

## Jarvis Capability Map

Use `jarvis-tools` and the local database for:

- principal, colleague, leader, and relationship facts;
- active projects, goals, progress events, and background;
- group/project and repository/project bindings;
- Todo, Task, TaskEvent, ExecutionRun, and Agent Session relations;
- frozen context snapshots;
- meeting ingestion state and local transcript paths;
- relevant history and outstanding commitments.

Use Jarvis as the deterministic Seed and relationship layer. Do not replace
an external terminal state with a Jarvis summary when that external state is
already part of the current collector's assigned scope.

## lark-cli Capability Map

The current lark-cli exposes version-matched skills through
`lark-cli skills list` and `lark-cli skills read`.

### Identity and people

- `lark-shared`: auth status, user/bot identity, missing scopes.
- `lark-contact`: resolve names, email, open_id, department, and status.
- `lark-attendance`: the principal's attendance records when relevant.

### Communication

- `lark-im`: messages, replies, threads, mentions, chat membership, reactions,
  files, and search.
- `lark-mail`: mail, threads, drafts, and rules when mail evidence is relevant.
- `lark-event`: bounded real-time event consumption; use only when the report
  explicitly needs live events.

### Calendar and meetings

- `lark-calendar`: schedules, attendees, availability, and meeting discovery.
- `lark-vc`: ended meetings, meeting detail, participants, summaries, action
  items, transcript, and recording metadata.
- `lark-minutes`: search and read Minutes artifacts.
- `lark-note`: read a known Note and its unified transcript.
- `lark-vc-agent`: in-meeting events; normally outside a finished-day report.

### Documents and knowledge assets

- `lark-doc`: Docx and Wiki document content and mindnotes.
- `lark-wiki`: knowledge-space nodes and hierarchy.
- `lark-drive`: resource discovery, metadata, versions, comments, permissions,
  and file operations.
- `lark-markdown`: native Markdown files.
- `lark-sheets`, `lark-base`, `lark-slides`, `lark-whiteboard`: structured or
  visual document evidence.

### Work, decisions, and goals

- `lark-task`: tasks, task lists, assignees, state, comments, and attachments.
- `lark-approval`: approval definitions, instances, status, and actions.
- `lark-okr`: objectives, key results, alignments, and progress.

### Discovery beyond registered commands

- `lark-openapi-explorer`: find a native Feishu OpenAPI only when installed
  lark skills and registered commands cannot satisfy a material evidence query.

For personal resources, prefer `--as user`; bot identity often cannot see the
principal's calendar, drive, mail, or other private resources. Check identity
with:

```bash
lark-cli auth status --json --verify
lark-cli whoami
```

Treat `ok == true` or process exit code 0 as success. Do not test for a
top-level `code == 0`.

## bytedcli Capability Map

Use `bytedcli --json ...` for stable machine-readable results. Put `--json`
before the command domain.

### Code, review, and delivery

- `codebase`: repositories, files, commits, branches, MR/CR, review, issues,
  CI, and cross-repository search.
- `code-review`: repository-specific review rules and workflows.
- `bits`: engineering tasks, pipelines, MR, release, and Anywheredoor evidence.
- `devflow`: projects, requirements, release tasks, and deployment state.
- `ftf`, `tesla`, `smartq`, `test-plan`, `codecov`: test execution and quality
  evidence.

### Internal knowledge and decisions

- `insearch`: internal search, Feishu/ByteCloud documents, ByteTech, BitsAI,
  and authenticated read-only internal URLs.
- `deepwiki`, `bitsai`, `ai-dev-pro afs`, `tika`, `aime`: code knowledge,
  internal knowledge, and AI-assisted retrieval.
- `cloud-docs`, `meego`, `cloud-ticket`, `kani`, `lark-oncall`: documents,
  work items, tickets, approvals, and operational collaboration.

### Deployment, configuration, and service state

- `tce`, `nexde`, `lark-devops`, `release-manager`, `env`, `clouddev`,
  `byteflow`, `tcc`, `bytetree`, `sd`: deployment, environments, workflows,
  configuration, service tree, and discovery.
- `log`, `apm`, `slardar`, `kelemetry`, `rca`, `sip`, `primus`, `vela`:
  logs, metrics, traces, alerts, incidents, and runtime verification.

### Data and experiment evidence

- `aeolus`, `tea`, `libra`: dashboards, behavioral analytics, and experiments.
- `rds`, `db`, `hive`, `bytehouse`, `doris-ops`, `tqs`, `dorado`: databases,
  data assets, queries, and task state.

### People and organizational context

- `lark`: Feishu commands routed through bytedcli when direct lark-cli is not
  the appropriate entry.
- `people`, `hire`: personal and organizational records when materially
  relevant and authorized.

### Site and network context

Use `--site` or `BYTEDCLI_CLOUD_SITE` for `cn`, `i18n-bd`, `i18n-tt`, BOE, US,
or EU surfaces. On production development machines, i18n/SG commands may also
require `BYTEDCLI_NETWORK_PROFILE=prod`. Verify the current environment rather
than guessing.

## Local Engineering Capability Map

Use:

- `git` for exact local commits, refs, diffs, worktrees, and author evidence;
- local Codex session index and run artifacts for delegated Agent work;
- repository tests and build logs for validation evidence;
- deployment and service logs only when linked to the day's work.

Local branch presence or a commit subject does not prove remote merge,
deployment, or runtime effect.

## Live Discovery Commands

Refresh capability knowledge instead of depending on stale examples:

```bash
lark-cli skills list
lark-cli skills read <lark-skill-name>
lark-cli schema <service.resource.method>

bytedcli --help
bytedcli --json --all-help
bytedcli <domain> --help
bytedcli --json auth status
```

When a URL is available, prefer passing the URL directly if the command supports
it. Read the current domain Skill or leaf `--help`; never guess parameter names.
