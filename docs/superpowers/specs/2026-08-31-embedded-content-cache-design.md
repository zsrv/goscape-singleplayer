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
| Cache access | Extract the bundle to `<data-dir>/content/` at startup | goscape reads the cache by path; see *Rejected: the `fs.FS` port* |
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
    testdata/fixturebundle/ few-KB bundle standing in for the real one in CI
    embedded/
        embed_on.go         //go:build embedcache   -> //go:embed all:bundle
        embed_off.go        //go:build !embedcache  -> nil, false
        bundle/             GITIGNORED — produced by `make embed-pack`
            pack/           goscape-cli pack output
            raw/wordenc     rev-244+ only; copied from the goscape module
content.lock                pinned Content commit for this branch
Makefile                    embed-pack, embed-pack-fixture, build targets
```

`embed_off.go` returns `(nil, false)`, so a default build never references
`bundle/` and does not care that it is absent. With `-tags embedcache` and no
`bundle/`, the build fails at compile time with Go's own `pattern bundle: no
matching files found` — an early, clear failure rather than a binary that
silently ships an empty cache. The pattern is `all:bundle` so nothing is
skipped for a leading dot or underscore.

**Why a bundle rather than the pack alone.** On rev-244 … rev-274 the binary
needs two things, not one: the packed cache *and* the raw `wordenc` jagfile,
which is an **input** to the packer and therefore never appears in its output.
`main.go` derives the wordenc default as `<cache-dir>/../raw/wordenc` and
`server.CheckWordEnc` fails fast when it is missing, so an extracted tree
containing only the pack would start and then abort. Embedding a bundle whose
two members are `pack/` and `raw/` makes the existing default resolve with no
change to the derivation logic. On rev-225 the bundle has no `raw/` member,
since that revision's packer compiles wordenc into `pack/client/wordenc`.

## content.lock and the pack pipeline

```
repo          = LostCityRS/Content
branch        = 274
commit        = 2b62ae68dfed02b441bae47987a01d6bcbaeb358
engine_repo   = LostCityRS/Engine-TS
engine_branch = 274
engine_commit = 1d25566cb53e7af1b1cb18ade8af996316c19614
```

The second pin exists because Content alone is not packable — see *The
generated pack indexes* below. Engine-TS `274` at `1d25566c` is the revision
goscape's own porting spec names as its sync target, so the three pins
describe one coherent revision.

`make embed-pack` reads it and runs, with no goscape checkout:

```bash
git clone --filter=blob:none https://github.com/LostCityRS/Content "$W"
git -C "$W" checkout "$COMMIT"

# Generate the server-only pack indexes into $W/pack/ — Content ships none.
git clone --filter=blob:none https://github.com/LostCityRS/Engine-TS "$E"
git -C "$E" checkout "$ENGINE_COMMIT"
(cd "$E" && npm ci && BUILD_SRC_DIR="$W" npm run build)

RAW=$(go list -m -f '{{.Dir}}' github.com/zsrv/goscape)/data/raw   # rev-244+ only
go run github.com/zsrv/goscape/cmd/goscape-cli pack \
    --src-dir "$W" --out-dir internal/content/embedded/bundle/pack --raw-dir "$RAW"
install -m 0644 "$RAW/wordenc" internal/content/embedded/bundle/raw/wordenc
```

### The generated pack indexes

Content's `pack/*.pack` files map config names to numeric IDs. **Fourteen of
them are gitignored** — Content's own `.gitignore` explains why: "since these
are generated for the server only, we don't have to share them". The list
includes `param.pack`, `struct.pack`, `enum.pack`, `category.pack`,
`hunt.pack`, `dbtable.pack`, `dbrow.pack`, `mesanim.pack`, `varn.pack`,
`vars.pack` and `script.pack`. A `git clone` of Content therefore contains 19
index files, not 30.

**goscape reads these and never writes them.** `pack.PackFile` has `Register`
and `Save`, but nothing in the pack pipeline calls either. When a name is
absent from `param.pack`, `parseStructConfigFor` fails its
`paramTypes.ConfigNames[name]` lookup; `read_typed.go` then reports the
generic `invalid property value in <file>: param=<name>,<value>` and
**discards the underlying `unknown param` error**, which is what makes this
failure so hard to read — an `int` param assigned `5` appears to fail
value-parsing when it never reached parsing at all.

**Engine-TS generates them.** `tools/pack/PackFile.ts:validateConfigPack`
crawls the config names, registers any it has not seen with `pack.max++`, and
calls `pack.save()`. That auto-assignment runs for server-only packs, and for
transmitted packs only when `BUILD_VERIFY` is off. Transmitted packs — `obj`,
`loc`, `npc`, `seq` — still require hand-assigned IDs, because those IDs are
baked into the client cache and must stay stable.

So the pipeline needs an Engine-TS build step before the goscape packer, and
`content.lock` gains a matching engine pin. `npm run build` (which runs
`tsx tools/pack/Build.ts`) is the supported entry point; it does more work than we need (it builds its own
cache too) but generating the indexes is the side effect we depend on, and
there is no narrower supported entry.

**On reproducibility.** Auto-assigned IDs depend on crawl order and on
whatever the index already contained. CI always starts from a fresh clone with
no server-only indexes, so a given content commit yields the same assignment
every time and a tag still rebuilds byte-identically. A *developer's* tree is
different: an existing stale index is appended to rather than rebuilt, so
local IDs can diverge from CI's. `make embed-pack` therefore clones into a
temp directory rather than reusing a working checkout, and a bundle built
locally is not guaranteed to match one built by CI. Only CI's output is
published.

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
   the embedded bundle.
2. Otherwise, a bundle is embedded — ensure it is extracted at
   `<data-dir>/content/`, then use `<data-dir>/content/pack` as the cache
   directory.
3. Otherwise — today's actionable "no packed cache" error, unchanged.

Extracting the bundle to `<data-dir>/content/` is what makes the wordenc
default work untouched: with the cache directory at `<data-dir>/content/pack`,
the existing `<cache-dir>/../raw/wordenc` derivation lands on
`<data-dir>/content/raw/wordenc`, exactly where the bundle put it.

`server.CheckCache` and `server.CheckWordEnc` both move to after this
resolution, so they validate the chosen directory rather than firing before
extraction has happened.

Extraction is idempotent and stamp-guarded:

- Read `<data-dir>/content/.content-stamp`. If it equals the injected
  `PackDigest`, skip entirely — an O(1) string compare, so warm starts pay
  nothing.
- Otherwise walk the embedded FS into `<data-dir>/content.tmp-<pid>`, writing
  `.content-stamp` **last**. Then swap it in, in this order: rename any
  existing `content/` to `content.old-<pid>`, rename the temp directory to
  `content/`, and only then remove `content.old-<pid>`. Deleting the live tree
  before the replacement is in place would leave no cache at all if the rename
  failed.
- Writing the stamp last means an interrupted extraction is never mistaken for
  a complete one, since a temp tree without a stamp is never promoted. The
  rename makes the swap atomic within a filesystem.
- Two instances starting concurrently each extract to their own pid-suffixed
  temp directory and race on the install rename. **They do not both succeed at
  renaming** — on a cold first run both find no `content/`, both write their
  temp tree, and the loser's install fails with `EEXIST`/`ENOTEMPTY`. The loser
  therefore re-reads `content/.content-stamp` after a failed install: if it now
  matches the digest, the winner has installed a complete tree and the loser
  removes its temp directory and reports success. Only a mismatched stamp is an
  error. Still no locking, but the guarantee is "the loser observes the winner's
  result", not "both renames succeed".

  A stamp can only appear in `content/` after the top-of-function check by way
  of a peer's atomic install, and the stamp is written last inside the temp
  tree — so a matching stamp implies a complete tree, and the early return
  cannot mask a genuine failure.
- Any leftover `content.tmp-*` or `content.old-*` directories from a crashed run
  are swept at the start of the next extraction. They are the only way this
  scheme can accumulate anything. **The sweep must not be unconditional.** A
  directory belonging to another pid is indistinguishable, by name alone, from
  a live peer's in-flight extraction — and deleting one is not merely rude, it
  is silently corrupting: `writeTree` calls `os.MkdirAll` per entry, so the
  victim recreates the deleted structure without error, stamps the resulting
  partial tree as complete, and renames it into place. It is then never
  re-extracted. So: a process always clears its **own** pid's leftovers, and
  clears a **foreign** one only when its mtime is older than a conservative
  liveness threshold no in-flight extraction could still be inside (an
  extraction of the real bundle takes seconds; the threshold is an hour). If
  the stat fails, the directory is left alone rather than guessed at.

  This ordering matters: the concurrency guarantee above rests on the
  pid-suffixed temp directories, the atomic rename, and the loser's stamp
  re-read. The sweep is the one thing that can falsify it — deleting a live
  peer's in-flight tree lets the victim silently recreate and stamp a partial
  one — which is why the sweep is constrained rather than the guarantee
  weakened.
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
gate ──> pack (ubuntu) ──────────────> build matrix (5 native runners) ──> release
           1. checkout Content @ pin        └── downloads bundle,
           2. checkout Engine-TS @ pin          builds -tags embedcache
           3. npm ci + npm run build
              (generates pack/*.pack)
           4. goscape-cli pack
           5. uploads bundle/ ───────────┘
```

The pack job has five steps rather than one, because the goscape packer cannot
run against a bare Content clone (see *The generated pack indexes*). Steps 2-3
are a Bun toolchain and an Engine-TS checkout pinned by `content.lock`'s
`engine_*` fields; they exist solely to write the server-only `pack/*.pack`
indexes into the Content clone. Only step 4's output is uploaded — Engine-TS's
own cache output is discarded.

Node, not Bun. At the pinned Engine-TS commit the repository ships a
`package-lock.json` and a plain `"build": "tsx tools/pack/Build.ts"` script, so
`npm ci` reproduces its dependency tree exactly and the first-party
`actions/setup-node` is the only toolchain action needed — this workflow
deliberately carries no third-party actions, for the same reason the release
step uses the preinstalled `gh` CLI instead of `softprops/action-gh-release`.

This is itself an argument for pinning the engine: **later** Engine-TS commits
replace `package-lock.json` with `bun.lock` and rename the script to
`node:build`, so a floating `274` branch would silently break the step. The
pin fixes both the lockfile format and the script name.

**Verified end to end** on 2026-08-31 against the pinned trio: a bare Content
clone carries 19 of the 30 index files; `npm ci && npm run build` took ~9 s and
produced all 30 with `pyre_level` present; `goscape-cli pack` then succeeded,
emitting the expected 39 MB `client/` + `server/` + `main_file_cache.*` +
`mapview/` tree. Both the engine and the packer emit the same three benign
`missing model` warnings.

Packing runs the RuneScript compiler; inside the matrix it would run five
times. Packing once also guarantees all five platforms embed a byte-identical
cache, so `PackDigest` is identical across targets — which matters, because
that digest is what `-version` invites users to compare. The index generation
must likewise happen exactly once, upstream of the matrix: run per-target it
could assign different IDs on different runners and silently produce five
divergent caches.

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
| stale `content.tmp-*` / `content.old-*`, own pid or aged | removed before extracting |
| foreign `content.tmp-*` with a recent mtime | left alone — it may be a live peer |
| precedence | table test over explicit/default `--cache-dir` × embedded/not |
| provenance rendering | embedded and non-embedded forms |

A pack-pipeline check belongs here too, and did not exist before: after the
index-generation step, assert that `pack/param.pack` in the clone contains a
name that only the pinned content revision introduces. Without it, a silently
skipped Engine-TS build degrades into the same opaque `invalid property value`
failure this design exists to avoid.

Build coverage needs deliberate care: without it, the `embedcache` path would
only ever compile during a release. CI gains a second build job running
`make embed-pack-fixture`, which copies `internal/content/testdata/fixturebundle/`
into `embedded/bundle/` and builds with the tag. The real 39 MB pack stays out of
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
