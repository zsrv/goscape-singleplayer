// Package build reports how this binary was built. The Version, Revision,
// Branch, BuildUser and BuildDate values are injected at link time by the
// Makefile and the release workflow (-ldflags -X); a plain `go build` leaves
// them empty, in which case they render as "unknown". This mirrors
// goscape-client's pkg/util/build so the whole repo family reports the same
// shape.
package build

import (
	"cmp"
	"fmt"
	"runtime"
)

var (
	Version   string
	Revision  string
	Branch    string
	BuildUser string
	BuildDate string
	GoVersion string
)

func init() { GoVersion = runtime.Version() }

// Info returns the build metadata as a multi-line, human-readable block,
// suitable for printing behind -version.
func Info() string {
	return fmt.Sprintf(`goscape-singleplayer
  version:     %s
  revision:    %s
  branch:      %s
  go version:  %s
  build user:  %s
  build date:  %s`,
		cmp.Or(Version, "unknown"),
		cmp.Or(Revision, "unknown"),
		cmp.Or(Branch, "unknown"),
		GoVersion,
		cmp.Or(BuildUser, "unknown"),
		cmp.Or(BuildDate, "unknown"),
	)
}
