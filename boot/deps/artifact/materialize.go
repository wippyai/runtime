// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Materialize validates a resource with its format and transactionally mirrors
// it below root at the format-derived path.
func Materialize(
	ctx context.Context,
	registry *Registry,
	declaration Declaration,
	input InspectInput,
	root string,
) (Descriptor, string, error) {
	descriptor, err := registry.Inspect(ctx, declaration, input)
	if err != nil {
		return Descriptor{}, "", err
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return Descriptor{}, "", fmt.Errorf("resolve artifact root: %w", err)
	}
	if err := ensureMaterializationRoot(rootAbs); err != nil {
		return Descriptor{}, "", err
	}
	destination := filepath.Join(rootAbs, filepath.FromSlash(descriptor.RelativePath))
	if err := ensureWithinRoot(rootAbs, destination); err != nil {
		return Descriptor{}, "", err
	}
	unlock, err := acquireArtifactLock(ctx, rootAbs)
	if err != nil {
		return Descriptor{}, "", err
	}
	if err := exactMirror(input.Filesystem, rootAbs, destination); err != nil {
		return Descriptor{}, "", fmt.Errorf(
			"materialize %s: %w",
			input.ResourceID.String(),
			errors.Join(err, unlock()),
		)
	}
	if err := unlock(); err != nil {
		return Descriptor{}, "", fmt.Errorf("unlock artifact root: %w", err)
	}
	return descriptor, destination, nil
}

func ensureWithinRoot(root, destination string) error {
	relative, err := filepath.Rel(root, destination)
	if err != nil {
		return fmt.Errorf("resolve artifact destination: %w", err)
	}
	if relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("artifact destination escapes the materialization root")
	}
	return nil
}

func ensureMaterializationRoot(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fmt.Errorf("create artifact root: %w", err)
		}
		info, err = os.Lstat(root)
	}
	if err != nil {
		return fmt.Errorf("inspect artifact root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("artifact root must not be a symlink")
	}
	if !info.IsDir() {
		return errors.New("artifact root must be a directory")
	}
	return nil
}

func ensureDirectoryBelowRoot(root, directory string) error {
	if filepath.Clean(root) == filepath.Clean(directory) {
		return nil
	}
	if err := ensureWithinRoot(root, directory); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return fmt.Errorf("resolve artifact directory: %w", err)
	}
	current := root
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := os.Mkdir(current, 0o755); mkdirErr != nil &&
				!errors.Is(mkdirErr, os.ErrExist) {
				return fmt.Errorf("create artifact directory: %w", mkdirErr)
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return fmt.Errorf("inspect artifact directory: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact directory %q is a symlink", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("artifact directory %q is not a directory", current)
		}
	}
	return nil
}

func exactMirror(source fs.FS, root, destination string) error {
	parent := filepath.Dir(destination)
	if err := ensureDirectoryBelowRoot(root, parent); err != nil {
		return fmt.Errorf("prepare destination parent: %w", err)
	}

	stage, err := os.MkdirTemp(parent, "."+filepath.Base(destination)+".stage-*")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	stageActive := true
	defer func() {
		if stageActive {
			_ = os.RemoveAll(stage)
		}
	}()

	if err := copyTree(source, stage); err != nil {
		return err
	}

	backup, err := os.MkdirTemp(parent, "."+filepath.Base(destination)+".backup-*")
	if err != nil {
		return fmt.Errorf("reserve backup path: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("prepare backup path: %w", err)
	}
	hadDestination := false
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("stage existing destination: %w", err)
		}
		hadDestination = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat destination: %w", err)
	}

	if err := os.Rename(stage, destination); err != nil {
		if hadDestination {
			if restoreErr := os.Rename(backup, destination); restoreErr != nil {
				return fmt.Errorf(
					"activate staged artifact: %w (restore previous destination: %w)",
					err,
					restoreErr,
				)
			}
		}
		return fmt.Errorf("activate staged artifact: %w", err)
	}
	stageActive = false
	if hadDestination {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove artifact backup: %w", err)
		}
	}
	return nil
}

func copyTree(source fs.FS, destination string) error {
	portablePaths := make(map[string]string)
	return fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if err := validatePortablePath(path); err != nil {
			return fmt.Errorf("invalid resource path %q: %w", path, err)
		}
		portableKey := strings.ToLower(path)
		if previous, exists := portablePaths[portableKey]; exists && previous != path {
			return fmt.Errorf("resource paths %q and %q collide on case-insensitive filesystems", previous, path)
		}
		portablePaths[portableKey] = path
		target := filepath.Join(destination, filepath.FromSlash(path))
		if err := ensureWithinRoot(destination, target); err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink %q is not allowed", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file %q is not allowed", path)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := source.Open(path)
		if err != nil {
			return fmt.Errorf("open %q: %w", path, err)
		}

		dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = src.Close()
			return fmt.Errorf("create %q: %w", path, err)
		}
		_, copyErr := io.Copy(dst, src)
		sourceCloseErr := src.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return fmt.Errorf("copy %q: %w", path, copyErr)
		}
		if sourceCloseErr != nil {
			return fmt.Errorf("close source %q: %w", path, sourceCloseErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %q: %w", path, closeErr)
		}
		if err := os.Chmod(target, 0o644); err != nil { //nolint:gosec // Artifacts are shared source files, not secrets.
			return fmt.Errorf("set permissions on %q: %w", path, err)
		}
		return nil
	})
}

func digestTree(source fs.FS) ([sha256.Size]byte, error) {
	digest := sha256.New()
	portablePaths := make(map[string]string)
	err := fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if err := validatePortablePath(path); err != nil {
			return fmt.Errorf("invalid resource path %q: %w", path, err)
		}
		portableKey := strings.ToLower(path)
		if previous, exists := portablePaths[portableKey]; exists && previous != path {
			return fmt.Errorf("resource paths %q and %q collide on case-insensitive filesystems", previous, path)
		}
		portablePaths[portableKey] = path

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink %q is not allowed", path)
		}
		if entry.IsDir() {
			writeDigestField(digest, "directory")
			writeDigestField(digest, path)
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file %q is not allowed", path)
		}
		writeDigestField(digest, "file")
		writeDigestField(digest, path)
		writeDigestField(digest, strconv.FormatInt(info.Size(), 10))

		file, err := source.Open(path)
		if err != nil {
			return fmt.Errorf("open %q: %w", path, err)
		}
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("hash %q: %w", path, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %q: %w", path, closeErr)
		}
		return nil
	})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func writeDigestField(digest hash.Hash, value string) {
	_, _ = digest.Write([]byte(strconv.Itoa(len(value))))
	_, _ = digest.Write([]byte{':'})
	_, _ = digest.Write([]byte(value))
}
