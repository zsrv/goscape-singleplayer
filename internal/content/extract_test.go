package content

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func testBundle() fstest.MapFS {
	// rev-225 packs a split layout — client/ + server/ under the pack root —
	// and compiles wordenc into client/wordenc rather than shipping it as a
	// separate raw jagfile, so the bundle has no raw/ member on this branch.
	return fstest.MapFS{
		"pack/client/config":  {Data: []byte("cfg")},
		"pack/client/wordenc": {Data: []byte("we")},
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
	if got := read(t, filepath.Join(dir, "pack", "client", "wordenc")); got != "we" {
		t.Errorf("pack/client/wordenc = %q, want %q", got, "we")
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
	old := time.Now().Add(-2 * staleForeignThreshold)
	for _, name := range []string{"content.tmp-999", "content.old-999"} {
		p := filepath.Join(parent, name)
		if err := os.MkdirAll(filepath.Join(p, "junk"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
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

func TestEnsureExtractedPreservesRecentForeignTempDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "content")
	foreign := filepath.Join(parent, "content.tmp-424242")
	if err := os.MkdirAll(filepath.Join(foreign, "junk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("EnsureExtracted: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("a recent foreign temp dir must survive — it may be a live peer's in-flight extraction: %v", err)
	}
}

func TestEnsureExtractedRemovesOldForeignTempDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "content")
	foreign := filepath.Join(parent, "content.tmp-424242")
	if err := os.MkdirAll(filepath.Join(foreign, "junk"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleForeignThreshold)
	if err := os.Chtimes(foreign, old, old); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("EnsureExtracted: %v", err)
	}
	if _, err := os.Stat(foreign); !os.IsNotExist(err) {
		t.Error("an old foreign temp dir from a crashed run must be removed")
	}
}

func TestEnsureExtractedRejectsEmptyDigest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, ""); err == nil {
		t.Fatal("an unstamped build must not extract; want an error")
	}
}

// TestEnsureExtractedIsFastOnWarmStart proves the warm-start property that
// motivates the stamp check: a matching stamp makes EnsureExtracted return
// before doing any filesystem writes, rather than merely finishing quickly
// (a wall-clock assertion would flake on a loaded CI runner without actually
// testing the property). The install path always ends by renaming a new
// directory into place, which changes dir's identity even though the path
// stays the same, so comparing the directory's identity — not just its
// path — before and after is what proves no install happened.
func TestEnsureExtractedIsFastOnWarmStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureExtracted(testBundle(), dir, "digest-1"); err != nil {
		t.Fatalf("warm extract: %v", err)
	}
	after, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("warm start replaced dir via rename; a matching stamp must return immediately with no filesystem writes")
	}
}
