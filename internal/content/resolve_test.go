package content

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestResolveCacheDirExplicitFlagWins(t *testing.T) {
	data := t.TempDir()
	got, err := ResolveCacheDir(true, "/somewhere/pack", data, testBundle(), true, "digest-1")
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
	got, err := ResolveCacheDir(false, "./data/pack", data, testBundle(), true, "digest-1")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	want := filepath.Join(data, "content", "pack")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "client", "config")); err != nil {
		t.Errorf("bundle was not extracted: %v", err)
	}
}

// rev-225 has no --wordenc-path derivation: wordenc is compiled into the pack
// at client/wordenc. What resolution must guarantee here is that the directory
// it returns is one server.CheckCache accepts, which stats client/config.
func TestResolveCacheDirSatisfiesCacheCheck(t *testing.T) {
	data := t.TempDir()
	cacheDir, err := ResolveCacheDir(false, "./data/pack", data, testBundle(), true, "digest-1")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	for _, rel := range []string{"client/config", "client/wordenc"} {
		if _, err := os.Stat(filepath.Join(cacheDir, rel)); err != nil {
			t.Errorf("resolved cache dir missing %s: %v", rel, err)
		}
	}
}

func TestResolveCacheDirNoBundleFallsBackToFlag(t *testing.T) {
	got, err := ResolveCacheDir(false, "./data/pack", t.TempDir(), nil, false, "")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	if got != "./data/pack" {
		t.Errorf("got %q, want the --cache-dir default", got)
	}
}

func TestResolveCacheDirEmptyBundleIsNotABundle(t *testing.T) {
	got, err := ResolveCacheDir(false, "./data/pack", t.TempDir(), fstest.MapFS{}, true, "digest-1")
	if err != nil {
		t.Fatalf("ResolveCacheDir: %v", err)
	}
	if got != "./data/pack" {
		t.Errorf("got %q, want the --cache-dir default for an empty bundle", got)
	}
}
