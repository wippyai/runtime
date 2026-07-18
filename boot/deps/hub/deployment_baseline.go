// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
)

// deploymentBaselineDigest binds a durable dependency resolution to the
// deployment graph it was composed over. Registry history is an overlay on the
// current deployment; it must never make an old deployment graph authoritative
// after the lock or source-owned roots change.
func (h *DependencyHandler) deploymentBaselineDigest(
	ctx context.Context,
	baseline regapi.State,
	transcoder payload.Transcoder,
) (string, error) {
	type lockedModule struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Digest      string `json:"digest,omitempty"`
		Root        bool   `json:"root,omitempty"`
		Replacement bool   `json:"replacement,omitempty"`
	}
	type deploymentRoot struct {
		ID        string `json:"id"`
		Component string `json:"component"`
		Version   string `json:"version"`
	}

	modules := make([]lockedModule, 0)
	if h != nil && h.lock != nil {
		for _, mod := range h.lock.GetModules() {
			if mod.Name == "" || mod.Version == "" {
				continue
			}
			_, replaced := h.replacements[mod.Name]
			modules = append(modules, lockedModule{
				Name: mod.Name, Version: mod.Version, Digest: mod.Hash,
				Root: mod.Root, Replacement: replaced,
			})
		}
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })

	roots := make([]deploymentRoot, 0)
	for _, entry := range baseline {
		// DependencyRoot is assigned by source/lock ingestion. Unowned roots
		// created through the registry API remain history-owned overlays.
		if entry.Kind != regapi.NamespaceDependency || !entry.DependencyRoot {
			continue
		}
		definition, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return "", err
		}
		if definition.Component == "" {
			return "", NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}
		roots = append(roots, deploymentRoot{
			ID: entry.ID.String(), Component: definition.Component, Version: definition.Version,
		})
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].ID != roots[j].ID {
			return roots[i].ID < roots[j].ID
		}
		if roots[i].Component != roots[j].Component {
			return roots[i].Component < roots[j].Component
		}
		return roots[i].Version < roots[j].Version
	})

	data, err := json.Marshal(struct {
		Modules []lockedModule   `json:"modules"`
		Roots   []deploymentRoot `json:"roots"`
	}{Modules: modules, Roots: roots})
	if err != nil {
		return "", fmt.Errorf("encode deployment dependency baseline: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (h *DependencyHandler) resolutionForSnapshot(
	ctx context.Context,
	baseline regapi.State,
	roots []desiredDependency,
	modules []ResolvedModule,
	transcoder payload.Transcoder,
) (*regapi.DependencyResolution, error) {
	baselineDigest, err := h.deploymentBaselineDigest(ctx, baseline, transcoder)
	if err != nil {
		return nil, err
	}
	resolution := dependencyResolution(roots, modules)
	resolution.BaselineDigest = baselineDigest
	return resolution.Canonical(), nil
}

func (h *DependencyHandler) resolutionRefreshReason(
	ctx context.Context,
	baseline regapi.State,
	roots []desiredDependency,
	resolution *regapi.DependencyResolution,
	baselineDigest string,
	transcoder payload.Transcoder,
) (string, error) {
	if resolution.BaselineDigest != "" {
		if resolution.BaselineDigest != baselineDigest {
			return "deployment baseline changed", nil
		}
		// With an unchanged deployment, a root-set mismatch can only be
		// inconsistent history. Leave it to strict stored-root validation.
		return "", nil
	}
	if dependencyInputDigest(roots) != resolution.InputDigest {
		for _, entry := range baseline {
			if entry.Kind == regapi.NamespaceDependency && entry.DependencyRoot {
				return "legacy resolution root declarations differ from deployment baseline", nil
			}
		}
		return "", nil
	}
	conflict, err := h.legacyResolutionConflictsWithBaseline(ctx, baseline, resolution, transcoder)
	if err != nil {
		return "", err
	}
	if conflict {
		return "legacy resolution conflicts with deployment baseline", nil
	}
	return "", nil
}

// legacyResolutionConflictsWithBaseline upgrades histories created before the
// baseline digest existed. It compares only modules controlled by the current
// deployment roots; history-owned roots remain governed by the stored graph.
func (h *DependencyHandler) legacyResolutionConflictsWithBaseline(
	ctx context.Context,
	baseline regapi.State,
	resolution *regapi.DependencyResolution,
	transcoder payload.Transcoder,
) (bool, error) {
	controlled, err := h.collectControlledModules(ctx, baseline, transcoder)
	if err != nil {
		return false, err
	}
	stored := make(map[string]regapi.ResolvedModule, len(resolution.Modules))
	for _, mod := range resolution.Modules {
		stored[mod.Name] = mod
	}
	versions := snapshotModuleVersions(baseline)
	digests := snapshotModuleDigests(baseline)
	if h != nil && h.lock != nil {
		for _, mod := range h.lock.GetModules() {
			if _, owned := controlled[mod.Name]; !owned {
				continue
			}
			if mod.Version != "" {
				versions[mod.Name] = mod.Version
			}
			if mod.Hash != "" {
				digests[mod.Name] = mod.Hash
			}
		}
	}
	for module := range controlled {
		version := versions[module]
		if version == "" {
			continue
		}
		selected, ok := stored[module]
		if !ok || selected.Version != version {
			return true, nil
		}
		if digest := digests[module]; digest != "" && !artifactDigestsEqual(digest, selected.Digest) {
			return true, nil
		}
	}
	return false, nil
}

func storedResolutionVersions(resolution *regapi.DependencyResolution) map[string]string {
	versions := make(map[string]string, len(resolution.Modules))
	for _, mod := range resolution.Modules {
		if mod.Name != "" && mod.Version != "" {
			versions[mod.Name] = mod.Version
		}
	}
	return versions
}

func resolvedModulesFromStored(resolution *regapi.DependencyResolution) ([]ResolvedModule, error) {
	resolved := make([]ResolvedModule, 0, len(resolution.Modules))
	seen := make(map[string]struct{}, len(resolution.Modules))
	for _, mod := range resolution.Modules {
		name, err := graph.ParseName(mod.Name)
		if err != nil || mod.Version == "" {
			return nil, NewDependencyResolutionError(fmt.Errorf("invalid stored module %q@%s", mod.Name, mod.Version))
		}
		if err := validateStoredModuleArtifactIdentity(name, mod.Version, mod.Source, mod.Digest); err != nil {
			return nil, NewDependencyResolutionError(err)
		}
		if _, duplicate := seen[mod.Name]; duplicate {
			return nil, NewDependencyResolutionError(fmt.Errorf("duplicate stored module %q", mod.Name))
		}
		seen[mod.Name] = struct{}{}
		resolved = append(resolved, ResolvedModule{
			Org: name.Organization, Name: name.Module, Version: mod.Version,
			VersionID: mod.VersionID, Source: mod.Source, Digest: mod.Digest,
			SizeBytes: mod.SizeBytes, Protected: mod.Protected,
		})
	}
	return resolved, nil
}
