//go:build !embedcache

// Package embedded carries the game content bundle when the binary is built
// with -tags embedcache, and nothing at all otherwise. Keeping the //go:embed
// directive behind a build tag is what lets a plain `go build` ignore the
// (gitignored, usually absent) bundle directory entirely.
package embedded

import "io/fs"

// Bundle reports that this build carries no content.
func Bundle() (fs.FS, bool) { return nil, false }
