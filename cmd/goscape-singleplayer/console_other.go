//go:build !windows

package main

import "os"

// No other platform has Windows' notion of a subsystem field that decides
// whether the loader opens a console, so there is nothing to attach to,
// allocate or hide: stdio is whatever the launcher handed the process, and
// -console is inert (main reports that rather than ignoring it — see
// consoleFlagInert).
func setupConsole(bool) bool { return true }

// redirectStdio is unreachable today, because setupConsole above never reports
// unusable stdio. It exists as the Go-level half of the Windows implementation,
// and as the place the same fix would go for a macOS .app bundle, where Finder
// starts the executable with no terminal attached.
//
// Unlike the Windows version it cannot redirect the runtime's own fd 1 and
// fd 2 writes — panic traces — because that needs dup2, and pulling in
// x/sys/unix for a path nothing currently takes is not a trade worth making.
func redirectStdio(f *os.File) { os.Stdout, os.Stderr = f, f }
