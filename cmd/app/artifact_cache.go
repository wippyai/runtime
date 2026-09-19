// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
)

// seedCache stores the shipped packs and imports the verified immutable
// artifacts every retained deployment holds. Deployment locks and history stay
// untouched; only exact content-addressed files enter the cache. The cache
// layout, publication and verification belong to boot/deps/hub, which is also
// the component that reads the cache back at startup.
//
// The selected deployment is the one about to start, so content it pins must
// reach the cache and its failures end the launch. Every other retained
// deployment is an optimization source: a failure reading one is returned in
// skipped for the caller to report, and the remaining sources still contribute.
func seedCache(state, deployment string, bundle Bundle) (skipped []error, err error) {
	cache := cachePath(state)
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return nil, NewApplicationStateError("create artifact cache", cache, err)
	}
	for _, pack := range bundle.Packs {
		_, relative, err := immutableArtifactPath(pack.Module, pack.Version, pack.Digest)
		if err != nil {
			return nil, err
		}
		if err := hub.PublishImmutableArtifact(cache, relative, bytes.NewReader(pack.Data), pack.Digest, uint64(len(pack.Data))); err != nil {
			return nil, NewArtifactCacheError("cache shipped module", pack.Module+"@"+pack.Version, err)
		}
	}
	selected, retained, err := retainedDeployments(state, deployment)
	if err != nil {
		return nil, err
	}
	if err := importDeploymentArtifacts(selected, cache); err != nil {
		return nil, err
	}
	for _, root := range retained {
		if importErr := importDeploymentArtifacts(root, cache); importErr != nil {
			skipped = append(skipped, NewRetainedDeploymentError("retained deployment import stopped", root, importErr))
		}
	}
	return skipped, nil
}

// immutableArtifactPath names the cache entry a module record pins. A record
// that cannot be turned into an exact identity is corruption of the record, not
// a missing artifact: dropping it silently would leave the module to be
// downloaded at startup, which is the condition the cache exists to remove.
func immutableArtifactPath(module, version, digest string) (graph.Name, string, error) {
	name, err := graph.ParseName(module)
	if err != nil {
		return graph.Name{}, "", NewModuleArtifactError("module name is not in org/module form", module, version, err)
	}
	relative, err := hub.ImmutableWappRelativePath(name, version, digest)
	if err != nil {
		return graph.Name{}, "", NewModuleArtifactError("digest does not identify content", module, version, err)
	}
	return name, relative, nil
}

// retainedDeployments returns the absolute path of the selected deployment and
// every other deployment the state retains, each once.
func retainedDeployments(state, selected string) (string, []string, error) {
	selectedRoot, err := filepath.Abs(selected)
	if err != nil {
		return "", nil, NewApplicationStateError("resolve selected deployment", selected, err)
	}
	selectedRoot = filepath.Clean(selectedRoot)
	parent := deploymentsPath(state)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return selectedRoot, nil, nil
	}
	if err != nil {
		return "", nil, NewApplicationStateError("read retained deployments", parent, err)
	}
	retained := make([]string, 0, len(entries))
	seen := map[string]struct{}{selectedRoot: {}}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		root, err := filepath.Abs(filepath.Join(parent, entry.Name()))
		if err != nil {
			return "", nil, NewApplicationStateError("resolve retained deployment", entry.Name(), err)
		}
		root = filepath.Clean(root)
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		retained = append(retained, root)
	}
	return selectedRoot, retained, nil
}

func importDeploymentArtifacts(deployment, cache string) error {
	lockPath := filepath.Join(deployment, lock.DefaultFilename)
	lockInfo, err := os.Lstat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return NewApplicationStateError("inspect retained deployment lock", lockPath, err)
	}
	if lockInfo.Mode()&os.ModeSymlink != 0 || !lockInfo.Mode().IsRegular() {
		return NewRetainedDeploymentError("retained deployment lock is not a regular file", lockPath, nil)
	}
	locked, err := lock.New(lockPath)
	if err != nil {
		return NewApplicationStateError("read retained deployment lock", lockPath, err)
	}
	vendor := lock.ResolveLockPath(filepath.Dir(lockPath), locked.GetVendorPath())
	vendorRel, err := deploymentRelative(deployment, vendor)
	if err != nil {
		return err
	}
	deploymentRoot, err := os.OpenRoot(deployment)
	if err != nil {
		return NewApplicationStateError("open retained deployment", deployment, err)
	}
	defer func() { _ = deploymentRoot.Close() }()
	vendorRoot, err := deploymentRoot.OpenRoot(filepath.ToSlash(vendorRel))
	if err != nil {
		return NewApplicationStateError("open retained deployment vendor", vendor, err)
	}
	defer func() { _ = vendorRoot.Close() }()
	if err := importImmutableArtifacts(vendorRoot, cache); err != nil {
		return err
	}
	for _, module := range locked.GetModules() {
		// A row without a digest pins no content, so there is no immutable
		// identity to store it under. A row that carries a malformed digest or
		// name is a corrupt record and is reported.
		if module.Hash == "" {
			continue
		}
		name, exact, err := immutableArtifactPath(module.Name, module.Version, module.Hash)
		if err != nil {
			return err
		}
		legacy := filepath.ToSlash(lock.WappPath(name, module.Version))
		if _, err := vendorRoot.Lstat(legacy); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return NewApplicationStateError("inspect retained legacy artifact", legacy, err)
		}
		if err := copyRetainedArtifact(vendorRoot, legacy, cache, exact, module.Hash); err != nil {
			return NewArtifactCacheError("cache retained module", module.Name+"@"+module.Version, err)
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
		return NewApplicationStateError("read retained artifact vendor", "", err)
	}
	for _, org := range entries {
		if org.Type()&os.ModeSymlink != 0 {
			return NewRetainedDeploymentError("retained artifact vendor contains symlink", org.Name(), nil)
		}
		if !org.IsDir() {
			continue
		}
		if _, err := graph.ParseName(org.Name() + "/module"); err != nil {
			return NewRetainedDeploymentError("retained artifact organization is not a module organization", org.Name(), err)
		}
		orgRoot, err := vendor.OpenRoot(org.Name())
		if err != nil {
			return NewApplicationStateError("open retained artifact organization", org.Name(), err)
		}
		files, readErr := readRootDir(orgRoot, ".")
		_ = orgRoot.Close()
		if readErr != nil {
			return NewApplicationStateError("read retained artifact organization", org.Name(), readErr)
		}
		for _, file := range files {
			if file.Type()&os.ModeSymlink != 0 {
				return NewRetainedDeploymentError("retained artifact organization contains symlink", filepath.Join(org.Name(), file.Name()), nil)
			}
			if file.IsDir() {
				continue
			}
			digest, ok := hub.ImmutableWappDigest(file.Name())
			if !ok {
				continue
			}
			source := filepath.ToSlash(filepath.Join(org.Name(), file.Name()))
			if err := copyRetainedArtifact(vendor, source, cache, source, digest); err != nil {
				return NewArtifactCacheError("cache retained artifact", source, err)
			}
		}
	}
	return nil
}

// copyRetainedArtifact publishes a retained deployment file into the cache. The
// source is read through the retained deployment root, so a retained tree can
// never redirect the read outside itself.
func copyRetainedArtifact(source *os.Root, sourceRel, cache, cacheRel, digest string) error {
	info, err := source.Lstat(sourceRel)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return NewRetainedDeploymentError("retained artifact is not a regular file", sourceRel, nil)
	}
	input, err := source.Open(sourceRel)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	return hub.PublishImmutableArtifact(cache, cacheRel, input, digest, 0)
}

func readRootDir(root *os.Root, relative string) ([]os.DirEntry, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return file.ReadDir(-1)
}

// deploymentRelative confines a path a retained lock declares to the deployment
// that declares it.
func deploymentRelative(deployment, target string) (string, error) {
	relative, err := filepath.Rel(filepath.Clean(deployment), filepath.Clean(target))
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", NewRetainedDeploymentError("retained deployment vendor path escapes deployment", target, err)
	}
	return relative, nil
}
