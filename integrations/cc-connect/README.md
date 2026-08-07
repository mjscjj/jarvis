# CC Connect integration

This directory is the product-owned integration between Jarvis and CC Connect. It is independent of the Agent Skills that orchestrate installation.

- `manifest.sh`: pinned upstream repository, base commit, Jarvis version and patch identity.
- `patches/`: the auditable Jarvis patch applied to the pinned upstream checkout.
- `manage.sh`: creates or validates the `jarvis-codex` project binding. `scripts/jarvis-install` is its public orchestration entry.
- `../../scripts/install-cc-connect.sh`: builds, tests and installs `bin/cc-connect-jarvis`.

The integration has one topology rule: CC Connect is the only Feishu Bot WebSocket owner for the selected App. Jarvis uses the same lark-cli Profile for user reads and Bot operations, while M2 reads work messages through its normal polling path.

The `jarvis-codex` Agent must run `scripts/jarvis-tools get-context` at the beginning of each Feishu user turn. This makes CC Connect an interactive entry into Jarvis's current world model instead of a standalone Codex process in the same checkout.

Build and binding are separate operations:

```bash
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install bind-cc --profile <profile>
./scripts/jarvis-install validate-binding --profile <profile>
```

Changing the pinned upstream or patch requires updating `manifest.sh`, regenerating the patch, running the upstream Feishu tests through `scripts/install-cc-connect.sh`, and running the Jarvis test suite.
