// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
)

const artifactCacheDirectory = "artifact-cache"

func dependencyVendorDirectory(state string) string {
	return filepath.Join(state, artifactCacheDirectory, "vendor")
}

// seedDependencyCache stores embedded packs and imports verified immutable
// artifacts from retained deployment vendors. Deployment locks and history are
// left untouched; only exact content-addressed files enter the stable vendor.
func seedDependencyCache(state, deployment string, bundle Bundle) error {
	if err := bundle.validate(); err != nil {
		return err
	}
	cache := dependencyVendorDirectory(state)
	stateRoot, err := os.OpenRoot(state)
	if err != nil {
		return fmt.Errorf("open application state: %w", err)
	}
	if err := stateRoot.MkdirAll(filepath.ToSlash(filepath.Join(artifactCacheDirectory, "vendor")), 0o700); err != nil {
		stateRoot.Close()
		return fmt.Errorf("create dependency artifact cache: %w", err)
	}
	stateRoot.Close()
	for _, pack := range bundle.Packs {
		name, _ := graph.ParseName(pack.Module)
		relative := immutableRelativePath(name, pack.Version, pack.Digest)
		if relative == "" {
			return fmt.Errorf("invalid embedded artifact digest for %s", pack.Module)
		}
		if err := publishBytes(cache, relative, pack.Data, pack.Digest, uint64(len(pack.Data))); err != nil {
			return fmt.Errorf("cache embedded module %s@%s: %w", pack.Module, pack.Version, err)
		}
	}
	roots, err := retainedDeploymentRoots(state, deployment, bundle)
	if err != nil {
		return err
	}
	for _, root := range roots {
		if err := importDeploymentArtifacts(root, cache); err != nil {
			return err
		}
	}
	return nil
}

func retainedDeploymentRoots(state, selected string, bundle Bundle) ([]string, error) {
	roots := make([]string, 0, 8)
	seen := make(map[string]struct{})
	add := func(root string) error {
		if root == "" {
			return nil
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		absolute = filepath.Clean(absolute)
		if _, exists := seen[absolute]; !exists {
			seen[absolute] = struct{}{}
			roots = append(roots, absolute)
		}
		return nil
	}
	if err := add(selected); err != nil {
		return nil, err
	}
	if err := add(embeddedDeployment(state, bundle)); err != nil {
		return nil, err
	}
	if active, err := selectedDeployment(state); err == nil {
		if err := add(active); err != nil {
			return nil, err
		}
	}
	for _, parent := range []string{filepath.Join(state, "base"), filepath.Join(state, "revisions")} {
		entries, err := os.ReadDir(parent)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read retained deployments: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			root := filepath.Join(parent, entry.Name())
			if filepath.Base(parent) == "revisions" {
				root = filepath.Join(root, "deployment")
			}
			if err := add(root); err != nil {
				return nil, err
			}
		}
	}
	return roots, nil
}

func importDeploymentArtifacts(deployment, cache string) error {
	lockPath := filepath.Join(deployment, lock.DefaultFilename)
	lockInfo, err := os.Lstat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect retained deployment lock %s: %w", lockPath, err)
	}
	if lockInfo.Mode()&os.ModeSymlink != 0 || !lockInfo.Mode().IsRegular() {
		return fmt.Errorf("retained deployment lock is not a regular file: %s", lockPath)
	}
	locked, err := lock.New(lockPath)
	if err != nil {
		return fmt.Errorf("read retained deployment lock %s: %w", lockPath, err)
	}
	vendor := lock.ResolveLockPath(filepath.Dir(lockPath), locked.GetVendorPath())
	if !pathWithin(deployment, vendor) {
		return fmt.Errorf("retained deployment vendor path escapes deployment: %s", vendor)
	}
	deploymentRoot, err := os.OpenRoot(deployment)
	if err != nil {
		return err
	}
	defer deploymentRoot.Close()
	vendorRel, err := filepath.Rel(deployment, vendor)
	if err != nil || filepath.IsAbs(vendorRel) || strings.HasPrefix(vendorRel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("retained deployment vendor path escapes deployment")
	}
	vendorRoot, err := deploymentRoot.OpenRoot(filepath.ToSlash(vendorRel))
	if err != nil {
		return fmt.Errorf("open retained deployment vendor: %w", err)
	}
	defer vendorRoot.Close()
	if err := importImmutableArtifacts(vendorRoot, cache); err != nil {
		return err
	}
	for _, module := range locked.GetModules() {
		digest, err := sha256Digest(module.Hash)
		if err != nil {
			continue
		}
		name, err := graph.ParseName(module.Name)
		if err != nil {
			continue
		}
		relative := filepath.ToSlash(filepath.Join(name.Organization, name.Module+"-"+module.Version+".wapp"))
		if _, err := vendorRoot.Lstat(relative); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect retained legacy artifact: %w", err)
		}
		exact := immutableRelativePath(name, module.Version, digest)
		if exact == "" {
			continue
		}
		if err := copyArtifact(vendorRoot, relative, cache, exact, digest); err != nil {
			return fmt.Errorf("cache retained module %s@%s: %w", module.Name, module.Version, err)
		}
	}
	return nil
}

func importImmutableArtifacts(vendor *os.Root, cache string) error {
	entries, err := readRootDir(vendor, ".")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, org := range entries {
		if org.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("retained artifact vendor contains symlink: %s", org.Name())
		}
		if !org.IsDir() {
			continue
		}
		if _, err := graph.ParseName(org.Name() + "/module"); err != nil {
			return fmt.Errorf("invalid retained artifact organization %s", org.Name())
		}
		orgRoot, err := vendor.OpenRoot(org.Name())
		if err != nil {
			return err
		}
		files, readErr := readRootDir(orgRoot, ".")
		orgRoot.Close()
		if readErr != nil {
			return readErr
		}
		for _, file := range files {
			if file.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("retained artifact organization contains symlink: %s", file.Name())
			}
			if file.IsDir() {
				continue
			}
			digest, ok := immutableFilenameDigest(file.Name())
			if !ok {
				continue
			}
			source := filepath.ToSlash(filepath.Join(org.Name(), file.Name()))
			if err := copyArtifact(vendor, source, cache, source, digest); err != nil {
				return fmt.Errorf("cache retained artifact %s: %w", source, err)
			}
		}
	}
	return nil
}

func immutableFilenameDigest(filename string) (string, bool) {
	const marker = ".sha256-"
	if !strings.HasSuffix(filename, ".wapp") {
		return "", false
	}
	base := strings.TrimSuffix(filename, ".wapp")
	index := strings.LastIndex(base, marker)
	if index <= 0 {
		return "", false
	}
	digest, err := sha256Digest(base[index+len(marker):])
	return digest, err == nil
}

func immutableRelativePath(name graph.Name, version, digest string) string {
	digest, err := sha256Digest(digest)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(filepath.Join(name.Organization, name.Module+"-"+version+".sha256-"+digest+".wapp"))
}

func sha256Digest(raw string) (string, error) {
	value := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(raw), "sha256:"))
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("invalid sha256 artifact digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("invalid sha256 artifact digest: %w", err)
	}
	return value, nil
}

func publishBytes(root, relative string, data []byte, digest string, size uint64) error {
	destination, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer destination.Close()
	if err := destination.MkdirAll(filepath.ToSlash(filepath.Dir(relative)), 0o700); err != nil {
		return err
	}
	if err := verifyRootArtifact(destination, relative, digest, size); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return publishArtifact(destination, relative, bytes.NewReader(data), digest, size)
}

func publishArtifact(root *os.Root, relative string, input io.Reader, digest string, size uint64) error {
	tmp, tmpRel, err := createRootTemp(root, filepath.Dir(relative), "artifact")
	if err != nil {
		return err
	}
	hash := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(tmp, hash), input)
	if copyErr != nil {
		tmp.Close()
		_ = root.Remove(tmpRel)
		return copyErr
	}
	want, digestErr := sha256Digest(digest)
	if digestErr != nil || hex.EncodeToString(hash.Sum(nil)) != want || (size > 0 && uint64(count) != size) {
		tmp.Close()
		_ = root.Remove(tmpRel)
		return fmt.Errorf("artifact content does not match its immutable identity")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = root.Remove(tmpRel)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = root.Remove(tmpRel)
		return err
	}
	if err := root.Link(tmpRel, relative); err != nil {
		if verifyErr := verifyRootArtifact(root, relative, digest, size); verifyErr == nil {
			_ = root.Remove(tmpRel)
			return nil
		}
		_ = root.Remove(tmpRel)
		return err
	}
	return root.Remove(tmpRel)
}

func copyArtifact(source *os.Root, sourceRel, destination, destinationRel, digest string) error {
	destinationRoot, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer destinationRoot.Close()
	if err := destinationRoot.MkdirAll(filepath.ToSlash(filepath.Dir(destinationRel)), 0o700); err != nil {
		return err
	}
	if err := verifyRootArtifact(destinationRoot, destinationRel, digest, 0); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info, err := source.Lstat(sourceRel); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("retained artifact is not a regular file")
	}
	input, err := source.Open(sourceRel)
	if err != nil {
		return err
	}
	defer input.Close()
	return publishArtifact(destinationRoot, destinationRel, input, digest, 0)
}

func verifyRootArtifact(root *os.Root, relative, digest string, expectedSize uint64) error {
	info, err := root.Lstat(relative)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("artifact is not a regular file")
	}
	file, err := root.Open(relative)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	want, err := sha256Digest(digest)
	if err != nil || hex.EncodeToString(hash.Sum(nil)) != want {
		return fmt.Errorf("artifact digest mismatch")
	}
	if expectedSize > 0 && uint64(info.Size()) != expectedSize {
		return fmt.Errorf("artifact size mismatch")
	}
	return nil
}

func verifyCachedPath(root, relative, digest string, size uint64) error {
	handle, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer handle.Close()
	return verifyRootArtifact(handle, relative, digest, size)
}

func createRootTemp(handle *os.Root, directory, prefix string) (*os.File, string, error) {
	for counter := 0; counter < 32; counter++ {
		relative := filepath.ToSlash(filepath.Join(directory, fmt.Sprintf(".%s-%d", prefix, counter)))
		file, err := handle.OpenFile(relative, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return file, relative, nil
	}
	return nil, "", fmt.Errorf("create private artifact file: too many concurrent writers")
}

func readRootDir(root *os.Root, relative string) ([]os.DirEntry, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.ReadDir(-1)
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
