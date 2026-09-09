# Contributing to goscape-singleplayer

This repository is small on purpose. It contains no game logic: it is the glue
that starts the [goscape](https://github.com/zsrv/goscape) server stack
in-process, waits for it to become ready, starts the
[goscape-client](https://github.com/zsrv/goscape-client) game window against
it, over in-memory transports by default (or loopback with `--expose-tcp`),
and shuts the server down cleanly when the window closes.

**Most changes people want to make here belong in one of those two
repositories instead.** Game behaviour, protocol handling, rendering and
content live upstream. What lives here is process lifecycle, configuration
wiring, and the version pins that decide which upstream commits a build uses.

## Which branch

`main` carries documentation only. The launcher lives on the revision
branches, each pinned to the same-named branches of both upstreams:

| Branch | Pairs with |
|---|---|
| `rev-274` | newest revision; default target for changes |
| `rev-254`, `rev-245.2`, `rev-244`, `rev-225` | earlier revisions, all maintained |

Branch from the revision you are fixing. A lifecycle fix usually applies to
every branch; fix it on the newest affected branch first and let the backports
follow.

## The dependency pins

Both upstreams are public Go modules but carry no semver tags, so `go.mod`
pins them by pseudo-version — an exact commit, checksummed in `go.sum`:

```
require (
	github.com/zsrv/goscape        v0.0.0-20260831000000-af44e1b01c2b
	github.com/zsrv/goscape-client v0.0.0-20260710000000-b45631d32ee3
)
```

There is deliberately **no `replace` directive**. A `replace` pointing at a
sibling directory only works on the machine that has those directories, and it
is what stopped this repository from building anywhere else.

To move a branch onto newer upstream commits, pin by commit SHA:

```bash
go get github.com/zsrv/goscape@<sha> github.com/zsrv/goscape-client@<sha>
go mod tidy
```

Pin by SHA rather than by branch name. `@rev-225` resolves through
`proxy.golang.org`, which caches the branch-to-version mapping and can hand you
a tip that is hours behind the one you just pushed; a SHA is unambiguous.
Always name the upstream commits in the commit message — the pin bump is the
only record of what changed in the build.

### Working against local upstream checkouts

While developing a change that spans repositories, add a `replace` block
locally and **do not commit it**:

```
replace (
	github.com/zsrv/goscape        => ../goscape
	github.com/zsrv/goscape-client => ../goscape-client
)
```

Push the upstream commits, then land the SHA pin here instead.

### The content pin

`content.lock` names the exact `LostCityRS/Content` commit whose game data
release binaries embed. It is pinned, not tracked, so a given tag always
builds the same content and the stamped provenance is known before the build
runs.

To move to newer content:

```bash
git -C /path/to/Content fetch origin 274
git -C /path/to/Content rev-parse origin/274      # the new commit
$EDITOR content.lock                              # update commit =
make embed-pack && make build-embedded            # verify it packs and builds
```

Say in the commit message what changed upstream — the pin bump is the only
record of why the shipped content moved. Do not bump the pin and change code
in the same commit.

## Development

Requirements: Go 1.27 or newer, and **CGO** — the client links GLFW, OpenGL and
ALSA. On Debian or Ubuntu the system packages are the ones listed in
[goscape-client's `.devcontainer/apt-packages.txt`](https://github.com/zsrv/goscape-client/blob/main/.devcontainer/apt-packages.txt).

```bash
CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer
go test ./...
```

Running it needs a packed game cache, which this repository does not ship —
goscape's `make pack` produces one. See the README for the flags.

## Before you open a pull request

| You changed | Run |
|---|---|
| any Go code | `gofmt -l .` (must be empty), `go vet ./...`, `go test ./...` |
| the lifecycle in `cmd/` | build it and confirm the window closes cleanly: the server must stop before the process exits, or saves are lost |
| `go.mod` pins | `go mod tidy`, then a full build and test against the new pins |

CI runs the build, tests, `gofmt` and `go vet` on every `rev-*` branch.

The exit paths are the subtle part of this program. There are three of them —
window close, signal, and server failure — and they all converge on a single
`sync.Once` so the server stops exactly once. `cmd/goscape-singleplayer/main.go`
documents them at the top; if you change any of them, say in the pull request
which of the three you tested.

## Commits and pull requests

Commit messages follow `type(scope): summary` — `feat`, `fix`, `docs`, `chore`,
`refactor`, `test` — with a body explaining *why*, including what you verified.
Keep a pull request to one concern.

## Security

Do not open a public issue for a vulnerability. [`SECURITY.md`](SECURITY.md)
has the reporting process and the trust model — in particular, why every
listener binds `127.0.0.1` and what that does not protect against.

## Licensing

Contributions are accepted under this repository's [MIT license](LICENSE). By
opening a pull request you agree your work may be distributed under it. If your
change derives from another project's code, say so in the pull request so
[`NOTICE`](NOTICE) can be updated.
