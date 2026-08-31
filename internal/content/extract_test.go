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
