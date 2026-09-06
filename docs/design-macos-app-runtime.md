# macOS App Runtime

## Ownership

The desktop build keeps the existing Tauri shell. Tauri owns:

- the macOS window, Dock, reopen, and quit lifecycle;
- starting one child process and reading its readiness line;
- loading the URL announced by that child.

`jarvis-app-service` owns:

- resolving `Contents/Resources/runtime`;
- synchronizing bundled runtime assets into a writable Application Support tree;
- starting and stopping Qdrant and `jarvis-server`;
- waiting for both health checks;
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

The shell loads `httpUrl`. On `WindowEvent::Destroyed` or `RunEvent::Exit`, it
sends `SIGTERM` to the app service and waits up to four seconds before killing
it. The app service applies the same graceful shutdown policy to its children.
It also watches `-supervisor-pid`, so a crashed shell does not leave services
behind.

The desktop server is forced to `127.0.0.1:18800` through the `-addr` process
argument. The repository and launchd path continue to use `server.addr` from
configuration.

## First Launch

The app service creates a bootstrap runtime overlay only when one does not
exist. Expensive or externally acting schedulers start disabled, which lets the
HTTP server and Web UI open before identity and provider setup is complete.
The future onboarding endpoint must explicitly enable the selected capabilities
after authorization.

Friday's `IdentitySetup` interaction can be reused:

- assistant name input;
- scope selection;
- provider cards and authorization polling;
- initialization progress and retry states.

Its API cannot be reused unchanged. Friday owns Genius/ACP provider state and
calls `/api/workspace/onboarding`; Jarvis owns BytedCLI SSO, lark-cli identity,
CC Connect binding, plugin authorization, and world-model bootstrap. Jarvis
needs its own `/api/setup/*` orchestration over the existing installer
operations. The Go app service must remain a process supervisor and must not
encode those semantic onboarding decisions.

CC Connect is bundled but is not started automatically by the app service.
Starting it before the user selects and validates the Lark App/Profile would
create an invalid second connection owner. The onboarding flow starts it only
after `validate-binding` succeeds.
