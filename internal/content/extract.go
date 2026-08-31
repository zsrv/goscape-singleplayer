package content

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StampName is the file inside an extracted content directory recording the
// pack digest that produced it.
const StampName = ".content-stamp"

// staleForeignThreshold is how old a foreign process's tmp/old directory's
// modification time must be before removeStale treats it as debris from a
// crashed run rather than a live peer's in-flight extraction. Extracting the
// real ~39 MB bundle takes seconds, so an hour is generous headroom.
const staleForeignThreshold = time.Hour

// EnsureExtracted materializes src into dir, unless dir already carries a
// stamp equal to digest.
//
// The stamp is written last, so an interrupted extraction is never mistaken
// for a complete one: a temp tree without a stamp is simply never promoted.
// The swap renames the existing tree aside before renaming the new one into
// place, and only then deletes it — removing the live tree first would leave
// no cache at all if the second rename failed.
//
// Concurrent callers each extract into their own pid-suffixed temp directory
// and race on the install rename; the winner installs its tree, and the
// loser's rename fails because dir already exists. Rather than treat that as
// an error, the loser re-reads dir's stamp: if it now matches digest, the
// winner produced an identical result, so the loser discards its own temp
// tree and returns success instead of surfacing the rename failure. No
// locking is needed.
//
// Debris left by a crashed run is swept before a fresh extraction begins,
// but only once it is provably not
// in flight: this process's own leftovers are always removed, while a
// foreign process's tmp/old directory is removed only once it has sat
// untouched longer than any real extraction could take.
func EnsureExtracted(src fs.FS, dir, digest string) error {
	if digest == "" {
		return errors.New("content: refusing to extract without a pack digest (binary was not stamped at build time)")
	}
	if current, err := os.ReadFile(filepath.Join(dir, StampName)); err == nil {
		if strings.TrimSpace(string(current)) == digest {
			return nil
		}
	}

	parent, base := filepath.Dir(dir), filepath.Base(dir)
	pid := os.Getpid()
	tmp := filepath.Join(parent, fmt.Sprintf("%s.tmp-%d", base, pid))
	old := filepath.Join(parent, fmt.Sprintf("%s.old-%d", base, pid))
	if err := removeStale(parent, base, tmp, old); err != nil {
		return err
	}

	if err := writeTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, StampName), []byte(digest+"\n"), 0o644); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("content: write stamp: %w", err)
	}

	movedAside := false
	if err := os.Rename(dir, old); err == nil {
		movedAside = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("content: move existing %s aside: %w", dir, err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		if current, readErr := os.ReadFile(filepath.Join(dir, StampName)); readErr == nil {
			if strings.TrimSpace(string(current)) == digest {
				// A peer's install rename won the race; dir already holds an
				// identically contented tree. Discard our redundant one.
				if movedAside {
					_ = os.RemoveAll(old)
				}
				_ = os.RemoveAll(tmp)
				return nil
			}
		}
		if movedAside {
			_ = os.Rename(old, dir)
		}
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("content: install %s: %w", dir, err)
	}
	if movedAside {
		_ = os.RemoveAll(old)
	}
	return nil
}

// removeStale clears temp and aside directories left by a crashed run.
//
// ownTmp and ownOld — this process's own pid-suffixed directories — are
// always removed; no other process can be using them. A directory belonging
// to another pid is removed only when its modification time is older than
// staleForeignThreshold, since a live peer's in-flight extraction leaves an
// identically-shaped directory that must not be deleted out from under it.
// If the directory can't be stat'd, it is left alone rather than guessed at.
func removeStale(parent, base, ownTmp, ownOld string) error {
	if err := os.RemoveAll(ownTmp); err != nil {
		return fmt.Errorf("content: remove own stale %s: %w", ownTmp, err)
	}
	if err := os.RemoveAll(ownOld); err != nil {
		return fmt.Errorf("content: remove own stale %s: %w", ownOld, err)
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("content: scan %s for stale entries: %w", parent, err)
	}
	for _, prefix := range []string{base + ".tmp-", base + ".old-"} {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), prefix) {
				continue
			}
			m := filepath.Join(parent, e.Name())
			if m == ownTmp || m == ownOld {
				continue
			}
			info, err := os.Stat(m)
			if err != nil {
				continue // can't confirm it's not in flight; leave it alone
			}
			if time.Since(info.ModTime()) < staleForeignThreshold {
				continue // recent enough that a live peer may still be writing it
			}
			if err := os.RemoveAll(m); err != nil {
				return fmt.Errorf("content: remove stale %s: %w", m, err)
			}
		}
	}
	return nil
}

// writeTree copies every regular file in src to dst, streaming rather than
// buffering: the rev-225 pack is ~27 MB across client/ and server/.
func writeTree(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("content: walk %s: %w", p, err)
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("content: unexpected non-regular entry %s", p)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("content: mkdir for %s: %w", p, err)
		}
		in, err := src.Open(p)
		if err != nil {
			return fmt.Errorf("content: open %s: %w", p, err)
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("content: create %s: %w", target, err)
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return fmt.Errorf("content: write %s: %w", target, err)
		}
		return out.Close()
	})
}
