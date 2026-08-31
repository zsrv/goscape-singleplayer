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
