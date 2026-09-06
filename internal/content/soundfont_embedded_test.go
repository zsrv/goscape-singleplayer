//go:build embedcache

package content

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEmbeddedBundleCarriesSoundFont is the tagged counterpart to the MapFS
// tests in soundfont_test.go: those pin the copying logic, this one pins that
// a real -tags embedcache build actually has something to copy. Without it
// the unit tests would keep passing while `make embed-pack` quietly stopped
// installing the file, and the only symptom would be silent music.
//
// Both the CI fixture bundle and a genuine pack carry raw/SCC1_Florestan.sf2,
// so this holds for a release build too.
func TestEmbeddedBundleCarriesSoundFont(t *testing.T) {
	fsys, ok := Bundle()
	if !ok {
		t.Fatal("Bundle() reported no bundle in a -tags embedcache build")
	}

	dir := filepath.Join(t.TempDir(), "public")
	if err := EnsureSoundFont(fsys, true, dir); err != nil {
		t.Fatalf("EnsureSoundFont over the embedded bundle: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, SoundFontName))
	if err != nil {
		t.Fatalf("embedded bundle did not yield %s — did make embed-pack stop "+
			"installing it? %v", SoundFontName, err)
	}
	if info.Size() == 0 {
		t.Errorf("%s materialized empty", SoundFontName)
	}
}
