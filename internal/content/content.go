// Package content reports where this binary's game content came from, and —
// when built with -tags embedcache — carries and extracts it.
//
// Repo, Branch, Commit and PackDigest are injected at link time by the
// Makefile and the release workflow. PackDigest doubles as the extraction
// stamp, which is why it is computed once at pack time rather than hashed at
// startup.
package content

import (
	"cmp"
	"fmt"
)

var (
	Repo       string
	Branch     string
	Commit     string
	PackDigest string
)

// Info returns the content provenance block printed under -version.
func Info() string {
	embedded := "no"
	if _, ok := Bundle(); ok {
		embedded = "yes"
	}
	return fmt.Sprintf(`  content:
    repo:      %s
    branch:    %s
    commit:    %s
    pack:      %s
    embedded:  %s`,
		cmp.Or(Repo, "unknown"),
		cmp.Or(Branch, "unknown"),
		cmp.Or(Commit, "unknown"),
		cmp.Or(PackDigest, "unknown"),
		embedded,
	)
}
