// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
)

// ContentDigestPrefix names the digest scheme ContentDigest produces.
const ContentDigestPrefix = "sha256-content-v1:"

// ContentDigest computes the aggregate content digest of an embedded
// filesystem: every file path and its bytes, length-framed, in sorted path
// order. It changes exactly when the served content changes, independent of
// mtimes or how a pack writer chunks or compresses the data, so a directory
// digested at pack time and the same resource read back from a pack produce
// the same value.
func ContentDigest(fsys fs.FS) (string, error) {
	var paths []string
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(paths)

	hash := sha256.New()
	for _, path := range paths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%d:%s:%d:", len(path), path, len(data))
		_, _ = hash.Write(data)
	}
	return ContentDigestPrefix + hex.EncodeToString(hash.Sum(nil)), nil
}
