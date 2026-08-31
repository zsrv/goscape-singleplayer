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
// that it compiles. client/config and client/wordenc are present in both the
// few-KB CI fixture bundle and a real rev-225 pack, so this test stays true
// for a genuine release build too.
func TestBundlePresentWithBuildTag(t *testing.T) {
	fsys, ok := Bundle()
	if !ok {
		t.Fatal("Bundle() reported no bundle in a -tags embedcache build")
	}
	if fsys == nil {
		t.Fatal("Bundle() returned ok=true with a nil FS")
	}

	for _, path := range []string{"pack/client/config", "pack/client/wordenc"} {
		if _, err := fsys.Open(path); err != nil {
			t.Errorf("Bundle() FS missing %q: %v", path, err)
		}
	}

	got := Info()
	if !strings.Contains(got, "embedded:  yes") {
		t.Errorf("tagged build should report embedded: yes:\n%s", got)
	}
}
