# goscape-singleplayer — In-process transports (rev-274)

**Date:** 2026-09-08
**Status:** Approved
**Scope:** rev-274 first, then backported to rev-254, rev-245.2, rev-244,
rev-225. Requires additive changes to **goscape** and **goscape-client**;
both keep their current TCP behaviour when the new fields are unset.

## Goal

A singleplayer binary that opens no sockets. Today four loopback TCP
listeners carry traffic that never leaves the process:

| Port | Module | Who talks | Carries |
|---|---|---|---|
| 43594 | world | client → world | raw game protocol |
| 8080 | ondemand | client → ondemand | `/crc`, cache archives, SoundFont |
| 2004 | login | world → login | gRPC, incl. player save/load |
| 2005 | friends | world → friends | gRPC, incl. world-events stream |

Account and hiscore are in goscape's `SingleBinary` dependency set but both
default to `Enable=false`, so there is no fifth listener.

After this change all four run over in-memory connections. `--expose-tcp`
(default off) restores the old behaviour for debugging.

## Decisions already made

| Decision | Choice | Rationale |
|---|---|---|
| Transport substitution | Inject a pre-made `net.Listener`; keep every protocol | gRPC still marshals protobuf, ondemand still parses HTTP, world still runs its accept loop — singleplayer keeps exercising the code multiplayer runs |
| In-memory conn | `google.golang.org/grpc/test/bufconn` | Bounded ring buffer with real deadline support (`bufconn.go:277`/`294`); already a transitive dependency; TCP-like blocking semantics |
| World's seam | Listener injection, **not** `HandleConn` | Keeps `serveTCP`'s admission gate, `tcpWg` tracking and graceful shutdown on the production path; needs no accessor for `app.App`'s unexported `world` field |
| Escape hatch | `--expose-tcp`, default off | Keeps a second client, `tcpdump` and `curl` available when something goes wrong |
| Upstream posture | Additive; TCP remains the default | goscape is a real distributed stack; multiplayer must be untouched |
| Branch order | rev-274, then backport | CONTRIBUTING: "fix it on the newest affected branch first" |

### Rejected: protocol bypass

Calling login/friends handlers directly through the existing `LoginClient` /
`FriendsClient` interfaces, serving ondemand through an in-process
`http.RoundTripper` that invokes `HTTPServer.Handler` with no HTTP parsing,
and using a pipe conn only for the game protocol.

Rejected on fidelity and surface area:

- Three mechanisms instead of one.
- `modules/login`'s `handler` is unexported and constructed inside
  `newGRPCServer`; a direct path needs new exported surface upstream.
- The friends bridge consumes a **streaming** RPC via
  `newWorldEventsSubscriber`. There is no cheap direct equivalent.
- Singleplayer would stop exercising the real login and HTTP paths — the two
  deployments could then diverge silently, which defeats the point of running
  the actual server stack in one process.

The saving is protobuf marshalling and HTTP parsing on a stack that boots in
600 ms. Not worth it.

### Rejected: unix domain sockets, and `:0`

Both were considered as cheaper stopping points and both were declined
because they do not meet the goal — they remove *ports*, not *sockets*.
Recorded here because they remain the right answer to adjacent problems:

- **Unix sockets.** `world.tcp-listen-network` and ondemand's
  `http_listen_network` are already config knobs and gRPC dials `unix://`
  natively, but `net.JoinHostPort` mangles a socket path in both
  `world/server.go:805` and `dskit/server.go:96`. Adds socket-file lifecycle.
- **Bind `:0`.** Solves instance collisions and the 8080 conflict for a
  fraction of the work; `world/config.go:138` currently rejects port 0.

## Architecture

### goscape — six new fields, no behaviour change when unset

| Site | Change |
|---|---|
| `pkg/dskit/server/server.go:96` | `Config.Listener net.Listener` (`yaml:"-"`); `newServer` adopts it or binds |
| `modules/login/server.go:46` | `Config.Listener`; `grpcServer.listen` returns it when set |
| `modules/friends/server.go:77` | `Config.Listener`; same |
| `modules/world/server.go:805` | `Config.Listener`; `Server.Listen()` adopts it instead of binding |
| `modules/world/login_client.go:38` | `Config.LoginServerDialer func(context.Context, string) (net.Conn, error)` → `grpc.WithContextDialer` |
| `modules/world/friends_client.go:87` | `Config.FriendsServerDialer`, same |

Three validation relaxations — an injected listener makes the port
meaningless, so each module's `[1, 65535]` check is skipped when one is set:
`world/config.go:138`, `login/config.go:72` and `friends/config.go:39`.

The port fields themselves stay assigned even in fabric mode. `OnDemand.Port`
is not the HTTP port but `ondemand.node-port` — the *world* port `/rs2.cgi`
uses to emit `portoff = node-port - 43594` — and `app/config.go:117` warns
when it disagrees with `World.TCPListenPort`. Keeping both assigned keeps that
check quiet and makes fabric mode purely additive.

Nothing downstream of the bind site changes. `world/server.go:926` spawns
`serveTCP()`, which reads only `s.tcpListener`; login (`login.go:82`) and
friends (`friends.go:53`) both take the listener in their `starting` phase and
serve it in `running`. All four already operate on a `net.Listener` interface,
and `bufconn` is one.

### goscape-client — two hooks

- `clientextras`: add `TransportInProc` to the transport enum, plus
  `DialInProc func(port int) (net.Conn, error)` and
  `HTTPClient *http.Client` (defaults to `http.DefaultClient`).
  The hook takes the **port**, not a bare context: `OpenSocket` is called
  synchronously with a port, and the port is what lets the hook reject
  JAGGRAB's 43595 while accepting world's 43594 (see *Error handling*).
- `signlink.go:335`: a third case in `OpenSocket`'s existing switch, beside
  the TCP and WebSocket branches.
- `signlink.go:215` and `client.go:7241`: `http.Get(...)` becomes
  `clientextras.HTTPClient.Get(...)`. The URL strings, `urlBase()` and
  `codeBaseURL()` are untouched — the dialer ignores the address.
- `launch.Options`: carry both hooks through `configure`.

The fidelity note in `signlink_socket_native.go` and the Java citations at
both `http.Get` sites stay accurate: the call shape is unchanged, only the
client instance and the dialer are now injectable.

### goscape-singleplayer — the fabric

`internal/inproc` owns four named endpoints — `world`, `ondemand`, `login`,
`friends` — each a `bufconn` listener plus a matching `DialContext`.

```
Fabric
  Endpoint(name) *Endpoint
Endpoint
  Listener() net.Listener
  DialContext(ctx context.Context, network, addr string) (net.Conn, error)  // http.Transport
  DialGRPC(ctx context.Context, target string) (net.Conn, error)            // grpc.WithContextDialer
  Dial() (net.Conn, error)                                                  // clientextras.DialInProc
```

Three shapes because the three consumers disagree:
`http.Transport.DialContext` takes `(ctx, network, addr)`,
`grpc.WithContextDialer` takes `(ctx, target)`, and the client's
`OpenSocket` hook takes a port. All three ignore the address argument and
resolve to the same endpoint; they are thin adapters over one dial.

gRPC still parses its target string even when a dialer is supplied, so
world's `grpc.NewClient` calls must pass a resolvable placeholder —
`passthrough:///login` and `passthrough:///friends` — rather than the
now-meaningless `127.0.0.1:2004`.

`internal/server/config.go` hands the four listeners to the modules and the
two dialers to world's gRPC clients. `cmd/goscape-singleplayer/main.go` hands
the client its `DialInProc` and an `http.Client` whose `Transport.DialContext`
reaches the ondemand endpoint.

`--expose-tcp` selects the current path instead: real ports, no fabric. The
four port flags survive and are meaningful only in that mode.

### Buffer sizes

256 KiB for world, login and friends; 1 MiB for ondemand. Both sides of every
endpoint read continuously on their own goroutines, so a full buffer applies
backpressure rather than deadlocking — but ondemand serves multi-MB archives,
so that case is asserted by test rather than assumed.

## Data flow

Because real HTTP survives, `Server.WaitReady` keeps polling `/crc`; only the
`http.Client` it uses changes. `bufconn.Dial` blocks until `Accept` rather
than erroring, so the existing 2 s per-probe timeout and retry loop already
cover the window between listener creation and the module serving.

## Error handling

- **JAGGRAB.** `client.go:7223` opens port 43595 when the user toggles
  `JaggrabEnabled` (`client.go:2681`). The fabric dialer must **error** on an
  unknown endpoint rather than mis-route to world; the client already flips
  the toggle back on failure (`client.go:7342`).
- **Shutdown ordering.** Fabric endpoints close only after `app.Stop` has
  returned, so in-flight player saves and the sqlite flush complete.
  `Server.Stop` already waits for `Run` to return; this is ordering, not new
  machinery.
- **Module start failure.** Unchanged: a failed module still fails the app,
  and `main.go`'s `Done` watcher still exits 1. The fabric holds no state that
  needs unwinding beyond closing its listeners.

## Testing

1. **`internal/inproc`** — round-trip; unknown-endpoint dial returns an error;
   deadlines propagate; an 8 MB concurrent transfer completes without
   deadlock (the ondemand archive case).
2. **`internal/server`** — new `TestServerBootsInProcess`: boot, reach ready,
   then assert `127.0.0.1:43594`, `:8080`, `:2004` and `:2005` are all
   refused. The existing `TestServerBootsReadyAndStops` stays, now covering
   the `--expose-tcp` path.
3. **Upstream** — one test per injected site proving the module serves over a
   `bufconn` listener.
4. **Manual acceptance** — run the real window: log in, walk, confirm music
   plays (which proves the SoundFont fetch traversed in-process HTTP), close
   the window, confirm the save flushed, and confirm `ss -ltnp` showed nothing
   bound for the process throughout.

## Sequencing

| Phase | Work |
|---|---|
| 0 | Dev setup: CONTRIBUTING's local `replace` block, gitignored; no `go.work` committed |
| 1 | goscape: four listener injections, two dialer fields, validation relaxation, tests |
| 2 | goscape-client: transport kind, dial hook, HTTP client hook, `launch.Options`, tests |
| 3 | This repo: `internal/inproc`, config wiring, `main.go`, `--expose-tcp`, tests |
| 4 | End-to-end in the real window; push upstream; pin SHAs; drop `replace` |
| 5 | Backport to rev-254, rev-245.2, rev-244, rev-225 |

Phases 1 and 2 are independent and can land in either order; phase 3 needs
both. Phase 5 is not a cherry-pick: the client-side seams sit in a
per-revision port whose surrounding code differs between branches.

**The implementation plan covers phases 0–4 only** — rev-274 working end to
end. Backports get their own plan once the shape has proven itself on one
branch; writing four backports against an unproven design is how a mechanical
change becomes four divergent ones.

## Open risks

- **Backport cost.** Five branches across three repos. The upstream changes
  are small and mechanical; the client-side seams are the ones that will need
  real per-revision attention.
- **Buffer tuning.** The sizes above are reasoned, not measured. If the
  8 MB transfer test or the manual run shows stalls, they move.
