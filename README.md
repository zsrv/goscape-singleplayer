# goscape-singleplayer

A single-binary, singleplayer RuneScape: the [goscape](../goscape) server stack
and the [goscape-client](../goscape-client) game client combined into one
process, talking over loopback TCP.

## Branch model

`main` holds only this README. Code lives on revision branches (`rev-274`,
`rev-254`, `rev-245.2`, `rev-244`, `rev-225`), each pairing with the
same-named branches of `goscape` and `goscape-client`.

## Dependency wiring

Neither `goscape` nor `goscape-client` is fetchable as a Go module, so each
revision branch uses `replace` directives pointing at sibling checkouts
(`../goscape`, `../goscape-client`). Building a given revision requires those
checkouts to have the matching revision branch checked out.

## Usage (rev-225)

This branch's `replace` targets are the `../goscape-rev225` and
`../goscape-client-rev225` sibling worktrees — no branch-switching needed in
either sibling, they're already parked on their rev-225 tips.

Build (CGO required — GLFW/OpenGL):

    CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer

You need a packed game cache. In the goscape-rev225 repo, `make pack`
produces one; point `--cache-dir` at its output (default `./data/pack`).
rev-225 packs a split layout — `client/` and `server/` subdirectories under
the cache root — and the chat word-filter (wordenc) is baked into the cache
itself (`client/wordenc`), so there is no separate raw wordenc file to wire
up and no `--wordenc-path` flag on this branch.

    ./goscape-singleplayer --cache-dir ../goscape-rev225/data/pack

The world database and your character's saves live under `--data-dir`
(default `./data`). Accounts auto-register: type any username/password at the
login screen and the character is created on first login. Closing the window
shuts the server down gracefully (saves flush) before the process exits.

All listeners bind 127.0.0.1 only. Ports are adjustable if the defaults
collide: `--world-port 43594 --ondemand-port 8080 --login-port 2004
--friends-port 2005`.

The client itself takes two pass-through flags: `--mem low` selects the
client's low-memory mode and `--world-type free` a free-to-play world
(defaults: `high`, `members`).
