# CC Connect integration

This directory is the product-owned integration between Jarvis and CC Connect. It is independent of the Agent Skills that orchestrate installation.

- `manifest.sh`: pinned upstream repository, base commit, Jarvis version and patch identity.
- `patches/`: the auditable Jarvis patch applied to the pinned upstream checkout.
- `manage.sh`: creates or validates the `jarvis-codex` project binding. `scripts/jarvis-install` is its public orchestration entry.
- `../../scripts/install-cc-connect.sh`: builds, tests and installs `bin/cc-connect-jarvis`.

The integration has one topology rule: CC Connect is the only Feishu Bot WebSocket owner for the selected App. Explicit group `@Jarvis` messages are relayed to `/internal/interactive-task`, frozen with the latest 25 conversation messages and sent straight to M5. Messages without an explicit mention continue through Jarvis's normal M2 polling and M3 admission path.

The `jarvis-codex` Agent still loads `scripts/jarvis-tools get-context` for CC Connect turns that are not handled by the explicit-mention relay (for example, configured p2p interactions). The explicit group mention path does not run a second CC agent; M5 is its sole business executor.

Build and binding are separate operations:

```bash
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install bind-cc --profile <profile>
./scripts/jarvis-install validate-binding --profile <profile>
```

Changing the pinned upstream or patch requires updating `manifest.sh`, regenerating the patch, running the upstream Feishu tests through `scripts/install-cc-connect.sh`, and running the Jarvis test suite.
