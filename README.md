# goscape-singleplayer

A single-binary, singleplayer RuneScape: the
[goscape](https://github.com/zsrv/goscape) server stack and the
[goscape-client](https://github.com/zsrv/goscape-client) game client running in
one process, talking to each other over loopback TCP.

No server to deploy, no configuration file, no account to register — build the
binary, point it at a packed game cache, and you are at the login screen. Close
the window and the embedded server shuts down gracefully, flushing your saves.

## Branches

`main` carries documentation only. The launcher lives on the revision branches,
each pinned to the same-named branches of both upstream projects:

| Branch | Revision | Cache layout |
|---|---|---|
| [`rev-274`](https://github.com/zsrv/goscape-singleplayer/tree/rev-274) | 274 | flat, separate `wordenc` |
| [`rev-254`](https://github.com/zsrv/goscape-singleplayer/tree/rev-254) | 254 | flat, separate `wordenc` |
| [`rev-245.2`](https://github.com/zsrv/goscape-singleplayer/tree/rev-245.2) | 245.2 | flat, separate `wordenc` |
| [`rev-244`](https://github.com/zsrv/goscape-singleplayer/tree/rev-244) | 244 | flat, separate `wordenc` |
| [`rev-225`](https://github.com/zsrv/goscape-singleplayer/tree/rev-225) | 225 | split `client/` + `server/`, `wordenc` inside the cache |

All of them are maintained. Each branch's README has the build and run
instructions for that revision, including the flags that differ between them.

## Quick start

```bash
git clone -b rev-274 https://github.com/zsrv/goscape-singleplayer
cd goscape-singleplayer
CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer
./goscape-singleplayer --cache-dir /path/to/pack
```

Requirements: Go 1.26 or newer, and **CGO** — the client links GLFW, OpenGL and
ALSA. On Debian or Ubuntu the system packages are the ones in
[goscape-client's `apt-packages.txt`](https://github.com/zsrv/goscape-client/blob/main/.devcontainer/apt-packages.txt).

**This repository ships no game assets.** You supply the packed cache; `make
pack` in a [goscape](https://github.com/zsrv/goscape) checkout on the matching
revision branch produces one.

## How the pieces fit

This repository contains no game logic. It is roughly 400 lines of launcher:

- `internal/server` builds the goscape app configuration for a self-contained
  stack — `world`, `login`, `friends`, `ondemand` and SQLite, every module
  enabled, every listener on `127.0.0.1`, all state rooted under `--data-dir` —
  and owns the embedded server's start, readiness and shutdown.
- `cmd/goscape-singleplayer` starts that server, waits for it to report ready,
  then opens the client window against loopback. Three paths can end the
  process — the window closing, a signal, or a server module failing — and all
  three converge on one `sync.Once` so the server stops exactly once and player
  saves always flush before exit.

Both upstreams are pinned by exact pseudo-version in each branch's `go.mod`,
checksummed in `go.sum`. There are deliberately no `replace` directives: a
clone builds with nothing else checked out. [`CONTRIBUTING.md`](CONTRIBUTING.md)
covers how those pins are maintained.

## Security

This is a singleplayer launcher and its trust model is one trusted user on one
machine. Every listener binds `127.0.0.1` and no flag can move it — but the
internal `login` and `friends` gRPC services have no authentication, and
accounts auto-register by design. [`SECURITY.md`](SECURITY.md) has the details,
including what loopback binding does not protect against on a shared machine.

To run goscape for more than one player, run the real server instead of this
launcher.

## License

MIT — see [LICENSE](LICENSE). This launcher combines two MIT-licensed projects
derived from [Lost City](https://github.com/LostCityRS)'s work; [`NOTICE`](NOTICE)
carries the attribution. The original RuneScape client and server are the
intellectual property of Jagex Ltd; this is an unofficial, fan-made
preservation and study effort, not affiliated with or endorsed by Jagex, and it
distributes no Jagex game assets.
