# goscape-singleplayer — rev-244

A single-binary, singleplayer RuneScape: the
[goscape](https://github.com/zsrv/goscape) server stack and the
[goscape-client](https://github.com/zsrv/goscape-client) game client running in
one process, talking to each other over loopback TCP.

No server to deploy, no configuration file, no account to register — run the
binary and you are at the login screen.

This branch targets **wire-protocol revision 244** and is pinned to the
`rev-244` branches of both upstream projects. For the other revisions and the
project overview, see the
[`main` branch README](https://github.com/zsrv/goscape-singleplayer/blob/main/README.md).

## Requirements

- Go 1.26 or newer
- **CGO** — the client links GLFW, OpenGL and ALSA. On Debian or Ubuntu, the
  system packages are the ones in
  [goscape-client's `apt-packages.txt`](https://github.com/zsrv/goscape-client/blob/main/.devcontainer/apt-packages.txt).
- A packed revision 244 game cache (see below). This repository ships no game
  assets.

## Build

```bash
CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer
```

Both upstream projects are pinned by exact version in `go.mod`, so a clone
builds without any other checkout present.

## The game cache

You supply your own. In a [goscape](https://github.com/zsrv/goscape) checkout on
the `rev-244` branch, `make pack` produces one; point `--cache-dir` at its
output (default `./data/pack`). The launcher checks for `main_file_cache.dat`
there and fails with an actionable message if the pack is missing.

The chat word filter (wordenc) is a separate raw jagfile on this revision, not
part of the pack. `--wordenc-path` defaults to `<cache-dir>/../raw/wordenc`,
which is exactly where goscape's committed `data/raw/wordenc` sits when you
pack into `data/pack`, so the default is usually right.

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
| `--world-port` | `43594` | game TCP port |
| `--ondemand-port` | `8080` | cache/OnDemand HTTP port |
| `--login-port` | `2004` | internal login gRPC port |
| `--friends-port` | `2005` | internal friends gRPC port |
| `--mem` | `high` | client memory mode: `high` or `low` |
| `--world-type` | `members` | `members` or `free` |

The port flags exist for collisions only. **Every listener binds `127.0.0.1`
and there is no flag to change that** — see [`SECURITY.md`](SECURITY.md) for
the trust model, including what loopback binding does not protect against on a
shared machine.

## Tests

```bash
go test ./...
```

## How it works

`cmd/goscape-singleplayer` starts the goscape server stack in-process with
every module enabled (`world`, `login`, `friends`, `ondemand`, SQLite), waits
for it to report ready, then opens the client window against loopback.

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
including the note that no Jagex assets are distributed here.
