package content

import (
	"strings"
	"testing"
)

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
