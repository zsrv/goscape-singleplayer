//go:build !embedcache

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
