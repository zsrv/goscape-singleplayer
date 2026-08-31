# Embedded Content Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship release binaries with the game cache baked in, so a player downloads one file and plays, while a plain `go build` keeps today's behaviour and embeds nothing.

**Architecture:** A build tag (`embedcache`) gates an `//go:embed` of a bundle produced by the goscape packer that ships inside the pinned goscape module. At startup the bundle is extracted to `<data-dir>/content/`, guarded by a build-time digest so warm starts cost an O(1) string compare. Provenance is injected at link time and printed by a new `-version` flag.

**Tech Stack:** Go 1.26, `embed`, `io/fs`, `testing/fstest`, GNU Make, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-08-31-embedded-content-cache-design.md`

## Global Constraints

- Branch: `rev-274` only. Backports to rev-254, rev-245.2, rev-244, rev-225 follow in a separate pass.
- Go 1.26 or newer; CGO required for any build of `./cmd/...` (GLFW/OpenGL/ALSA).
- No changes to `goscape` or `goscape-client`. Both stay pinned as they are.
- Every commit must leave `gofmt -l .` empty and `go vet ./...` clean.
- Default `go build` must never reference the embedded bundle, and must behave exactly as it does today.
- All Go commands in this repo run as `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go ...`.
- Commit messages use `type(scope): summary` with a body explaining *why*, and `git commit --no-gpg-sign`.
- Content pin for this branch: repo `LostCityRS/Content`, branch `274`, commit `2b62ae68dfed02b441bae47987a01d6bcbaeb358`.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/build/build.go` | Binary build metadata (version/revision/branch/user/date), ldflags-injected |
| `internal/content/content.go` | Content provenance vars + `Info()` block |
| `internal/content/source.go` | `Bundle() (fs.FS, bool)` — re-exports the build-tag seam |
| `internal/content/extract.go` | `EnsureExtracted` — stamp-guarded atomic materialization |
| `internal/content/resolve.go` | `ResolveCacheDir` — precedence between `--cache-dir` and the bundle |
| `internal/content/embedded/embed_on.go` | `//go:build embedcache` — the `//go:embed all:bundle` |
| `internal/content/embedded/embed_off.go` | `//go:build !embedcache` — returns `nil, false` |
| `internal/content/testdata/fixturebundle/` | Few-KB stand-in bundle for CI's tagged build |
| `content.lock` | Pinned Content commit for this branch |
| `Makefile` | `embed-pack`, `embed-pack-fixture`, `build`, `build-embedded` |
| `cmd/goscape-singleplayer/main.go` | `-version` flag; cache resolution before the server starts |

---

## Task 1: Build metadata package and `-version`

**Files:**
- Create: `internal/build/build.go`
- Create: `internal/build/build_test.go`
- Modify: `cmd/goscape-singleplayer/main.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `build.Info() string`; package vars `build.Version`, `build.Revision`, `build.Branch`, `build.BuildUser`, `build.BuildDate` (all `string`, set via `-ldflags -X`).

- [ ] **Step 1: Write the failing test**

```go
// internal/build/build_test.go
package build

import (
	"strings"
	"testing"
)

func TestInfoRendersUnknownForUnstampedFields(t *testing.T) {
	got := Info()
	for _, want := range []string{"goscape-singleplayer", "version:", "unknown"} {
		if !strings.Contains(got, want) {
			t.Errorf("Info() missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "go version:") || strings.Contains(got, "go version:  unknown") {
		t.Errorf("Info() should always report a real Go version:\n%s", got)
	}
}

func TestInfoUsesStampedValues(t *testing.T) {
	Version, Revision, Branch = "rev274-v1.0.0", "abc1234", "rev-274"
	t.Cleanup(func() { Version, Revision, Branch = "", "", "" })

	got := Info()
	for _, want := range []string{"rev274-v1.0.0", "abc1234", "rev-274"} {
		if !strings.Contains(got, want) {
			t.Errorf("Info() missing stamped %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/build/ -run TestInfo -v`
Expected: FAIL — `internal/build/build.go` does not exist, so the package won't compile.

- [ ] **Step 3: Write minimal implementation**

```go
// Package build reports how this binary was built. The Version, Revision,
// Branch, BuildUser and BuildDate values are injected at link time by the
// Makefile and the release workflow (-ldflags -X); a plain `go build` leaves
// them empty, in which case they render as "unknown". This mirrors
// goscape-client's pkg/util/build so the whole repo family reports the same
// shape.
package build

import (
	"cmp"
	"fmt"
	"runtime"
)

var (
	Version   string
	Revision  string
	Branch    string
	BuildUser string
	BuildDate string
	GoVersion string
)

func init() { GoVersion = runtime.Version() }

// Info returns the build metadata as a multi-line, human-readable block,
// suitable for printing behind -version.
func Info() string {
	return fmt.Sprintf(`goscape-singleplayer
  version:     %s
  revision:    %s
  branch:      %s
  go version:  %s
  build user:  %s
  build date:  %s`,
		cmp.Or(Version, "unknown"),
		cmp.Or(Revision, "unknown"),
		cmp.Or(Branch, "unknown"),
		GoVersion,
		cmp.Or(BuildUser, "unknown"),
		cmp.Or(BuildDate, "unknown"),
	)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/build/ -v`
Expected: PASS (both tests).

- [ ] **Step 5: Add the `-version` flag to main**

In `cmd/goscape-singleplayer/main.go`, add `"github.com/zsrv/goscape-singleplayer/internal/build"` to the import block. Then add the flag declaration immediately after the `worldType` flag, and handle it immediately after `flag.Parse()`:

```go
	worldType := flag.String("world-type", "members", "world type: free|members")
	showVersion := flag.Bool("version", false, "print build and content provenance, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(build.Info())
		return
	}
```

- [ ] **Step 6: Verify the flag works**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache CGO_ENABLED=1 go run ./cmd/goscape-singleplayer -version`
Expected: the six-line block, with `unknown` for version/revision/branch/user/date and a real `go version`.

- [ ] **Step 7: Commit**

```bash
git add internal/build cmd/goscape-singleplayer/main.go
git commit --no-gpg-sign -m "feat(version): add -version with ldflags-injected build metadata

The binary reported nothing about itself. Mirrors goscape-client's
pkg/util/build so every binary in the family answers -version the same way;
the content provenance block lands on top of this in a later change."
```

---

## Task 2: The embed seam

**Files:**
- Create: `internal/content/embedded/embed_off.go`
- Create: `internal/content/embedded/embed_on.go`
- Create: `internal/content/source.go`
- Create: `internal/content/source_test.go`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: nothing.
- Produces: `embedded.Bundle() (fs.FS, bool)` and `content.Bundle() (fs.FS, bool)` — the FS is rooted at the bundle, so `pack/...` and `raw/...` are its top-level entries. Second return is `false` when no bundle is compiled in.

- [ ] **Step 1: Write the failing test**

```go
// internal/content/source_test.go
package content

import "testing"

// The default build has no bundle. The tagged build is covered by CI's
// second build job (make embed-pack-fixture), not by this test.
func TestBundleAbsentWithoutBuildTag(t *testing.T) {
	fsys, ok := Bundle()
	if ok {
		t.Fatal("Bundle() reported a bundle in an untagged build")
	}
	if fsys != nil {
		t.Fatalf("Bundle() returned a non-nil FS with ok=false: %#v", fsys)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/content/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write the two build-tag files and the re-export**

```go
// internal/content/embedded/embed_off.go
//go:build !embedcache

// Package embedded carries the game content bundle when the binary is built
// with -tags embedcache, and nothing at all otherwise. Keeping the //go:embed
// directive behind a build tag is what lets a plain `go build` ignore the
// (gitignored, usually absent) bundle directory entirely.
package embedded

import "io/fs"

// Bundle reports that this build carries no content.
func Bundle() (fs.FS, bool) { return nil, false }
```

```go
// internal/content/embedded/embed_on.go
//go:build embedcache

package embedded

import (
	"embed"
	"io/fs"
)

// bundle holds the packed game cache and, on rev-244+, the raw wordenc
// jagfile. It is produced by `make embed-pack` and is NOT tracked in git.
// Building with -tags embedcache and no bundle/ directory fails here, at
// compile time, which is the intended failure: better than a binary that
// silently ships an empty cache.
//
//go:embed all:bundle
var bundle embed.FS

// Bundle returns the embedded content rooted at the bundle directory, so its
// entries are "pack" and (rev-244+) "raw".
func Bundle() (fs.FS, bool) {
	sub, err := fs.Sub(bundle, "bundle")
	if err != nil {
		// Unreachable: the //go:embed above would have failed the build.
		panic("content: embedded bundle is malformed: " + err.Error())
	}
	return sub, true
}
```

```go
// internal/content/source.go
package content

import (
	"io/fs"

	"github.com/zsrv/goscape-singleplayer/internal/content/embedded"
)

// Bundle returns the content compiled into this binary, if any. The returned
// FS is rooted at the bundle: "pack" is the packed cache, and on rev-244+
// "raw/wordenc" is the chat word filter.
func Bundle() (fs.FS, bool) { return embedded.Bundle() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/content/ -v`
Expected: PASS.

- [ ] **Step 5: Ignore the generated bundle**

Add to `.gitignore`, under the "Runtime state" section:

```
# Generated by `make embed-pack`: the packed game cache embedded into release
# binaries. Never committed — it is 39 MB of build output, and every `go get`
# of this module would otherwise pay for it.
/internal/content/embedded/bundle/
```

- [ ] **Step 6: Verify the untagged build is unaffected**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache CGO_ENABLED=1 go build -o /dev/null ./... && GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go vet ./...`
Expected: both succeed with no bundle directory present.

- [ ] **Step 7: Commit**

```bash
git add internal/content .gitignore
git commit --no-gpg-sign -m "feat(content): build-tag seam for an embedded content bundle

Two files behind opposing build tags, so a default build never references
the bundle directory and does not care that it is absent, while -tags
embedcache fails at compile time if it is. The bundle is pack/ plus (on
this revision) raw/wordenc, because wordenc is an input to the packer and
never appears in its output."
```

---

## Task 3: Stamp-guarded extraction

**Files:**
- Create: `internal/content/extract.go`
- Create: `internal/content/extract_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `content.EnsureExtracted(src fs.FS, dir, digest string) error`, and the exported constant `content.StampName = ".content-stamp"`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/content/extract_test.go
package content

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func testBundle() fstest.MapFS {
	return fstest.MapFS{
		"pack/main_file_cache.dat": {Data: []byte("dat")},
		"pack/client/config":       {Data: []byte("cfg")},
		"raw/wordenc":              {Data: []byte("we")},
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestEnsureExtractedFreshDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("EnsureExtracted: %v", err)
	}
	if got := read(t, filepath.Join(dir, "pack", "client", "config")); got != "cfg" {
		t.Errorf("pack/client/config = %q, want %q", got, "cfg")
	}
	if got := read(t, filepath.Join(dir, "raw", "wordenc")); got != "we" {
		t.Errorf("raw/wordenc = %q, want %q", got, "we")
	}
	if got := read(t, filepath.Join(dir, StampName)); got != "digest-1\n" {
		t.Errorf("stamp = %q, want %q", got, "digest-1\n")
	}
}

func TestEnsureExtractedSkipsWhenStampMatches(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	marker := filepath.Join(dir, "pack", "client", "config")
	if err := os.WriteFile(marker, []byte("TOUCHED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	if got := read(t, marker); got != "TOUCHED" {
		t.Error("matching stamp must skip extraction entirely; the tree was rewritten")
	}
}

func TestEnsureExtractedReExtractsOnDigestChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	stale := filepath.Join(dir, "pack", "obsolete")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExtracted(testBundle(), dir, "digest-2"); err != nil {
		t.Fatalf("upgrade extract: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("re-extraction must replace the tree, not merge into it")
	}
	if got := read(t, filepath.Join(dir, StampName)); got != "digest-2\n" {
		t.Errorf("stamp = %q, want %q", got, "digest-2\n")
	}
}

func TestEnsureExtractedDistrustsTreeWithoutStamp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, StampName)); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("re-extract: %v", err)
	}
	if got := read(t, filepath.Join(dir, StampName)); got != "digest-1\n" {
		t.Error("a tree with no stamp must be re-extracted, not trusted")
	}
}

func TestEnsureExtractedRemovesStaleTempDirs(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "content")
	for _, name := range []string{"content.tmp-999", "content.old-999"} {
		if err := os.MkdirAll(filepath.Join(parent, name, "junk"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("EnsureExtracted: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "content" {
			t.Errorf("leftover directory not cleaned: %s", e.Name())
		}
	}
}

func TestEnsureExtractedRejectsEmptyDigest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, ""); err == nil {
		t.Fatal("an unstamped build must not extract; want an error")
	}
}

func TestEnsureExtractedIsFastOnWarmStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	start := time.Now()
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("warm extract: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("warm start took %v; the stamp compare should be near-instant", elapsed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/content/ -run TestEnsureExtracted -v`
Expected: FAIL — `undefined: EnsureExtracted`, `undefined: StampName`.

- [ ] **Step 3: Write the implementation**

```go
// internal/content/extract.go
package content

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StampName is the file inside an extracted content directory recording the
// pack digest that produced it.
const StampName = ".content-stamp"

// EnsureExtracted materializes src into dir, unless dir already carries a
// stamp equal to digest.
//
// The stamp is written last, so an interrupted extraction is never mistaken
// for a complete one: a temp tree without a stamp is simply never promoted.
// The swap renames the existing tree aside before renaming the new one into
// place, and only then deletes it — removing the live tree first would leave
// no cache at all if the second rename failed.
//
// Concurrent callers each extract into their own pid-suffixed temp directory
// and race on the rename. Both orderings leave a complete, identically
// contented dir, so no locking is needed.
func EnsureExtracted(src fs.FS, dir, digest string) error {
	if digest == "" {
		return errors.New("content: refusing to extract without a pack digest (binary was not stamped at build time)")
	}
	if current, err := os.ReadFile(filepath.Join(dir, StampName)); err == nil {
		if strings.TrimSpace(string(current)) == digest {
			return nil
		}
	}

	parent, base := filepath.Dir(dir), filepath.Base(dir)
	if err := removeStale(parent, base); err != nil {
		return err
	}

	pid := os.Getpid()
	tmp := filepath.Join(parent, fmt.Sprintf("%s.tmp-%d", base, pid))
	old := filepath.Join(parent, fmt.Sprintf("%s.old-%d", base, pid))
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("content: clear %s: %w", tmp, err)
	}
	if err := writeTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, StampName), []byte(digest+"\n"), 0o644); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("content: write stamp: %w", err)
	}

	movedAside := false
	if err := os.Rename(dir, old); err == nil {
		movedAside = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("content: move existing %s aside: %w", dir, err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		if movedAside {
			_ = os.Rename(old, dir)
		}
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("content: install %s: %w", dir, err)
	}
	if movedAside {
		_ = os.RemoveAll(old)
	}
	return nil
}

// staleForeignThreshold is how old a foreign process's tmp/old directory's
// modification time must be before removeStale treats it as debris from a
// crashed run rather than a live peer's in-flight extraction. Extracting the
// real ~39 MB bundle takes seconds, so an hour is generous headroom.
const staleForeignThreshold = time.Hour

// removeStale clears temp and aside directories left by a crashed run.
//
// ownTmp and ownOld — this process's own pid-suffixed directories — are
// always removed; no other process can be using them. A directory belonging
// to another pid is removed only when its modification time is older than
// staleForeignThreshold, since a live peer's in-flight extraction leaves an
// identically-shaped directory that must not be deleted out from under it.
// If the directory can't be stat'd, it is left alone rather than guessed at.
//
// Note the own-directory test is exact path equality, not a suffix match:
// suffix matching would let pid 123 claim pid 4123's live directory unless the
// separator were folded into the comparison. Equality on fully-qualified paths
// cannot make that mistake at all.
func removeStale(parent, base, ownTmp, ownOld string) error {
	if err := os.RemoveAll(ownTmp); err != nil {
		return fmt.Errorf("content: remove own stale %s: %w", ownTmp, err)
	}
	if err := os.RemoveAll(ownOld); err != nil {
		return fmt.Errorf("content: remove own stale %s: %w", ownOld, err)
	}

	for _, pattern := range []string{base + ".tmp-*", base + ".old-*"} {
		matches, err := filepath.Glob(filepath.Join(parent, pattern))
		if err != nil {
			return fmt.Errorf("content: scan for stale %s: %w", pattern, err)
		}
		for _, m := range matches {
			if m == ownTmp || m == ownOld {
				continue
			}
			info, err := os.Stat(m)
			if err != nil {
				continue // can't confirm it's not in flight; leave it alone
			}
			if time.Since(info.ModTime()) < staleForeignThreshold {
				continue // recent enough that a live peer may still be writing it
			}
			if err := os.RemoveAll(m); err != nil {
				return fmt.Errorf("content: remove stale %s: %w", m, err)
			}
		}
	}
	return nil
}

// writeTree copies every regular file in src to dst, streaming rather than
// buffering: the server pack is ~26 MB and main_file_cache.dat alone is 8 MB.
func writeTree(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("content: walk %s: %w", p, err)
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("content: unexpected non-regular entry %s", p)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("content: mkdir for %s: %w", p, err)
		}
		in, err := src.Open(p)
		if err != nil {
			return fmt.Errorf("content: open %s: %w", p, err)
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("content: create %s: %w", target, err)
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return fmt.Errorf("content: write %s: %w", target, err)
		}
		return out.Close()
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/content/ -v`
Expected: PASS — all eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/content/extract.go internal/content/extract_test.go
git commit --no-gpg-sign -m "feat(content): stamp-guarded extraction of the embedded bundle

goscape reads the cache by path, so an embedded bundle has to become real
files before the server can start. Extraction is guarded by the build-time
pack digest: a matching stamp skips the work entirely, so warm starts pay
one string compare rather than 39 MB of writes, and an upgraded binary
re-extracts on its own.

The stamp is written last and the swap renames the live tree aside before
installing the new one, so neither an interrupted extraction nor a failed
rename can leave the player without a cache."
```

---

## Task 4: Cache resolution and provenance reporting

**Files:**
- Create: `internal/content/content.go`
- Create: `internal/content/resolve.go`
- Create: `internal/content/resolve_test.go`
- Create: `internal/content/content_test.go`
- Modify: `cmd/goscape-singleplayer/main.go`

**Interfaces:**
- Consumes: `content.EnsureExtracted` (Task 3), `content.Bundle` (Task 2), `build.Info` (Task 1).
- Produces: `content.ResolveCacheDir(explicit bool, cacheDir, dataDir string, bundle fs.FS, digest string) (string, error)`; `content.Info() string`; vars `content.Repo`, `content.Branch`, `content.Commit`, `content.PackDigest`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/content/resolve_test.go
package content

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestResolveCacheDirExplicitFlagWins(t *testing.T) {
	data := t.TempDir()
	got, err := ResolveCacheDir(true, "/somewhere/pack", data, testBundle(), "digest-1")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	if got != "/somewhere/pack" {
		t.Errorf("got %q, want the explicit --cache-dir", got)
	}
	if _, err := os.Stat(filepath.Join(data, "content")); !os.IsNotExist(err) {
		t.Error("an explicit --cache-dir must not trigger extraction")
	}
}

func TestResolveCacheDirExtractsBundle(t *testing.T) {
	data := t.TempDir()
	got, err := ResolveCacheDir(false, "./data/pack", data, testBundle(), "digest-1")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	want := filepath.Join(data, "content", "pack")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "main_file_cache.dat")); err != nil {
		t.Errorf("bundle was not extracted: %v", err)
	}
}

// The wordenc default is derived by main as <cache-dir>/../raw/wordenc.
// Extraction must place the bundle so that derivation resolves untouched.
func TestResolveCacheDirSatisfiesWordEncDefault(t *testing.T) {
	data := t.TempDir()
	cacheDir, err := ResolveCacheDir(false, "./data/pack", data, testBundle(), "digest-1")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	wordenc := filepath.Join(cacheDir, "..", "raw", "wordenc")
	if _, err := os.Stat(wordenc); err != nil {
		t.Errorf("wordenc default %s does not resolve: %v", filepath.Clean(wordenc), err)
	}
}

func TestResolveCacheDirNoBundleFallsBackToFlag(t *testing.T) {
	got, err := ResolveCacheDir(false, "./data/pack", t.TempDir(), nil, "")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	if got != "./data/pack" {
		t.Errorf("got %q, want the --cache-dir default", got)
	}
}

func TestResolveCacheDirEmptyBundleIsNotABundle(t *testing.T) {
	got, err := ResolveCacheDir(false, "./data/pack", t.TempDir(), fstest.MapFS{}, "digest-1")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	if got != "./data/pack" {
		t.Errorf("got %q, want the --cache-dir default for an empty bundle", got)
	}
}
```

```go
// internal/content/content_test.go
package content

import (
	"strings"
	"testing"
)

func TestInfoReportsNotEmbedded(t *testing.T) {
	got := Info()
	if !strings.Contains(got, "embedded:  no") {
		t.Errorf("untagged build should report embedded: no:\n%s", got)
	}
	if !strings.Contains(got, "unknown") {
		t.Errorf("unstamped fields should render as unknown:\n%s", got)
	}
}

func TestInfoReportsStampedProvenance(t *testing.T) {
	Repo, Branch, Commit, PackDigest = "LostCityRS/Content", "274", "2b62ae68d", "sha256:1f3a"
	t.Cleanup(func() { Repo, Branch, Commit, PackDigest = "", "", "", "" })

	got := Info()
	for _, want := range []string{"LostCityRS/Content", "274", "2b62ae68d", "sha256:1f3a"} {
		if !strings.Contains(got, want) {
			t.Errorf("Info() missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/content/ -run 'TestResolve|TestInfo' -v`
Expected: FAIL — `undefined: ResolveCacheDir`, `undefined: Info`, `undefined: Repo`.

- [ ] **Step 3: Write the provenance package**

```go
// Package content reports where this binary's game content came from, and —
// when built with -tags embedcache — carries and extracts it.
//
// Repo, Branch, Commit and PackDigest are injected at link time by the
// Makefile and the release workflow. PackDigest doubles as the extraction
// stamp, which is why it is computed once at pack time rather than hashed at
// startup.
package content

import (
	"cmp"
	"fmt"
)

var (
	Repo       string
	Branch     string
	Commit     string
	PackDigest string
)

// Info returns the content provenance block printed under -version.
func Info() string {
	embedded := "no"
	if _, ok := Bundle(); ok {
		embedded = "yes"
	}
	return fmt.Sprintf(`  content:
    repo:      %s
    branch:    %s
    commit:    %s
    pack:      %s
    embedded:  %s`,
		cmp.Or(Repo, "unknown"),
		cmp.Or(Branch, "unknown"),
		cmp.Or(Commit, "unknown"),
		cmp.Or(PackDigest, "unknown"),
		embedded,
	)
}
```

- [ ] **Step 4: Write the resolver**

```go
// internal/content/resolve.go
package content

import (
	"io/fs"
	"path/filepath"
)

// ResolveCacheDir decides which directory the server should read its cache
// from, extracting the embedded bundle if that is the answer.
//
// Precedence:
//  1. an explicitly passed --cache-dir wins and is used verbatim;
//  2. otherwise an embedded bundle is extracted to <dataDir>/content and its
//     pack subdirectory is used;
//  3. otherwise the --cache-dir default is returned unchanged, and the
//     caller's existing "no packed cache" check reports the problem.
//
// Extracting to <dataDir>/content is what keeps main's wordenc default
// working untouched: with the cache at <dataDir>/content/pack, the existing
// <cache-dir>/../raw/wordenc derivation lands on <dataDir>/content/raw/wordenc.
func ResolveCacheDir(explicit bool, cacheDir, dataDir string, bundle fs.FS, digest string) (string, error) {
	if explicit || bundle == nil {
		return cacheDir, nil
	}
	if entries, err := fs.ReadDir(bundle, "."); err != nil || len(entries) == 0 {
		return cacheDir, nil
	}
	dir := filepath.Join(dataDir, "content")
	if err := EnsureExtracted(bundle, dir, digest); err != nil {
		return "", err
	}
	return filepath.Join(dir, "pack"), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache go test ./internal/content/ -v`
Expected: PASS — all tests, including Task 3's.

- [ ] **Step 6: Wire it into main**

In `cmd/goscape-singleplayer/main.go`, add `"github.com/zsrv/goscape-singleplayer/internal/content"` to the imports. Replace the `-version` handler from Task 1 so it prints both blocks:

```go
	if *showVersion {
		fmt.Println(build.Info())
		fmt.Println(content.Info())
		return
	}
```

Then replace the existing cache check — currently `server.CheckCache(*cacheDir)` immediately before `server.NewConfig` — with resolution first:

```go
	explicitCacheDir := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "cache-dir" {
			explicitCacheDir = true
		}
	})

	bundle, _ := content.Bundle()
	resolvedCacheDir, err := content.ResolveCacheDir(
		explicitCacheDir, *cacheDir, *dataDir, bundle, content.PackDigest)
	if err != nil {
		fatalf("content: %v", err)
	}

	if err := server.CheckCache(resolvedCacheDir); err != nil {
		fatalf("%v", err)
	}
```

`server.NewConfig` must now receive `CacheDir: resolvedCacheDir` instead of `CacheDir: *cacheDir`. `CheckWordEnc` already runs after `NewConfig` against `cfg.World.WordEncPath`, so it needs no change — it will see the derived path under the extracted tree.

Note the shadowing: `err` is first declared here; the later `cfg, err := server.NewConfig(...)` must become `cfg, err = server.NewConfig(...)` with `var cfg *app.Config` declared, or simply keep `:=` by renaming this one to `resolveErr`. Prefer the rename — it is a smaller diff:

```go
	resolvedCacheDir, resolveErr := content.ResolveCacheDir(
		explicitCacheDir, *cacheDir, *dataDir, bundle, content.PackDigest)
	if resolveErr != nil {
		fatalf("content: %v", resolveErr)
	}
```

- [ ] **Step 7: Verify the untagged binary is unchanged**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache CGO_ENABLED=1 go run ./cmd/goscape-singleplayer --data-dir /tmp/sp-test`
Expected: exits with the existing `no packed game cache at ./data/pack (main_file_cache.dat missing)` message — proving behaviour is unchanged without a bundle.

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache CGO_ENABLED=1 go run ./cmd/goscape-singleplayer -version`
Expected: build block followed by the content block reporting `embedded:  no`.

- [ ] **Step 8: Commit**

```bash
git add internal/content cmd/goscape-singleplayer/main.go
git commit --no-gpg-sign -m "feat(content): resolve the cache directory from the embedded bundle

An explicitly passed --cache-dir still wins, detected with flag.Visit so the
documented ./data/pack default keeps working. Otherwise an embedded bundle
is extracted to <data-dir>/content and its pack/ used.

Extracting to <data-dir>/content rather than <data-dir>/cache is deliberate:
main derives the wordenc default as <cache-dir>/../raw/wordenc, so this
layout makes that derivation land on the bundle's own raw/wordenc with no
change to the derivation logic.

-version now prints the content provenance block alongside build metadata."
```

---

## Task 5: content.lock, Makefile, and the pack pipeline

**Files:**
- Create: `content.lock`
- Create: `Makefile`
- Create: `internal/content/testdata/fixturebundle/pack/main_file_cache.dat`
- Create: `internal/content/testdata/fixturebundle/pack/client/config`
- Create: `internal/content/testdata/fixturebundle/raw/wordenc`

**Interfaces:**
- Consumes: `internal/build` and `internal/content` package variable paths for `-X`.
- Produces: `make embed-pack`, `make embed-pack-fixture`, `make build`, `make build-embedded`; the `CONTENT_*` and `PACK_DIGEST` Make variables.

- [ ] **Step 1: Write content.lock**

```
# Pinned LostCityRS/Content revision for this branch. Bump with a commit that
# says what changed upstream; `make embed-pack` reads this file, and the
# release workflow stamps the commit into the binary.
repo   = LostCityRS/Content
branch = 274
commit = 2b62ae68dfed02b441bae47987a01d6bcbaeb358
```

- [ ] **Step 2: Write the Makefile**

```make
# goscape-singleplayer

SHELL := /bin/bash
BIN   := goscape-singleplayer
CMD   := ./cmd/goscape-singleplayer

BUNDLE_DIR := internal/content/embedded/bundle
PACK_DIR   := $(BUNDLE_DIR)/pack
RAW_DIR    := $(BUNDLE_DIR)/raw
# Written only after a pack fully succeeds, and deliberately a SIBLING of
# bundle/: the embed directive is `all:bundle`, and the all: prefix would
# otherwise sweep this marker into the embedded tree and the digest.
MARKER     := internal/content/embedded/.bundle-complete

CONTENT_REPO   := $(shell sed -n 's/^repo[[:space:]]*=[[:space:]]*//p' content.lock)
CONTENT_BRANCH := $(shell sed -n 's/^branch[[:space:]]*=[[:space:]]*//p' content.lock)
CONTENT_COMMIT := $(shell sed -n 's/^commit[[:space:]]*=[[:space:]]*//p' content.lock)

GIT_REVISION := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_BRANCH   := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
BUILD_TAG    ?= $(shell git describe --tags --exact-match 2>/dev/null)
BUILD_DATE   := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

BPREFIX := github.com/zsrv/goscape-singleplayer/internal/build
CPREFIX := github.com/zsrv/goscape-singleplayer/internal/content

# PACK_DIGEST is a deterministic digest over the bundle: every file's SHA-256
# and path, sorted, hashed again. Recursively evaluated (=) so it is computed
# only when a recipe references it — after embed-pack has run.
PACK_DIGEST = sha256:$(shell cd $(BUNDLE_DIR) 2>/dev/null && \
    find . -type f -print0 | LC_ALL=C sort -z | xargs -0 shasum -a 256 | shasum -a 256 | cut -d' ' -f1)

LDFLAGS = -s -w \
    -X $(BPREFIX).Version=$(BUILD_TAG) \
    -X $(BPREFIX).Revision=$(GIT_REVISION) \
    -X $(BPREFIX).Branch=$(GIT_BRANCH) \
    -X $(BPREFIX).BuildUser=$(shell whoami)@$(shell hostname) \
    -X $(BPREFIX).BuildDate=$(BUILD_DATE) \
    -X $(CPREFIX).Repo=$(CONTENT_REPO) \
    -X $(CPREFIX).Branch=$(CONTENT_BRANCH) \
    -X $(CPREFIX).Commit=$(CONTENT_COMMIT)

.PHONY: help build build-embedded embed-pack embed-pack-fixture test clean

help: ## list targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-20s %s\n", $$1, $$2}'

build: ## build without embedded content (the default; needs --cache-dir at runtime)
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(CMD)

build-embedded: ## build with the bundle in $(BUNDLE_DIR) embedded
	@test -f $(MARKER) || { echo "no complete bundle at $(BUNDLE_DIR); run 'make embed-pack' first" >&2; exit 1; }
	CGO_ENABLED=1 go build -trimpath -tags embedcache \
	    -ldflags "$(LDFLAGS) -X $(CPREFIX).PackDigest=$(PACK_DIGEST)" -o $(BIN) $(CMD)

embed-pack: ## pack the pinned Content revision into $(BUNDLE_DIR)
	@test -n "$(CONTENT_COMMIT)" || { echo "content.lock: no commit pinned" >&2; exit 1; }
	rm -f $(MARKER)
	rm -rf $(BUNDLE_DIR)
	mkdir -p $(PACK_DIR) $(RAW_DIR)
# One shell, so the trap can remove the clone on EVERY exit path. Make runs
# each recipe line in its own shell and aborts at the first failure, so a
# trailing `rm -rf` is only reached when nothing went wrong.
	set -euo pipefail; \
	WORK=$$(mktemp -d); \
	trap 'rm -rf "$$WORK"' EXIT; \
	git clone --filter=blob:none --no-checkout \
	    https://github.com/$(CONTENT_REPO).git "$$WORK"; \
	git -C "$$WORK" checkout --detach $(CONTENT_COMMIT); \
	GOSCAPE_RAW=$$(go list -m -f '{{.Dir}}' github.com/zsrv/goscape)/data/raw; \
	CGO_ENABLED=0 go run github.com/zsrv/goscape/cmd/goscape-cli pack \
	    --src-dir "$$WORK" --out-dir $(PACK_DIR) --raw-dir "$$GOSCAPE_RAW"; \
	install -m 0644 "$$GOSCAPE_RAW"/wordenc $(RAW_DIR)/wordenc
	touch $(MARKER)
	@echo "bundle ready: $$(du -sh $(BUNDLE_DIR) | cut -f1), digest $(PACK_DIGEST)"

embed-pack-fixture: ## install the tiny CI fixture as the bundle
	rm -f $(MARKER)
	rm -rf $(BUNDLE_DIR)
	mkdir -p $(BUNDLE_DIR)
	cp -R internal/content/testdata/fixturebundle/. $(BUNDLE_DIR)/
	touch $(MARKER)

test: ## run the test suite
	CGO_ENABLED=1 go test ./...

clean: ## remove build output and the generated bundle
	rm -rf $(BIN) $(BUNDLE_DIR) $(MARKER)
```

The `.gitignore` gains the marker alongside the bundle entry from Task 2:

```
# Written by `make embed-pack` only after a pack fully succeeds; `make
# build-embedded` refuses to build without it. A sibling of bundle/ rather
# than a file inside it, because the `all:` embed prefix would otherwise
# bake the marker into the shipped tree.
/internal/content/embedded/.bundle-complete
```

Note on `--raw-dir`: this branch is rev-274, where wordenc comes from the goscape module. **On rev-225 the `--raw-dir` argument and the `install` line are both dropped**, because that revision's packer compiles wordenc from Content's own `wordenc/*.txt` into `pack/client/wordenc`.

- [ ] **Step 3: Create the CI fixture bundle**

The fixture only has to satisfy the embed pattern and `server.CheckCache`, which stats `main_file_cache.dat`. Contents are irrelevant — CI never runs the game.

```bash
mkdir -p internal/content/testdata/fixturebundle/pack/client
mkdir -p internal/content/testdata/fixturebundle/raw
printf 'fixture, not a real cache\n' > internal/content/testdata/fixturebundle/pack/main_file_cache.dat
printf 'fixture\n' > internal/content/testdata/fixturebundle/pack/client/config
printf 'fixture\n' > internal/content/testdata/fixturebundle/raw/wordenc
```

- [ ] **Step 4: Verify the fixture path end to end**

Run:
```bash
make embed-pack-fixture
GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache make build-embedded
./goscape-singleplayer -version
```
Expected: the build succeeds with `-tags embedcache`, and `-version` reports `embedded:  yes` with a non-empty `pack: sha256:...` and the pinned content commit.

- [ ] **Step 5: Verify the untagged build still ignores the bundle**

Run: `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache make build && ./goscape-singleplayer -version`
Expected: `embedded:  no`, even though `internal/content/embedded/bundle/` exists on disk.

- [ ] **Step 6: Confirm the bundle is not tracked**

Run: `git status --porcelain internal/content/embedded/`
Expected: no output — the `.gitignore` rule from Task 2 covers it.

- [ ] **Step 7: Commit**

```bash
make clean
git add content.lock Makefile internal/content/testdata
git commit --no-gpg-sign -m "build: content.lock, Makefile, and the pack pipeline

embed-pack clones the pinned Content revision and packs it with the
goscape-cli that ships inside the pinned goscape module, so the packer
always matches the server that will read its output and CI needs no goscape
checkout. wordenc is copied from the same module into the bundle, since it
is an input to the packer and never appears in its output.

PACK_DIGEST is a deterministic digest over the bundle and doubles as the
extraction stamp, so it is computed once here rather than hashed at startup.

The fixture bundle exists so CI can build the tagged path on every push
without a 39 MB pack in git."
```

---

## Task 6: CI coverage of both build-tag states

**Files:**
- Modify: `.github/workflows/go.yml`

**Interfaces:**
- Consumes: `make embed-pack-fixture` (Task 5).
- Produces: a `build-embedded` job gating the tagged path, and a `workflow_call` trigger on `go.yml` that Task 8's release workflow reuses as its gate.

- [ ] **Step 1: Make go.yml callable**

`go.yml` currently triggers only on push and pull_request. Task 8's release workflow reuses it as a gate via `uses: ./.github/workflows/go.yml`, which requires a `workflow_call` trigger — without it the release fails at workflow-parse time. Add it to the `on:` block:

```yaml
on:
  push:
    branches: ['rev-*']
  pull_request:
    branches: ['rev-*']
  # Called by release.yml so a tag build reuses this gate rather than
  # duplicating it.
  workflow_call:
```

- [ ] **Step 2: Add the job**

Append to `.github/workflows/go.yml`, after the existing `build-test` job:

```yaml
  # The embedcache path would otherwise only ever be compiled during a
  # release. This builds it on every push using a few-KB fixture bundle, so
  # the //go:embed directive and the tagged code are gated without carrying a
  # 39 MB pack in git.
  build-embedded:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      - name: Install native build dependencies
        run: |
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends \
            gcc pkg-config libffi-dev \
            libgl-dev libgles-dev libegl-dev \
            libx11-dev libx11-xcb-dev libxkbcommon-x11-dev \
            libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev \
            libxxf86vm-dev \
            libasound2-dev

      - name: Install the fixture bundle
        run: make embed-pack-fixture

      - name: Build with -tags embedcache
        run: CGO_ENABLED=1 go build -trimpath -tags embedcache ./...

      - name: Test with -tags embedcache
        run: CGO_ENABLED=1 go test -tags embedcache ./...
```

- [ ] **Step 3: Verify locally what CI will run**

Run:
```bash
make embed-pack-fixture
GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache CGO_ENABLED=1 go build -trimpath -tags embedcache ./...
GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache CGO_ENABLED=1 go test -tags embedcache ./...
make clean
```
Expected: both succeed. Note `TestBundleAbsentWithoutBuildTag` from Task 2 is skipped by neither build — it asserts the *untagged* behaviour, so under `-tags embedcache` it would fail. Before running, guard that test file with `//go:build !embedcache` as its first line.

- [ ] **Step 4: Guard the untagged-only tests, and assert the tagged path**

Two test files assert untagged-only behaviour, not one: `source_test.go`'s
`TestBundleAbsentWithoutBuildTag` and `content_test.go`'s
`TestInfoReportsNotEmbedded` (which asserts `embedded:  no`). Both fail under
the tag, for the same reason.

Guard `source_test.go` by adding this as its **literal first line**:

```go
//go:build !embedcache
```

Do not guard `content_test.go` wholesale — it also holds
`TestInfoReportsStampedProvenance`, which is tag-agnostic (it only sets the
provenance vars and checks formatting) and should keep running in both states.
Split instead: move `TestInfoReportsNotEmbedded` verbatim into a new
`internal/content/content_notag_test.go` whose literal first line is
`//go:build !embedcache`, and leave `content_test.go` unguarded.

Then add the assertion that makes the tagged CI job worth running. Without it
the job proves only that the package compiles: under the tag the surviving
test files never call `Bundle()` or `Info()` at all, so a green run says
nothing about the tagged behaviour.

```go
//go:build embedcache

package content

import (
	"strings"
	"testing"
)

// TestBundlePresentWithBuildTag is the tagged counterpart to
// TestBundleAbsentWithoutBuildTag (source_test.go) and
// TestInfoReportsNotEmbedded (content_notag_test.go): it proves the
// -tags embedcache build actually carries and serves a bundle, not just
// that it compiles. main_file_cache.dat and wordenc are present in both the
// few-KB CI fixture bundle and a real rev-274 pack, so this test stays true
// for a genuine release build too.
func TestBundlePresentWithBuildTag(t *testing.T) {
	fsys, ok := Bundle()
	if !ok {
		t.Fatal("Bundle() reported no bundle in a -tags embedcache build")
	}
	if fsys == nil {
		t.Fatal("Bundle() returned ok=true with a nil FS")
	}

	for _, path := range []string{"pack/main_file_cache.dat", "raw/wordenc"} {
		if _, err := fsys.Open(path); err != nil {
			t.Errorf("Bundle() FS missing %q: %v", path, err)
		}
	}

	got := Info()
	if !strings.Contains(got, "embedded:  yes") {
		t.Errorf("tagged build should report embedded: yes:\n%s", got)
	}
}
```

Re-run Step 3 to confirm both tag states pass. Verify the split with
`go list -f '{{.TestGoFiles}}'` and `go list -tags embedcache -f '{{.TestGoFiles}}'`
on `./internal/content/`: `content_test.go` must appear in BOTH, and each
guarded file in exactly one. Run the tagged test **by name** — a test excluded
by a build tag produces output identical to one that passed.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/go.yml internal/content/source_test.go
git commit --no-gpg-sign -m "ci: build and test the embedcache path on every push

Without this the tagged path would only ever compile during a release,
which is exactly when a broken //go:embed directive is most expensive to
discover. Uses the few-KB fixture bundle so the real pack stays out of git.

source_test.go asserts untagged behaviour, so it is now guarded with
//go:build !embedcache."
```

---

## Task 7: NOTICE, README, and CONTRIBUTING

**Files:**
- Modify: `NOTICE`
- Modify: `README.md`
- Modify: `CONTRIBUTING.md`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Rewrite the NOTICE Jagex section**

The current closing section asserts the opposite of what release binaries will contain. Replace the `Jagex` section with:

```
-----------------------------------------------------------------------
Lost City — Content
-----------------------------------------------------------------------

Release binaries of goscape-singleplayer embed a game cache compiled from
the Lost City Content repository (https://github.com/LostCityRS/Content) at
the revision pinned in this branch's content.lock. Content is licensed under
the MIT License:

MIT License

Copyright (c) 2023-2026 Lost City

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

-----------------------------------------------------------------------
Jagex
-----------------------------------------------------------------------

The original RuneScape client and server are the intellectual property of
Jagex Ltd. This project is an unofficial, fan-made preservation and study
effort. It is not affiliated with or endorsed by Jagex, and it is not
authorised by Jagex.

Two build modes differ in what they contain, and the distinction matters:

- A build from source embeds no game content. It requires you to supply your
  own packed cache via --cache-dir, exactly as before.
- A release binary embeds a game cache compiled from the Lost City Content
  repository named above. That cache describes RuneScape game data, and the
  underlying game design and assets remain the property of Jagex Ltd.

Run `goscape-singleplayer -version` to see exactly which Content revision a
given binary carries, or whether it carries any at all.
```

- [ ] **Step 2: Add a Downloads section to the README**

Insert immediately after the intro paragraph, before `## Requirements`:

```markdown
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
```

- [ ] **Step 3: Document the content pin in CONTRIBUTING**

Add after the existing "The dependency pins" section:

```markdown
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
```

- [ ] **Step 4: Sweep the README for every restatement of the old claim**

The claim appears in the README **three times, in three different wordings**,
and a grep for the original phrase finds only the first. On rev-274 they were:

| Section | Wording |
|---|---|
| `## License` | "including the note that no Jagex assets are distributed here" |
| `## Requirements` | "This repository ships no game assets." |
| `## The game cache` | "You supply your own." |

Each contradicts the new Downloads section. Scope every one of them to the
build-from-source case and point at Downloads for the alternative — e.g. the
Requirements bullet becomes "A packed revision N game cache, **when building
from source** (see below). Release binaries carry one already — see
[Downloads](#downloads)."

Then read the whole README end to end asking only: *does any remaining
sentence imply a reader must supply their own game content, without saying
that applies to source builds?* Do not rely on grep for this step — the whole
point is that the restatements share no common phrase.

- [ ] **Step 5: Verify the docs are honest**

Run: `grep -rn "ships no game assets\|distributes no Jagex\|no Jagex assets are distributed" README.md NOTICE CONTRIBUTING.md`
Expected: no output. Note this grep is a backstop, not the check — Step 4's
read-through is the check, because a fourth restatement would not match any of
these patterns either.

Run: `grep -c "content.lock" CONTRIBUTING.md README.md`
Expected: at least one hit in CONTRIBUTING.md.

- [ ] **Step 6: Commit**

```bash
git add NOTICE README.md CONTRIBUTING.md
git commit --no-gpg-sign -m "docs: NOTICE now matches what release binaries contain

NOTICE claimed the project 'distributes no Jagex game assets or content —
you supply your own packed cache'. That stays true of a source build and
becomes false for a release binary, so the file now names the Lost City
Content repository, reproduces its MIT licence, and states plainly which of
the two build modes carries game data.

README gains a Downloads section; CONTRIBUTING documents the content.lock
bump alongside the existing module-pin procedure."
```

---

## Task 8: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: `make embed-pack` (Task 5), the `go.yml` workflow (Task 6) via `workflow_call`.
- Produces: a GitHub release per `rev<N>-v<semver>` tag.

- [ ] **Step 1: Write the workflow**

```yaml
name: release

# Tag shape: rev274-v1.0.0 — the revision, then the release semver. Mirrors
# goscape-client's convention so artifacts across the family sort together.
on:
  push:
    tags: ['rev*-v*']

# Least privilege: only the release job needs write. gate/pack/build run
# read-only.
permissions:
  contents: read

jobs:
  # A tag that fails the normal gate never reaches the build matrix.
  gate:
    uses: ./.github/workflows/go.yml

  # Packing runs the RuneScript compiler. Doing it once here rather than in
  # each matrix job saves four redundant runs AND guarantees every platform
  # embeds a byte-identical bundle, so the stamped PackDigest — which is what
  # -version invites users to compare — is the same everywhere.
  pack:
    needs: gate
    runs-on: ubuntu-latest
    outputs:
      digest: ${{ steps.digest.outputs.digest }}
    steps:
      - uses: actions/checkout@v7

      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      - name: Pack the pinned Content revision
        run: make embed-pack

      - id: digest
        name: Record the bundle digest
        run: |
          cd internal/content/embedded/bundle
          d=$(find . -type f -print0 | LC_ALL=C sort -z \
              | xargs -0 shasum -a 256 | shasum -a 256 | cut -d' ' -f1)
          echo "digest=sha256:$d" >> "$GITHUB_OUTPUT"

      - uses: actions/upload-artifact@v4
        with:
          name: bundle
          path: internal/content/embedded/bundle
          retention-days: 1

  build:
    needs: pack
    name: build ${{ matrix.target }}
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        include:
          - { target: linux-amd64,   os: ubuntu-26.04,     ext: tar.gz }
          - { target: linux-arm64,   os: ubuntu-26.04-arm, ext: tar.gz }
          - { target: darwin-amd64,  os: macos-26-intel,   ext: tar.gz }
          - { target: darwin-arm64,  os: macos-15,         ext: tar.gz }
          - { target: windows-amd64, os: windows-2025,     ext: zip }
    steps:
      - uses: actions/checkout@v7

      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      - name: Install native build dependencies (Linux)
        if: runner.os == 'Linux'
        run: |
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends \
            gcc pkg-config libffi-dev \
            libgl-dev libgles-dev libegl-dev \
            libx11-dev libx11-xcb-dev libxkbcommon-x11-dev \
            libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev \
            libxxf86vm-dev \
            libasound2-dev

      - uses: actions/download-artifact@v4
        with:
          name: bundle
          path: internal/content/embedded/bundle

      # The tag is checked out detached, so Branch is derived from the tag's
      # rev prefix (rev274-v1.0.0 -> rev-274). Untrusted input reaches the
      # script through env, never string interpolation.
      - name: Build
        shell: bash
        env:
          VERSION: ${{ github.ref_name }}
          DIGEST: ${{ needs.pack.outputs.digest }}
          ACTOR: ${{ github.actor }}
        run: |
          set -euo pipefail
          rev="${VERSION%%-v*}"                 # rev274
          branch="rev-${rev#rev}"               # rev-274
          B=github.com/zsrv/goscape-singleplayer/internal/build
          C=github.com/zsrv/goscape-singleplayer/internal/content
          repo=$(sed -n 's/^repo[[:space:]]*=[[:space:]]*//p' content.lock)
          cbranch=$(sed -n 's/^branch[[:space:]]*=[[:space:]]*//p' content.lock)
          commit=$(sed -n 's/^commit[[:space:]]*=[[:space:]]*//p' content.lock)
          # An `[ ... ] && x=y` one-liner would abort the step under set -e on
          # every non-Windows runner, because the false test makes the list
          # return 1. Use an explicit if.
          bin=goscape-singleplayer
          if [ "$RUNNER_OS" = Windows ]; then bin=goscape-singleplayer.exe; fi
          CGO_ENABLED=1 go build -trimpath -tags embedcache \
            -ldflags "-s -w \
              -X $B.Version=$VERSION \
              -X $B.Revision=$(git rev-parse --short HEAD) \
              -X $B.Branch=$branch \
              -X $B.BuildUser=$ACTOR@github-actions \
              -X $B.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
              -X $C.Repo=$repo \
              -X $C.Branch=$cbranch \
              -X $C.Commit=$commit \
              -X $C.PackDigest=$DIGEST" \
            -o "$bin" ./cmd/goscape-singleplayer

      - name: Verify the binary reports its content
        shell: bash
        run: |
          set -euo pipefail
          bin=./goscape-singleplayer
          if [ "$RUNNER_OS" = Windows ]; then bin=./goscape-singleplayer.exe; fi
          out=$("$bin" -version)
          echo "$out"
          echo "$out" | grep -q "embedded:  yes"

      - name: Archive (tar.gz)
        if: matrix.ext == 'tar.gz'
        env:
          VERSION: ${{ github.ref_name }}
          TARGET: ${{ matrix.target }}
        run: tar -czf "goscape-singleplayer-${VERSION}-${TARGET}.tar.gz" goscape-singleplayer LICENSE NOTICE README.md

      - name: Archive (zip)
        if: matrix.ext == 'zip'
        shell: bash
        env:
          VERSION: ${{ github.ref_name }}
          TARGET: ${{ matrix.target }}
        run: 7z a "goscape-singleplayer-${VERSION}-${TARGET}.zip" goscape-singleplayer.exe LICENSE NOTICE README.md

      - uses: actions/upload-artifact@v4
        with:
          name: ${{ matrix.target }}
          path: goscape-singleplayer-*.${{ matrix.ext }}

  release:
    needs: build
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      # `pattern` is load-bearing. download-artifact with no name/pattern
      # downloads EVERY artifact in the run, which would drag the pack job's
      # `bundle` (the whole content cache) into dist/ beside the archives.
      # Every target name contains a hyphen; `bundle` does not.
      - uses: actions/download-artifact@v4
        with:
          path: dist
          pattern: '*-*'
          merge-multiple: true

      # Assert dist/ holds exactly the five expected archives and nothing
      # else, BEFORE publishing. This must hold on its own rather than
      # trusting the pattern above — two independent checks of one property.
      # Counting only regular files would pass a dist/ polluted with leaked
      # directories, which is precisely the failure being guarded against.
      - name: Verify the release payload
        shell: bash
        run: |
          set -euo pipefail
          count=$(find dist -mindepth 1 -maxdepth 1 | wc -l)
          if [ "$count" -ne 5 ]; then
            echo "expected exactly 5 entries in dist/, found $count:" >&2
            find dist -mindepth 1 -maxdepth 1 >&2
            exit 1
          fi
          nonfiles=$(find dist -mindepth 1 -maxdepth 1 -not -type f)
          if [ -n "$nonfiles" ]; then
            echo "dist/ must contain only regular files, found:" >&2
            echo "$nonfiles" >&2
            exit 1
          fi

      # The preinstalled gh CLI rather than a third-party action: this job
      # holds contents: write, and a mutable @v2 tag would let its owner run
      # code here on every release. GH_REPO is required — gh resolves the repo
      # from a git remote or GH_REPO, and does NOT fall back to
      # GITHUB_REPOSITORY, so with no checkout it would otherwise fail with
      # "unable to determine current repository".
      - name: Publish release
        shell: bash
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_REPO: ${{ github.repository }}
          VERSION: ${{ github.ref_name }}
        run: gh release create "$VERSION" dist/* --generate-notes
```

- [ ] **Step 2: Validate the workflow parses**

Run: `python3 -c "import sys;import json;print(open('.github/workflows/release.yml').read().count('runs-on'))"`
Expected: `3` — one per job that declares a runner (`pack`, `build`, `release`). If PyYAML is available, prefer `python3 -c "import yaml;yaml.safe_load(open('.github/workflows/release.yml'))"`.

Run: `grep -c $'\t' .github/workflows/release.yml`
Expected: `0` — YAML must not contain tabs.

- [ ] **Step 3: Verify the archive contents claim is true**

Run: `ls LICENSE NOTICE README.md`
Expected: all three exist, since the archive steps reference them.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit --no-gpg-sign -m "ci: release workflow for embedded binaries

Tag rev<N>-v<semver> builds five native targets — no cross-compiling,
because the client links GLFW, GL and ALSA through cgo.

A pack job sits upstream of the matrix. Packing runs the RuneScript
compiler, so doing it once saves four redundant runs, and more importantly
it guarantees all five platforms embed a byte-identical bundle: the
PackDigest that -version invites users to compare is then the same
everywhere. Each build verifies its own binary reports embedded: yes before
the artifact is uploaded.

Untrusted input reaches run: steps through env rather than string
interpolation, matching goscape-client's release workflow."
```

---

## Self-Review

**Spec coverage.** Every section of the spec maps to a task: repo layout and the build-tag seam → Task 2; `content.lock` and the pack pipeline → Task 5; runtime resolution and extraction → Tasks 3 and 4; provenance → Tasks 1 and 4; NOTICE and documentation → Task 7; release workflow → Task 8; verification → Tasks 3, 4 and 6. The spec's "Out of scope" list adds nothing to implement.

**Type consistency.** `Bundle() (fs.FS, bool)` is defined in Task 2 and consumed unchanged in Tasks 4 and 5. `EnsureExtracted(src fs.FS, dir, digest string) error` and `StampName` are defined in Task 3 and consumed in Task 4's `ResolveCacheDir` and its tests. `build.Info()` (Task 1) and `content.Info()` (Task 4) are both consumed by main's `-version` handler. The ldflags paths in Task 5's Makefile and Task 8's workflow both target `internal/build` and `internal/content` package variables that Tasks 1 and 4 actually declare: `Version`, `Revision`, `Branch`, `BuildUser`, `BuildDate`, `Repo`, `Branch`, `Commit`, `PackDigest`.

**Known ordering hazard.** Task 6 Step 2 discovers that `TestBundleAbsentWithoutBuildTag` (Task 2) fails under `-tags embedcache`; Step 3 fixes it with a `//go:build !embedcache` guard. This is deliberate — the failure is worth seeing rather than pre-empting, because it demonstrates that the tagged and untagged builds really are different programs.

**Backport note.** Only `Makefile` (the `--raw-dir` argument and the `install` of wordenc) and `internal/content/testdata/fixturebundle/` differ on rev-225, whose packer compiles wordenc from Content's own sources into `pack/client/wordenc`. Everything else backports unchanged.
