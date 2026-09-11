# goscape-singleplayer — rev-274

A single-binary, singleplayer RuneScape: the
[goscape](https://github.com/zsrv/goscape) server stack and the
[goscape-client](https://github.com/zsrv/goscape-client) game client running in
one process, communicating over in-memory transports (or loopback TCP with
`--expose-tcp`).

No server to deploy, no configuration file, no account to register — run the
binary and you are at the login screen.

This branch targets **wire-protocol revision 274** and is pinned to the
`rev-274` branches of both upstream projects. For the other revisions and the
project overview, see the
[`main` branch README](https://github.com/zsrv/goscape-singleplayer/blob/main/README.md).

## Downloads

Released binaries for this revision embed the game content — download one,
run it, and you are at the login screen. No cache to build, no goscape
checkout, nothing else to install.

    ./goscape-singleplayer

The content is extracted once to `<data-dir>/content/` on first run; later
starts reuse it. To see which Content revision a binary carries:

    ./goscape-singleplayer -version

To ignore the embedded content and use a cache you packed yourself, pass
`--cache-dir` — it always wins over the embedded copy.

**Building from source embeds nothing.** A plain `go build` produces a binary
that needs `--cache-dir`, exactly as described below. Embedding is opt-in via
`make embed-pack && make build-embedded`.

## Requirements

- Go 1.27 or newer
- **CGO** — the client links GLFW, OpenGL and ALSA. On Debian or Ubuntu, the
  system packages are the ones in
  [goscape-client's `apt-packages.txt`](https://github.com/zsrv/goscape-client/blob/main/.devcontainer/apt-packages.txt).
- A packed revision 274 game cache, **when building from source** (see
  below). Release binaries carry one already — see [Downloads](#downloads).

## Build

```bash
CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer
```

Both upstream projects are pinned by exact version in `go.mod`, so a clone
builds without any other checkout present.

On **Windows**, prefer `make build`: it adds `-ldflags -H windowsgui`, without
which the binary is a console app and Windows opens a console window next to
the game window. See [Windows](#windows) below.

## The game cache

Building from source, you supply your own — release binaries carry one
already; see [Downloads](#downloads). In a
[goscape](https://github.com/zsrv/goscape) checkout on the `rev-274` branch,
`make pack` produces one; point `--cache-dir` at its output (default
`./data/pack`). The launcher checks for `main_file_cache.dat` there and fails
with an actionable message if the pack is missing.

The chat word filter (wordenc) is a separate raw jagfile on this revision, not
part of the pack. `--wordenc-path` defaults to `<cache-dir>/../raw/wordenc`,
which is exactly where goscape's committed `data/raw/wordenc` sits when you
pack into `data/pack`, so the default is usually right.

## Music

The client fetches its SoundFont, `SCC1_Florestan.sf2`, over HTTP from the
server this binary also runs, and plays silence if it 404s — logging a single
`soundfont unavailable` line rather than failing. A release binary carries the
SoundFont and writes it into `<data-dir>/public` on startup, so music works
with no setup. Building from source, `make embed-pack` copies it out of the
pinned goscape module the same way it copies wordenc.

Without an embedded bundle, put a copy in `<data-dir>/public` yourself —
goscape's checkout has one at `public/SCC1_Florestan.sf2`. A file already
there is never overwritten, so your own SoundFont survives upgrades.

## Run

```bash
./goscape-singleplayer --cache-dir ../goscape/data/pack
```

The world database and your character's saves live under `--data-dir`
(default `./data`). **Accounts auto-register**: type any username and password
at the login screen and the character is created on first login. Closing the
window shuts the server down gracefully — saves flush — before the process
exits.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--cache-dir` | `./data/pack` | packed game cache |
| `--wordenc-path` | `<cache-dir>/../raw/wordenc` | raw wordenc jagfile for the chat filter |
| `--data-dir` | `./data` | world database and player saves |
| `--expose-tcp` | `false` | bind the loopback ports (43594 world, 8080 ondemand, 2004 login, 2005 friends) instead of running everything in-process. Off by default: the binary normally opens no sockets at all. Turn it on to attach a second client, run `tcpdump`, or `curl` the ondemand endpoints. |
| `--world-port` | `43594` | game TCP port |
| `--ondemand-port` | `8080` | cache/OnDemand HTTP port |
| `--login-port` | `2004` | internal login gRPC port |
| `--friends-port` | `2005` | internal friends gRPC port |
| `--mem` | `high` | client memory mode: `high` or `low` |
| `--world-type` | `members` | `members` or `free` |
| `--version` | | print build and content provenance, then exit |
| `--console` | `false` | Windows only: also open a console window for log output when launched from Explorer. Ignored elsewhere, and not needed when starting from a terminal — see [Windows](#windows). |

With `--expose-tcp`, the port flags select which loopback ports to bind.
**Every listener binds `127.0.0.1` and there is no flag to change that** — see
[`SECURITY.md`](SECURITY.md) for the trust model, including what loopback
binding does not protect against on a shared machine. Without `--expose-tcp`,
the binary opens no sockets at all.

### Windows

The Windows build links as a GUI app, so double-clicking
`goscape-singleplayer.exe` opens the game window and nothing else. Where its
output goes depends on how it was started:

| Started from | Output |
|---|---|
| a terminal (`cmd`, PowerShell, Git bash) | that terminal, as on any other platform |
| a redirect or pipe (`... -version > out.txt`) | the file or pipe |
| Explorer, a shortcut, a file association | `<data-dir>\logs\goscape-singleplayer.log` |
| Explorer, with `--console` | a console window, as well as the game window |

The log file holds what the terminal would have shown — the server log, any
warning, and a panic trace if the window fails to open (the usual cause is a
graphics driver or a remote session with no GL). It is truncated at every
start, so it always describes the run you just did. `--console`'s window
closes when the process does, so for a crash the log file is the more useful
of the two.

Linux and macOS have no console subsystem for a binary to be linked against,
so `--console` is accepted but inert there and the binary says so when passed.
That is not the same as saying macOS has no second window: Finder cannot launch
a bare Unix executable without a terminal, so double-clicking the binary there
opens Terminal.app to run it. Fixing that means shipping a `.app` bundle, which
is a packaging change rather than a linker flag, and is not done here.

## Tests

```bash
go test ./...
```

## How it works

`cmd/goscape-singleplayer` starts the goscape server stack in-process with
every module enabled (`world`, `login`, `friends`, `ondemand`, SQLite), waits
for it to report ready, then opens the client window against it — over
in-memory transports by default, or loopback with `--expose-tcp`.

The interesting part is shutdown. Three paths can end the process — the window
closing, a signal, or a server module failing — and all three converge on one
`sync.Once` so the server stops exactly once and player saves always flush
before exit. `internal/server` owns the embedded server's configuration and
lifecycle; `main.go` documents the three exit paths at the top of the file.

## Contributing

Game behaviour, protocol handling and rendering live upstream — this repository
is the launcher. [`CONTRIBUTING.md`](CONTRIBUTING.md) covers which branch to
target, how the upstream version pins are maintained, and how to develop
against local checkouts of both projects.

## License

MIT — see [LICENSE](LICENSE). This launcher combines two MIT-licensed projects
derived from Lost City's work; [`NOTICE`](NOTICE) carries the attribution,
including the Jagex notice covering the game content release binaries embed.
