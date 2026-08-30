// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	if h != nil && h.deployment != nil {
		for _, mod := range h.deployment.Modules {
			if mod.Name == "" || mod.Version == "" {
				continue
			}
			modules = append(modules, lockedModule{
				Name: mod.Name, Version: mod.Version, Digest: strings.TrimPrefix(strings.ToLower(mod.Digest), "sha256:"),
				Root: mod.Name == h.deployment.Root, Replacement: mod.Source == moduleSourceReplacementTreeV1,
			})
		}
	} else if h != nil && h.lock != nil {
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
		// Registry.Root is assigned by source/lock ingestion. Unowned roots
		// created through the registry API remain history-owned overlays.
		if entry.Kind != regapi.NamespaceDependency || !entry.Registry.Root {
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
		Model   string           `json:"model"`
		Modules []lockedModule   `json:"modules"`
		Roots   []deploymentRoot `json:"roots"`
	}{Model: "deployment-overlay-v1", Modules: modules, Roots: roots})
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
	references []desiredDependency,
	modules []ResolvedModule,
	transcoder payload.Transcoder,
) (*regapi.DependencyResolution, error) {
	baselineDigest, err := h.deploymentBaselineDigest(ctx, baseline, transcoder)
	if err != nil {
		return nil, err
	}
	resolution := dependencyResolution(roots, references, modules)
	resolution.BaselineDigest = baselineDigest
	resolution.Deployment = h.deployment.Canonical()
	return resolution.Canonical(), nil
}

func (h *DependencyHandler) resolutionRefreshReason(
	ctx context.Context,
	baseline regapi.State,
	roots []desiredDependency,
	references []desiredDependency,
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
	// A legacy graph predates recorded references. Folded declarations the
	// stored graph does not carry can never replay strictly — and whether the
	// duplicate's ID sorts before or after the stored controller must not
	// decide the outcome — so upgrade once through the standard refresh.
	if len(references) > len(resolution.References) {
		return "legacy resolution predates folded reference declarations", nil
	}
	if dependencyInputDigest(roots) != resolution.InputDigest {
		for _, entry := range baseline {
			if entry.Kind == regapi.NamespaceDependency && entry.Registry.Root {
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
	versions, digests := h.currentModuleIdentities(ctx)
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

// upgradeLegacyReferencedResolution upgrades a baseline-unbound graph whose
// only deficit is folded reference declarations the stored selection already
// satisfies. The stored graph is the durable record of controller identity, so
// declarations partition by its recorded roots — never by re-running election —
// and every extra declaration becomes a reference. The stored module closure
// is retained exactly and the graph binds to the current baseline without a
// hub round trip, so a cold offline restart never wedges on declarations that
// change nothing about the selection. Any ineligibility falls back to the
// standard refresh.
func (h *DependencyHandler) upgradeLegacyReferencedResolution(
	ctx context.Context,
	baseline regapi.State,
	declarations []desiredDependency,
	resolution *regapi.DependencyResolution,
	baselineDigest string,
	transcoder payload.Transcoder,
) (*regapi.DependencyResolution, []ResolvedModule, bool) {
	if resolution.BaselineDigest != "" {
		return nil, nil, false
	}
	storedRoots := make(map[string]regapi.DependencyRoot, len(resolution.Roots))
	rootComponents := make(map[string]struct{}, len(resolution.Roots))
	for _, root := range resolution.Roots {
		storedRoots[idKey(regapi.ParseID(root.ID))] = root
		rootComponents[root.Component] = struct{}{}
	}

	rootDeps := make([]desiredDependency, 0, len(resolution.Roots))
	refDeps := make([]desiredDependency, 0, len(declarations))
	for _, dep := range declarations {
		root, isStored := storedRoots[idKey(dep.entry.ID)]
		if !isStored {
			if _, anchored := rootComponents[dep.definition.Component]; !anchored {
				return nil, nil, false
			}
			refDeps = append(refDeps, dep)
			continue
		}
		if dep.definition.Component != root.Component || dep.definition.Version != root.Version {
			return nil, nil, false
		}
		rootDeps = append(rootDeps, dep)
	}
	if len(rootDeps) != len(resolution.Roots) || len(refDeps) <= len(resolution.References) {
		return nil, nil, false
	}
	if dependencyInputDigest(rootDeps) != resolution.InputDigest {
		return nil, nil, false
	}
	conflict, err := h.legacyResolutionConflictsWithBaseline(ctx, baseline, resolution, transcoder)
	if err != nil || conflict {
		return nil, nil, false
	}
	resolved, err := resolvedModulesFromStored(resolution)
	if err != nil {
		return nil, nil, false
	}
	for _, dep := range declarations {
		selected, ok := selectedModuleVersion(resolved, dep.definition.Component)
		if !ok || !storedVersionSatisfies(selected, dep.definition.Version) {
			return nil, nil, false
		}
	}
	upgraded := resolution.Canonical()
	upgraded.References = dependencyReferenceRoots(refDeps)
	upgraded.BaselineDigest = baselineDigest
	upgraded = upgraded.Canonical()
	if !upgraded.Valid() {
		return nil, nil, false
	}
	return upgraded, resolved, true
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
	return resolvedModulesFromRecords(resolution.Modules)
}

func resolvedModulesFromRecords(modules []regapi.ResolvedModule) ([]ResolvedModule, error) {
	resolved := make([]ResolvedModule, 0, len(modules))
	seen := make(map[string]struct{}, len(modules))
	for _, mod := range modules {
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
