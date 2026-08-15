# CC Connect integration

This directory is the product-owned integration between Jarvis and CC Connect. It is independent of the Agent Skills that orchestrate installation.

- `manifest.sh`: pinned upstream repository, base commit, Jarvis version and patch identity.
- `patches/`: the auditable Jarvis patch applied to the pinned upstream checkout.
- `manage.sh`: creates or validates the `jarvis-codex` project binding. `scripts/jarvis-install` is its public orchestration entry.
- `../../scripts/install-cc-connect.sh`: builds, tests and installs `bin/cc-connect-jarvis`.

The integration has one topology rule: CC Connect is the only Feishu Bot WebSocket owner for the selected App. After its normal sender, chat and mention filters accept a message, CC Connect synchronously claims that exact `message_id` through `/internal/message-routing/claim` and then continues through its native Agent/session. The claim only prevents M3 from independently admitting the same message; it does not fetch history, create a Jarvis Task or change M2 monitoring. Group messages that CC Connect does not accept continue through Jarvis's normal M2 polling and M3 admission path.

Jarvis approval cards remain on the M2→M3→M5 Task path. CC Connect only transports their authenticated callbacks because it owns the Bot WebSocket connection.

Build and binding are separate operations:

```bash
./scripts/jarvis-install install-cc-connect
./scripts/jarvis-install bind-cc --profile <profile>
./scripts/jarvis-install validate-binding --profile <profile>
```

Changing the pinned upstream or patch requires updating `manifest.sh`, regenerating the patch, running the upstream Feishu tests through `scripts/install-cc-connect.sh`, and running the Jarvis test suite.
