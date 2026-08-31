//go:build embedcache

package embedded

import (
	"embed"
	"io/fs"
)

// bundle holds the packed game cache and, on rev-244+, the raw wordenc
// jagfile. It is produced by `make embed-pack` and is NOT tracked in git.
// Building with -tags embedcache and no bundle/ directory fails here, at
// compile time, which is the intended failure: better than a binary that
// silently ships an empty cache.
//
//go:embed all:bundle
var bundle embed.FS

// Bundle returns the embedded content rooted at the bundle directory, so its
// entries are "pack" and (rev-244+) "raw".
func Bundle() (fs.FS, bool) {
	sub, err := fs.Sub(bundle, "bundle")
	if err != nil {
		// Unreachable: the //go:embed above would have failed the build.
		panic("content: embedded bundle is malformed: " + err.Error())
	}
	return sub, true
}
