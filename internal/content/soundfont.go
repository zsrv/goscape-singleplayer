package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SoundFontName is the file the client asks the ondemand static root for.
// goscape-client fetches it as the relative URL "SCC1_Florestan.sf2"
// (pkg/jagex2/sound/audio/soundfont.go), so the name must match exactly or
// music is silent.
const SoundFontName = "SCC1_Florestan.sf2"

// soundFontBundlePath is where `make embed-pack` installs the SoundFont inside
// the bundle. Its own member rather than raw/ beside wordenc: raw/ means
// engine-owned raw jagfiles, the SoundFont is not one, and rev-225's layout
// has no raw/ member at all — a public/ member names what the file is for and
// is the same on every revision branch.
const soundFontBundlePath = "public/" + SoundFontName

// EnsureSoundFont places the embedded SoundFont in publicDir so the ondemand
// static root can serve it.
//
// The client fetches the SoundFont over HTTP from the server this binary also
// runs, rather than reading it off disk, because it is a port of an applet
// whose signlink.openurl went through the applet code base. That indirection
// is why a missing file surfaces as one log line and silent music rather than
// as a startup error, and why placing it is worth doing eagerly here.
//
// Deliberately does nothing in three cases, none of them an error:
//
//   - no embedded bundle (a build without -tags embedcache), where there is no
//     SoundFont to place and publicDir may not be this binary's to create;
//   - a bundle that carries none, which covers any bundle built before the
//     SoundFont was added;
//   - publicDir already holding a file by that name, which is an operator's
//     deliberate choice and outranks ours.
//
// Writes via a temp file and rename so a crash cannot leave a partial 3 MB
// SoundFont for the client to fetch and fail to parse.
func EnsureSoundFont(bundle fs.FS, haveBundle bool, publicDir string) error {
	if !haveBundle || bundle == nil {
		return nil
	}

	data, err := fs.ReadFile(bundle, soundFontBundlePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("content: reading embedded soundfont: %w", err)
	}

	dst := filepath.Join(publicDir, SoundFontName)
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("content: checking for %s: %w", dst, err)
	}

	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		return fmt.Errorf("content: creating public dir: %w", err)
	}

	tmp, err := os.CreateTemp(publicDir, "."+SoundFontName+".tmp-*")
	if err != nil {
		return fmt.Errorf("content: creating temp soundfont: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("content: writing soundfont: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("content: closing soundfont: %w", err)
	}
	// 0644: the ondemand file server only reads it, but the public dir is
	// operator-facing and the file is meant to be replaceable by hand.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("content: chmod soundfont: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("content: installing soundfont: %w", err)
	}
	return nil
}
