// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	depconfig "github.com/wippyai/runtime/boot/deps/config"
)

const treeIdentityModeMask = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// digestDirectoryTree identifies the complete loadable tree, not only file
// bytes. Lexical WalkDir order plus length-framed records makes the digest
// deterministic and unambiguous. Empty directories and executable/permission
// changes are significant; unstable or unsupported node types fail closed.
func digestDirectoryTree(root string) (string, uint64, error) {
	return digestDirectoryTreeFiltered(root, nil)
}

// digestReplacementTree applies the same source exclusions used when loading
// entries from a replacement tree.
func digestReplacementTree(root string) (string, uint64, error) {
	cfg, err := depconfig.Load(root)
	if err != nil {
		return digestDirectoryTree(root)
	}
	return digestDirectoryTreeFiltered(root, cfg.ExcludesSourcePath)
}

func digestDirectoryTreeFiltered(root string, excluded func(string) bool) (string, uint64, error) {
	hash := sha256.New()
	var total uint64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() && rel != "." && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if rel == extractedModuleMeta {
			return nil
		}
		if rel != "." && excluded != nil && excluded(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("module tree contains symlink %q", rel)
		}
		switch {
		case mode.IsDir():
			return writeTreeDigestRecord(hash, 'd', rel, mode, 0)
		case mode.IsRegular():
			if err := writeTreeDigestRecord(hash, 'f', rel, mode, info.Size()); err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			total += uint64(info.Size())
			return nil
		default:
			return fmt.Errorf("module tree contains unsupported file type %q (%s)", rel, mode.Type())
		}
	})
	if err != nil {
		return "", 0, err
	}
	return "sha256-tree-v1:" + hex.EncodeToString(hash.Sum(nil)), total, nil
}

func writeTreeDigestRecord(dst io.Writer, kind byte, rel string, mode fs.FileMode, size int64) error {
	_, err := fmt.Fprintf(dst, "%c:%d:%s:%o:%d:", kind, len(rel), rel, uint32(mode&treeIdentityModeMask), size)
	return err
}
