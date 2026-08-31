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
