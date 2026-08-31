package content

import (
	"io/fs"

	"github.com/zsrv/goscape-singleplayer/internal/content/embedded"
)

// Bundle returns the content compiled into this binary, if any. The returned
// FS is rooted at the bundle: "pack" is the packed cache, and on rev-244+
// "raw/wordenc" is the chat word filter.
func Bundle() (fs.FS, bool) { return embedded.Bundle() }
