# goscape-singleplayer — Embedded content cache (rev-274)

**Date:** 2026-08-31
**Status:** Approved
**Scope:** rev-274 first, then backported to rev-254, rev-245.2, rev-244,
rev-225. No changes to goscape or goscape-client.

## Goal

Publish release binaries a player can download and run with no further setup:
no cache to build, no goscape checkout, no packer to install. Building from
source keeps today's behaviour exactly — a plain `go build` embeds nothing and
still expects `--cache-dir`.

The game content comes from
[LostCityRS/Content](https://github.com/LostCityRS/Content), which keeps one
branch per revision (`225`, `244`, `245.2`, `254`, `274` — a 1:1 match with our
revision branches) and is MIT-licensed.

## Decisions already made

| Decision | Choice | Rationale |
|---|---|---|
| Content selection | Pinned commit per branch in `content.lock` | A tag rebuilds byte-identically; the stamped hash is known before the build runs; content bumps are reviewable commits |
| Embedding | Optional, behind the `embedcache` build tag | Source builds stay small and asset-free; only releases carry content |
| Delivery | Embedded in the binary | Works offline, one file to download; chosen over fetch-on-first-run |
| Cache access | Extract to `<data-dir>/cache` at startup | goscape reads the cache by path; see *Rejected: the `fs.FS` port* |
| Packer | `go run github.com/zsrv/goscape/cmd/goscape-cli pack` from the **pinned** goscape | The packer always matches the server that will read the pack; no goscape checkout in CI |
| Release artifacts | Embedded only, 5 native targets | One obvious download; mirrors goscape-client's release matrix |
| Provenance | `-version`, mirroring `goscape-client`'s `pkg/util/build` | One familiar flag across the repo family |

### Rejected: the `fs.FS` port

Teaching goscape's runtime loaders to read from an `fs.FS` would avoid writing
anything to disk and would fix a latent bug (`modules/ondemand/handler.go:118`
serves `/maps/` from a hardcoded `data/pack/client/maps`, ignoring
`cfg.CachePath`). It was rejected on size after measurement:

| Branch | `filestream.New` | `CachePath` refs | `ServeFile` routes |
|---|---|---|---|
| rev-225 | 0 | 40 | 11 |
| rev-244 / 245.2 | 7 | 33 | 3 |
| rev-254 | 7 | 34 | 3 |
| rev-274 | 8 | 36 | 2 |

`pkg/objtype` alone holds 23 `Load*Types(cachePath)` functions, each calling
`jagfile.LoadJagfile` independently and each invoked twice (startup and
reload) — so there is a shared primitive but no shared signature. With
`fonttype`, `script`, `crctable`, `MakeCRCs`, `gm.Init`, `encfilter`,
`filestream` and the ondemand routes, the port is roughly 30 signatures per
branch, ~150 across the five, plus test updates (`filestream.New` has 64 test
call sites). rev-225 is not a backport target at all: it has no `filestream`
usage and serves an 11-route file tree.

That is a large mechanical diff through the core loaders of a repository
published on 2026-08-30, and it blocks this project until it lands and is
pinned. Extraction confines the entire change to this unpublished repository
for roughly 150 lines. The `/maps/` bug is left for a separate upstream fix.

A related consequence, recorded so it is not rediscovered: `filestream.New`
mutates the filesystem even when `readOnly=true` — it `MkdirAll`s the directory
and fabricates empty `main_file_cache.dat`/`.idx0-4` when they are missing,
then opens them `O_RDONLY`. It is faithful to the TS reference
(`if (!fs.existsSync(dir)) mkdirSync`). Any future `fs.FS` variant cannot
reproduce that and would need a `docs/PORTING.md` divergence row.

## Repo layout

```
internal/content/
    content.go              provenance vars (ldflags-injected) + Info() block
    source.go               FS() (fs.FS, bool) — the embed seam
    extract.go              materialize an fs.FS into a directory, stamp-guarded
    extract_test.go
    testdata/fixturepack/   few-KB pack standing in for the real one in CI
    embedded/
        embed_on.go         //go:build embedcache   -> //go:embed all:pack
        embed_off.go        //go:build !embedcache  -> nil, false
        pack/               GITIGNORED — produced by `make embed-pack`
content.lock                pinned Content commit for this branch
Makefile                    embed-pack, embed-pack-fixture, build targets
```

`embed_off.go` returns `(nil, false)`, so a default build never references
`pack/` and does not care that it is absent. With `-tags embedcache` and no
`pack/`, the build fails at compile time with Go's own `pattern pack: no
matching files found` — an early, clear failure rather than a binary that
silently ships an empty cache. The pattern is `all:pack` so nothing is skipped
for a leading dot or underscore.

## content.lock and the pack pipeline

```
repo   = LostCityRS/Content
branch = 274
commit = 2b62ae68d5f1...
```

`make embed-pack` reads it and runs, with no goscape checkout:

```bash
git clone --filter=blob:none https://github.com/LostCityRS/Content "$W"
git -C "$W" checkout "$COMMIT"
RAW=$(go list -m -f '{{.Dir}}' github.com/zsrv/goscape)/data/raw   # rev-244+ only
go run github.com/zsrv/goscape/cmd/goscape-cli pack \
    --src-dir "$W" --out-dir internal/content/embedded/pack --raw-dir "$RAW"
```

Verified: `go run github.com/zsrv/goscape/cmd/goscape-cli` resolves from this
module, because `goscape` is already a dependency and `cmd/goscape-cli` is a
package within it. Packing is pure Go (`CGO_ENABLED=0`).

**The wordenc source differs by revision**, and both paths are satisfied
without extra inputs:

- **rev-244 … rev-274** — the prebuilt `wordenc` jagfile ships inside the
  goscape module zip at `data/raw/wordenc` (confirmed present in the module
  cache for `11ac7d989dd8` and `a0e075636bfa`). Pass it via `--raw-dir`.
- **rev-225** — the goscape module has no `data/raw`; Content branch `225`
  instead carries the sources `wordenc/{badenc,domainenc,fragmentsenc,tldlist}.txt`,
  which the rev-225 packer compiles into `client/wordenc`. `--raw-dir` is
  omitted.

Resulting pack sizes: **27 MB** on rev-225 (`client/` + `server/`), **39 MB** on
rev-274 (`client/`, `server/`, `main_file_cache.*`, `mapview/`).

The Makefile then computes a SHA-256 over the packed tree, and both values are
injected at link time:

```
-X .../internal/content.Commit=<from content.lock>
-X .../internal/content.PackDigest=<sha256 of the packed tree>
```

The digest is computed once at pack time, never at startup: it doubles as the
extraction stamp, and hashing 39 MB on every launch would be unacceptable.

## Runtime resolution and extraction

Resolved once in `main`, before `server.CheckCache`:

1. `--cache-dir` **explicitly passed** — detected with `flag.Visit`, so the
   documented `./data/pack` default still works — use it verbatim and ignore
   the embedded cache.
2. Otherwise, a cache is embedded — ensure it is extracted at
   `<data-dir>/cache` and use that.
3. Otherwise — today's actionable "no packed cache" error, unchanged.

`server.CheckCache` moves to after this resolution so it validates the chosen
directory rather than firing before extraction has happened.

Extraction is idempotent and stamp-guarded:

- Read `<data-dir>/cache/.content-stamp`. If it equals the injected
  `PackDigest`, skip entirely — an O(1) string compare, so warm starts pay
  nothing.
- Otherwise walk the embedded FS into `<data-dir>/cache.tmp-<pid>`, writing
  `.content-stamp` **last**. Then swap it in, in this order: rename any
  existing `cache/` to `cache.old-<pid>`, rename the temp directory to
  `cache/`, and only then remove `cache.old-<pid>`. Deleting the live tree
  before the replacement is in place would leave no cache at all if the rename
  failed.
- Writing the stamp last means an interrupted extraction is never mistaken for
  a complete one, since a temp tree without a stamp is never promoted. The
  rename makes the swap atomic within a filesystem.
- Any leftover `cache.tmp-*` or `cache.old-*` directories from a crashed run
  are removed at the start of the next extraction. They are the only way this
  scheme can accumulate anything.
- Two instances starting concurrently each extract to their own pid-suffixed
  temp directory and race on the rename. Both orderings leave a complete,
  identically-contented `cache/`, so no locking is needed.
- An upgraded binary carries a different digest and re-extracts automatically,
  with no user action and no stale-cache failure mode.

## Provenance

`internal/content` exposes ldflags-injected `Repo`, `Branch`, `Commit`,
`PackDigest`, and a compile-time `Embedded` bool from the build tag. A new
`-version` flag renders build metadata in `goscape-client`'s established shape,
with a content block appended:

```
goscape-singleplayer
  version:     rev274-v1.0.0
  revision:    9d4d4c4
  branch:      rev-274
  go version:  go1.26
  build user:  github-actions
  build date:  2026-08-31
  content:
    repo:      LostCityRS/Content
    branch:    274
    commit:    2b62ae68d
    pack:      sha256:1f3a...
    embedded:  yes
```

A source build with no tag reports `embedded: no` and `unknown` for the
unstamped fields, matching how `goscape-client` renders an unstamped binary.

## NOTICE and documentation

All six branches currently assert the opposite of what these binaries will
contain:

> …distributes no Jagex game assets or content — you supply your own packed cache.

NOTICE gains a Lost City Content section (MIT, © 2023-2026 Lost City) and a
reworked Jagex paragraph distinguishing the two build modes: a source build
embeds nothing and still requires a supplied cache, while a **released binary**
contains a game cache compiled from `LostCityRS/Content` at the pinned commit,
with the underlying game design and assets remaining the property of Jagex Ltd.
The goscape and goscape-client NOTICE files are not touched — they genuinely
ship no assets.

Per-branch README gains a Downloads section, documents `--cache-dir` as the
override, and documents `-version`. CONTRIBUTING gains the `content.lock` bump
procedure alongside the existing module-pin procedure.

## Release workflow

Mirrors `goscape-client/.github/workflows/release.yml`: tag `rev<N>-v<semver>`,
the CI gate reused via `workflow_call` so a failing tag never reaches the
matrix, and five native runners — `linux-amd64`, `linux-arm64`, `darwin-amd64`,
`darwin-arm64`, `windows-amd64`. No cross-compiling, because CGO. Artifacts are
named `goscape-singleplayer-rev274-v1.0.0-<target>.tar.gz` (`.zip` on Windows).

One structural difference from the client's workflow — a `pack` job upstream of
the matrix:

```
gate ──> pack (ubuntu, CGO off) ──> build matrix (5 native runners) ──> release
              └── uploads pack/ ────┘ downloads it, builds -tags embedcache
```

Packing runs the RuneScript compiler; inside the matrix it would run five
times. Packing once also guarantees all five platforms embed a byte-identical
cache, so `PackDigest` is identical across targets — which matters, because
that digest is what `-version` invites users to compare.

Link-time values follow the client's approach, including its security posture:
untrusted input reaches `run:` steps through env, never string interpolation.

## Verification

Extraction is where the bugs will be. `fstest.MapFS` stands in for the embedded
FS, so no large fixture is needed:

| Case | Expectation |
|---|---|
| fresh data-dir | tree written, stamp matches, `CheckCache` passes |
| stamp matches | no filesystem writes at all |
| stamp differs (upgrade) | old tree removed, re-extracted |
| stamp missing, tree present | re-extracted, not trusted |
| interrupted extraction (temp dir, no stamp) | not mistaken for complete |
| stale `cache.tmp-*` / `cache.old-*` present | removed before extracting |
| precedence | table test over explicit/default `--cache-dir` × embedded/not |
| provenance rendering | embedded and non-embedded forms |

Build coverage needs deliberate care: without it, the `embedcache` path would
only ever compile during a release. CI gains a second build job running
`make embed-pack-fixture`, which copies `internal/content/testdata/fixturepack/`
into `embedded/pack/` and builds with the tag. The real 39 MB pack stays out of
git while every push still proves the embed directive resolves and the tagged
code compiles.

## Out of scope

- Any change to goscape or goscape-client, including the `/maps/` hardcoded
  path bug and the `fs.FS` port.
- A slim (cache-less) release artifact. Source builds cover that audience.
- Hot-reloading embedded content. The extracted tree is derived data; changing
  content means a new binary or `--cache-dir`.
- Garbage-collecting old extracted caches. Extraction replaces in place under
  `--data-dir`, and the only transient directories it can leave behind are
  cleaned by the next run.
- Automated `content.lock` bump PRs. Deferred until the manual flow has been
  exercised at least once per branch.
