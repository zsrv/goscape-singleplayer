package content

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// StampName is the file inside an extracted content directory recording the
// pack digest that produced it.
const StampName = ".content-stamp"

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
// and race on the rename. Both orderings leave a complete, identically
// contented dir, so no locking is needed.
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
	if err := removeStale(parent, base); err != nil {
		return err
	}

	pid := os.Getpid()
	tmp := filepath.Join(parent, fmt.Sprintf("%s.tmp-%d", base, pid))
	old := filepath.Join(parent, fmt.Sprintf("%s.old-%d", base, pid))
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("content: clear %s: %w", tmp, err)
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
func removeStale(parent, base string) error {
	for _, pattern := range []string{base + ".tmp-*", base + ".old-*"} {
		matches, err := filepath.Glob(filepath.Join(parent, pattern))
		if err != nil {
			return fmt.Errorf("content: scan for stale %s: %w", pattern, err)
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil {
				return fmt.Errorf("content: remove stale %s: %w", m, err)
			}
		}
	}
	return nil
}

// writeTree copies every regular file in src to dst, streaming rather than
// buffering: the server pack is ~26 MB and main_file_cache.dat alone is 8 MB.
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
