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

const artifactCacheDirectory = "artifact-cache"

func dependencyVendorDirectory(state string) string {
	return filepath.Join(state, artifactCacheDirectory, "vendor")
}

// seedDependencyCache stores embedded packs and imports verified immutable
// artifacts from retained deployment vendors. Deployment locks and history are
// left untouched; only exact content-addressed files enter the stable vendor.
// The cache layout, publication and verification belong to boot/deps/hub, which
// is also the component that reads the cache back at startup.
//
// The selected deployment is the one about to start, so content it pins must
// reach the cache and its failures end the launch. Every other retained
// deployment is an optimization source: a failure reading one is returned in
// skipped for the caller to report, and the remaining sources still contribute.
func seedDependencyCache(state, deployment string, bundle Bundle) (skipped []error, err error) {
	cache := dependencyVendorDirectory(state)
	stateRoot, err := os.OpenRoot(state)
	if err != nil {
		return nil, NewApplicationStateError("open application state", state, err)
	}
	mkdirErr := stateRoot.MkdirAll(filepath.ToSlash(filepath.Join(artifactCacheDirectory, "vendor")), 0o700)
	if err := errors.Join(mkdirErr, stateRoot.Close()); err != nil {
		return nil, NewApplicationStateError("create dependency artifact cache", cache, err)
	}
	for _, pack := range bundle.Packs {
		_, relative, err := immutableArtifactPath(pack.Module, pack.Version, pack.Digest)
		if err != nil {
			return nil, err
		}
		if err := hub.PublishImmutableArtifact(cache, relative, bytes.NewReader(pack.Data), pack.Digest, uint64(len(pack.Data))); err != nil {
			return nil, NewArtifactCacheError("cache embedded module", pack.Module+"@"+pack.Version, err)
		}
	}
	selected, retained, err := retainedDeploymentRoots(state, deployment, bundle)
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

// retainedDeploymentRoots returns the absolute path of the selected deployment
// and every other retained deployment the state directory holds, each once.
func retainedDeploymentRoots(state, selected string, bundle Bundle) (string, []string, error) {
	selectedRoot, err := filepath.Abs(selected)
	if err != nil {
		return "", nil, err
	}
	selectedRoot = filepath.Clean(selectedRoot)
	retained := make([]string, 0, 8)
	seen := map[string]struct{}{selectedRoot: {}}
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
			retained = append(retained, absolute)
		}
		return nil
	}
	if err := add(embeddedDeployment(state, bundle)); err != nil {
		return "", nil, err
	}
	active, err := selectedDeployment(state)
	if err != nil {
		return "", nil, NewApplicationStateError("read application activation record", state, err)
	}
	if err := add(active); err != nil {
		return "", nil, err
	}
	for _, parent := range []string{filepath.Join(state, "base"), filepath.Join(state, "revisions")} {
		entries, err := os.ReadDir(parent)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", nil, NewApplicationStateError("read retained deployments", parent, err)
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
				return "", nil, err
			}
		}
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
	defer deploymentRoot.Close()
	vendorRoot, err := deploymentRoot.OpenRoot(filepath.ToSlash(vendorRel))
	if err != nil {
		return NewApplicationStateError("open retained deployment vendor", vendor, err)
	}
	defer vendorRoot.Close()
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
		orgRoot.Close()
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
	defer input.Close()
	return hub.PublishImmutableArtifact(cache, cacheRel, input, digest, 0)
}

func readRootDir(root *os.Root, relative string) ([]os.DirEntry, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
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
