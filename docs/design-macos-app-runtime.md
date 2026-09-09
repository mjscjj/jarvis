# macOS App Runtime

## Ownership

The desktop build keeps the existing Tauri shell. Tauri owns:

- the macOS window, Dock, reopen, and quit lifecycle;
- starting one child process and reading its readiness line;
- loading the URL announced by that child.

`jarvis-app-service` owns:

- resolving `Contents/Resources/runtime`;
- synchronizing bundled runtime assets into a writable Application Support tree;
- starting and stopping Qdrant, `jarvis-server`, and the configured CC Connect;
- waiting for both health checks;
- restarting `jarvis-server` after onboarding writes `restart.requested`;
- terminating the process groups when Tauri exits.

`jarvis-server` continues to own all Jarvis HTTP APIs and the production React
site. The frontend is not rewritten in Go.

## Bundle Layout

`packaging/macos/prepare-runtime.sh` produces:

```text
runtime/
  bin/
    jarvis-app-service
    jarvis-server
    qdrant
    cc-connect-jarvis
    lark-cli
    traex
    bytedcli
    node
    jq
  conf/
  .agents/
  scripts/
  web/dist/
```

Tauri should bundle that directory at `Contents/Resources/runtime`.

The signed application bundle is immutable. On launch, the app service creates:

```text
~/Library/Application Support/Jarvis/
  runtime/
    conf/
    .agents/
    scripts/
    web/dist/
    data/
    runs/
    var/
  logs/
  cc-connect/
    config.toml
    data/
  restart.requested
  .bundle-assets.json
  app-service.pid
```

Bundled files are updated only when the installed copy still matches the
previous bundle manifest. Files changed by the user are preserved. SQLite,
runtime configuration, logs, run output, and Qdrant storage never live inside
the `.app`.

## Tauri Contract

Start:

```text
Contents/Resources/runtime/bin/jarvis-app-service
  -resources Contents/Resources/runtime
  -supervisor-pid <tauri-pid>
```

After Qdrant and Jarvis are healthy, stdout contains exactly one discovery line:

```text
JARVIS_RUNTIME_CONNECTION {"httpUrl":"http://127.0.0.1:18800","dataRoot":"..."}
```

The shell loads `httpUrl`. On `RunEvent::ExitRequested` or `RunEvent::Exit`, it
sends `SIGTERM` to the app service and waits up to 20 seconds before killing it.
The shell also exits when the app service stops, including when the Web UI calls
the system shutdown API. The app service applies graceful shutdown to its child
process groups and watches `-supervisor-pid`, so a crashed shell does not leave
services behind.

The desktop server is forced to `127.0.0.1:18800` through the `-addr` process
argument. The repository and launchd path continue to use `server.addr` from
configuration.

## First Launch

The app service creates a bootstrap runtime overlay only when one does not
exist. Expensive or externally acting schedulers start disabled, which lets the
HTTP server and Web UI open before identity and provider setup is complete.

After ByteDance SSO, the Web onboarding gate drives the following API sequence:

1. `POST /api/setup/lark/bind` validates and stores the Lark app in `lark-cli`.
2. `POST /api/setup/lark/login` starts user device authorization.
3. `POST /api/setup/agent/login` starts Trae CLI device authorization.
4. `GET /api/setup/flows/:flow_id` exposes authorization URLs, codes, live
   command output, and completion state.
5. `POST /api/setup/finalize` writes the assistant and principal identity,
   enables runtime services, writes the CC Connect config, and requests a
   `jarvis-server` restart.
6. The app service observes the CC config and starts CC Connect. It consumes the
   restart marker and restarts the server without restarting Qdrant or Tauri.
7. `POST /api/setup/world-model` creates the normal M5 bootstrap task. The gate
   opens only after that task has produced a principal profile.

The semantic setup sequence belongs to `internal/onboarding` and the bootstrap
Skill. The Go app service remains a process supervisor and only enforces process,
path, health, and restart boundaries.

CC Connect is bundled but is not started until its validated config exists.
This prevents an unconfigured process from becoming a second Lark connection
owner during first launch.

## Build

Prepare the self-contained runtime and build the Apple Silicon image:

```bash
./packaging/macos/prepare-runtime.sh desktop/src-tauri/generated/runtime
./packaging/macos/build-dmg.sh
```

The image is written to:

```text
desktop/src-tauri/target/release/bundle/dmg/Jarvis_<version>_aarch64.dmg
```

The default build uses ad-hoc signing for internal testing. External
distribution requires a Developer ID identity and Apple notarization.
