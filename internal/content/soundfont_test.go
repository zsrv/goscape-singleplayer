package content

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func soundFontBundle(body string) fstest.MapFS {
	return fstest.MapFS{
		"pack/main_file_cache.dat": {Data: []byte("cache")},
		"raw/wordenc":              {Data: []byte("wordenc")},
		"public/" + SoundFontName:  {Data: []byte(body)},
	}
}

// TestEnsureSoundFont_Materializes is the point of the whole exercise: a
// binary that embeds a SoundFont must leave it where the ondemand static root
// will serve it, or the client fetches /SCC1_Florestan.sf2, 404s, and plays
// silence.
func TestEnsureSoundFont_Materializes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")

	if err := EnsureSoundFont(soundFontBundle("SF2-BYTES"), true, dir); err != nil {
		t.Fatalf("EnsureSoundFont: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, SoundFontName))
	if err != nil {
		t.Fatalf("reading materialized soundfont: %v", err)
	}
	if string(got) != "SF2-BYTES" {
		t.Errorf("contents = %q, want %q", got, "SF2-BYTES")
	}

	// The public dir is a static-file server root; nothing but the asset
	// should appear in it, least of all a half-written temp file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading public dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != SoundFontName {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("public dir holds %v, want just %s", names, SoundFontName)
	}
}

// TestEnsureSoundFont_KeepsOperatorCopy pins that an existing file is never
// overwritten: the public dir is operator-owned, and someone who dropped in a
// different SoundFont meant it.
func TestEnsureSoundFont_KeepsOperatorCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SoundFontName)
	if err := os.WriteFile(path, []byte("OPERATORS-OWN"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSoundFont(soundFontBundle("SF2-BYTES"), true, dir); err != nil {
		t.Fatalf("EnsureSoundFont: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "OPERATORS-OWN" {
		t.Errorf("contents = %q, want the operator's file left alone", got)
	}
}

// TestEnsureSoundFont_NoBundle covers the -tags-less build: no embedded
// content at all, so there is nothing to place and nothing to complain about.
func TestEnsureSoundFont_NoBundle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")

	if err := EnsureSoundFont(nil, false, dir); err != nil {
		t.Fatalf("EnsureSoundFont: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("public dir was created for a bundle-less build (err=%v)", err)
	}
}

// TestEnsureSoundFont_BundleWithoutOne covers a bundle built before the
// SoundFont was added. Not an error — the operator can still supply the file
// by hand.
func TestEnsureSoundFont_BundleWithoutOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")
	bundle := fstest.MapFS{"pack/main_file_cache.dat": {Data: []byte("cache")}}

	if err := EnsureSoundFont(bundle, true, dir); err != nil {
		t.Fatalf("EnsureSoundFont: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, SoundFontName)); !os.IsNotExist(err) {
		t.Errorf("something was written for a bundle with no soundfont (err=%v)", err)
	}
}

// TestEnsureSoundFont_Idempotent pins that a second run over a directory this
// function itself populated is a no-op rather than a rewrite, so repeated
// starts do not churn a 3 MB file.
func TestEnsureSoundFont_Idempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")
	bundle := soundFontBundle("SF2-BYTES")

	if err := EnsureSoundFont(bundle, true, dir); err != nil {
		t.Fatalf("first EnsureSoundFont: %v", err)
	}
	path := filepath.Join(dir, SoundFontName)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := EnsureSoundFont(bundle, true, dir); err != nil {
		t.Fatalf("second EnsureSoundFont: %v", err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.ModTime().Equal(second.ModTime()) {
		t.Errorf("file was rewritten: mtime %v → %v", first.ModTime(), second.ModTime())
	}
}

// errFS yields a read error that is not fs.ErrNotExist, which is the one case
// EnsureSoundFont must report rather than treat as "this build has no
// SoundFont". A corrupt embedded bundle should not look like an absent one.
type errFS struct{ err error }

func (e errFS) Open(string) (fs.File, error) { return nil, e.err }

func TestEnsureSoundFont_PropagatesUnreadableBundle(t *testing.T) {
	dir := t.TempDir()
	err := EnsureSoundFont(errFS{err: errors.New("bundle truncated")}, true, dir)
	if err == nil {
		t.Fatal("EnsureSoundFont ignored an unreadable bundle; a corrupt bundle must not pass for an absent one")
	}
	if !strings.Contains(err.Error(), "bundle truncated") {
		t.Errorf("error %q does not wrap the underlying cause", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, SoundFontName)); statErr == nil {
		t.Error("a SoundFont was installed despite the read failing")
	}
}
