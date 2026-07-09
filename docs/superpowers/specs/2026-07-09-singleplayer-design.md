# goscape-singleplayer — Design (rev-274)

**Date:** 2026-07-09
**Status:** Approved
**Scope:** rev-274 only. Other revisions follow once the shape is proven.

## Goal

One binary that gives a player a complete singleplayer game: it boots the full
goscape server stack in-process, then runs the goscape-client render loop in
the same process against it. Close the window, relaunch later, and your
character is where you left it.

## Decisions already made

| Decision | Choice | Rationale |
|---|---|---|
| Repo strategy | Third repo `goscape-singleplayer` importing both modules | No history merge, no CGO pollution of the server repo, provenance policies stay separate |
| Transport | Loopback TCP for everything | Zero transport changes in either repo; every client network path (game socket, ondemand-over-game-socket, HTTP cache, JAGGRAB fallback) works exactly as in multiplayer |
| Revisions | rev-274 first | Prove the shape once, then replicate per the established cross-rev methodology |
| Login UX | Standard login screen | `login.auto-register` defaults to `true` (`modules/login/config.go:42`), so first login creates the account; zero client divergence |

`net.Pipe`/bufconn transports were evaluated and deferred: the client's HTTP
cache path has no injection seam (`client.go:7241`, `signlink.go:215` use
`http.Get` directly), the world binds TCP inside `Run()` regardless, and
`net.Pipe`'s unbuffered synchronous writes are a timing regime the
`ClientStream` `available()` emulation was never exercised under. The server's
`world.Server.HandleConn` seam (`modules/world/conn_handler.go:13`) remains
available if this is ever revisited.

## Repo layout

```
goscape-singleplayer/
  README.md                      (main branch)
  go.mod                         module github.com/zsrv/goscape-singleplayer, go 1.26
  cmd/goscape-singleplayer/      main: flags, server boot, readiness, client launch
  internal/server/               server-stack bootstrap (config build, run, stop)
  docs/superpowers/specs/        this document
```

`main` holds only the README; code lives on `rev-274` (and later, the other
four revision branches, each pairing with the same-named goscape and
goscape-client branches).

### Dependency wiring

`go.mod` requires `github.com/zsrv/goscape` and `github.com/zsrv/goscape-client`
with `replace` directives to the sibling checkouts (`../goscape`,
`../goscape-client`). goscape has no git remote, so module-proxy resolution is
impossible; replace-to-sibling is the working model. Consequence, documented in
the README: building a singleplayer revision branch requires the sibling
checkouts to be on the matching revision branch.

The binary needs `CGO_ENABLED=1` (GLFW/OpenGL via the client).

## Server bootstrap (one goscape change — Amendment 1)

**Amendment 1 (2026-07-09, user-approved):** Task 4's boot test surfaced that
`encfilter.Load()` reads the chat word-filter from a hardcoded cwd-relative
`data/raw/wordenc` (TS-faithful, `WordEnc.ts:35-37`), which breaks the
embedded server outside the goscape checkout. Fix: goscape rev-274 gains an
optional `world.wordenc_path` config knob whose default remains the
TS-faithful hardcoded path (same pattern as `world.rsa_private_key_path`);
the singleplayer bootstrap sets it absolute, defaulting to
`<cache-dir>/../raw/wordenc` (goscape's repo layout) with a `--wordenc-path`
override and a fail-fast existence check.

`internal/server` builds `app.Config` programmatically — start from
`app.NewDefaultConfig()` (`cmd/goscape/app/config.go:30`), then override:

- `Target: all` (world + login + friends + ondemand + sqlite database module)
- All listen addresses `127.0.0.1`; default ports 43594 (world TCP), 8080
  (ondemand HTTP), 2004/2005 (login/friends gRPC)
- sqlite DSN, player save path, and world/ondemand `cache_path` rooted under
  the data/cache directories resolved from flags (absolute paths — server
  defaults are cwd-relative)
- `ondemand.node-port` kept equal to `world.tcp-listen-port`

Run `app.New(logger, cfg)` / `(*App).Run()` (`cmd/goscape/app/app.go:58,110`)
on a goroutine. `app` never calls `os.Exit`; DB migrations run via the
database module anchor. `App.Run` installs the process signal handler — in
singleplayer that is the desired owner (see Lifecycle).

**Readiness:** poll `GET http://127.0.0.1:<ondemand-port>/crc` until it
answers (bounded, ~10s) before launching the client. The client fetches CRCs
at boot, so this is exactly the dependency being guarded, and it needs no
server-side change.

## Client launch (two goscape-client changes, rev-274)

Both are structural extractions with no behavior change, consistent with the
faithful-to-Java policy:

1. **Extract the launch wiring.** The ~150 lines in `cmd/client/main.go`
   (signlink/clientextras globals, `ConfigureTransport`, `StartPriv`, audio
   start, `platform.Main` + `RunShell` loop) move to an exported package —
   `pkg/jagex2/launch` — with an `Options` struct (node id, mem profile,
   members flag, store id, world host/transport/port, ondemand base URL).
   `cmd/client/main.go` becomes flag parsing plus one `launch.Run(opts)` call.
   Stock client behavior is byte-identical on the success path; the release
   banner moves inside `launch.Run`, so a flag-validation failure now prints
   only the error (previously banner then error). Accepted: flags are a
   Go-original surface with no Java-parity impact.

2. **Exit hook.** `(*Client).Shutdown` (`pkg/jagex2/client/gameshell.go:32`)
   and the post-`RunShell` exit call `os.Exit` directly. Replace with an
   exported hook (`var ExitFunc = os.Exit` in `clientextras` — package
   `client` must reach it, so the launch package would be an import cycle;
   `clientextras` is the designated cycle-breaker). Default behavior
   unchanged.

The singleplayer main calls `launch.Run` with loopback options and `ExitFunc`
overridden to perform graceful server shutdown first.

## Lifecycle

- **Startup:** parse flags → validate cache dir → start server goroutine →
  poll readiness → `launch.Run` (blocks on the render loop; main goroutine is
  consumed by `platform.Main`'s `LockOSThread`).
- **Window close:** exit hook fires → `app.Stop()`
  (`cmd/goscape/app/app.go:188`) → wait for `Run()` to return (bounded ~10s)
  so player saves and sqlite flush → `os.Exit(0)`.
- **SIGINT/SIGTERM:** `App.Run`'s signal handler stops the services; the
  watcher goroutine sees `Run()` return and calls `os.Exit` (the stock client
  has no graceful-stop concept — its own exit path is `os.Exit` anyway).
- **Server failure:** if `Run()` returns an error before readiness, print it
  and exit non-zero without launching the client; if it fails while the client
  runs, log and exit non-zero rather than leaving a client connected to
  nothing.

Guard `app.Stop()` against the not-running state (it panics otherwise) and
make the exit path idempotent (close-then-signal must not double-stop).

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--data-dir` | `./data` | sqlite DB, player saves |
| `--cache-dir` | `./data/pack` | packed game cache (server-side; consumed by world + ondemand) |
| `--world-port` | `43594` | world TCP listen port |
| `--ondemand-port` | `8080` | ondemand HTTP listen port |
| `--login-port` / `--friends-port` | `2004` / `2005` | internal gRPC ports |
| `--wordenc-path` | derived: `<cache-dir>/../raw/wordenc` | raw wordenc jagfile for the chat filter (Amendment 1) |
| `--mem` | `high` | client memory profile pass-through |
| `--world-type` | `members` | client free/members pass-through |

Fail fast with a clear message when the packed cache is missing at the
resolved path (the expected first-run mistake — the repo ships no cache; users
produce one with goscape's `make pack`) or when a port is already bound.

The client's own disk cache (`.file_store_32` via `signlink.FindCacheDir`)
stays stock.

## Verification

- Compile gate: `go build ./...` with CGO enabled.
- Automated smoke: a Go test drives `internal/server` (config build → run →
  readiness poll → stop) with a temp data dir — no GUI, no display needed.
  Requires a packed cache fixture path; skip with a clear message when absent.
- Full end-to-end (login, move around, close window, relaunch, character
  persisted) is a user-launched smoke test.

## Out of scope (v1)

- `net.Pipe`/bufconn in-process transport
- Auto-login / skipping the title screen
- rev-254/245.2/244/225 branches
- Packaging (icons, installers, embedding the cache in the binary)
- Cache packing — the binary consumes an already-packed cache
