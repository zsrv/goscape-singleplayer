package build

import (
	"strings"
	"testing"
)

func TestInfoRendersUnknownForUnstampedFields(t *testing.T) {
	got := Info()
	for _, want := range []string{"goscape-singleplayer", "version:", "unknown"} {
		if !strings.Contains(got, want) {
			t.Errorf("Info() missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "go version:") || strings.Contains(got, "go version:  unknown") {
		t.Errorf("Info() should always report a real Go version:\n%s", got)
	}
}

func TestInfoUsesStampedValues(t *testing.T) {
	Version, Revision, Branch = "rev274-v1.0.0", "abc1234", "rev-274"
	t.Cleanup(func() { Version, Revision, Branch = "", "", "" })

	got := Info()
	for _, want := range []string{"rev274-v1.0.0", "abc1234", "rev-274"} {
		if !strings.Contains(got, want) {
			t.Errorf("Info() missing stamped %q:\n%s", want, got)
		}
	}
}
