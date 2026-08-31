package content

import (
	"io/fs"
	"path/filepath"
)

// ResolveCacheDir decides which directory the server should read its cache
// from, extracting the embedded bundle if that is the answer.
//
// Precedence:
//  1. an explicitly passed --cache-dir wins and is used verbatim;
//  2. otherwise an embedded bundle is extracted to <dataDir>/content and its
//     pack subdirectory is used;
//  3. otherwise the --cache-dir default is returned unchanged, and the
//     caller's existing "no packed cache" check reports the problem.
//
// Extracting to <dataDir>/content is what keeps main's wordenc default
// working untouched: with the cache at <dataDir>/content/pack, the existing
// <cache-dir>/../raw/wordenc derivation lands on <dataDir>/content/raw/wordenc.
func ResolveCacheDir(explicit bool, cacheDir, dataDir string, bundle fs.FS, digest string) (string, error) {
	if explicit || bundle == nil {
		return cacheDir, nil
	}
	if entries, err := fs.ReadDir(bundle, "."); err != nil || len(entries) == 0 {
		return cacheDir, nil
	}
	dir := filepath.Join(dataDir, "content")
	if err := EnsureExtracted(bundle, dir, digest); err != nil {
		return "", err
	}
	return filepath.Join(dir, "pack"), nil
}
