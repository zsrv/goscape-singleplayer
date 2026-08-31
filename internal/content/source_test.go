// internal/content/source_test.go
//go:build !embedcache

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
