// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"path/filepath"
	"strings"

	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
)

// prepareRestoreSources materializes the stored graph and publishes the same
// source registry used by live dependency transitions.
func (h *DependencyHandler) prepareRestoreSources(ctx context.Context, modules []ResolvedModule) error {
	sources := make(moduleapi.Sources, len(modules))
	for index, module := range modules {
		path, err := h.ensureModuleAvailable(ctx, module)
		if err != nil {
			return NewArtifactIOError("materialize recorded module", modKey(module), err)
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return NewArtifactIOError("resolve recorded module", modKey(module), err)
		}
		name := module.Org + "/" + module.Name
		source := moduleapi.Source{
			LoadPath:       absolutePath,
			Owner:          name,
			Version:        module.Version,
			Digest:         module.Digest,
			Sequence:       uint64(index) + 1,
			DeploymentRoot: h.isDeploymentRoot(name),
			Replacement:    module.Source == moduleSourceReplacementTreeV1,
		}
		if source.Replacement {
			source.ResourceRoot = absolutePath
		}
		sources[name] = source
	}
	registry := moduleapi.GetSourceRegistry(ctx)
	if registry == nil {
		return nil
	}
	registry.Set(sources)
	return nil
}

// prepareRecordedArtifacts prefetches immutable history artifacts while online
// access is available. Local trees cannot be recovered from the Hub: the final
// declaration graph must select them before reconciliation snapshots and loads
// them. Deployment baseline sources are loaded separately by prepareRestoreSources
// (or the source checkout loader), including the application root.
func (h *DependencyHandler) prepareRecordedArtifacts(ctx context.Context, modules []ResolvedModule) error {
	for _, module := range modules {
		_, replaced := h.replacementPath(module.Org + "/" + module.Name)
		if module.Source == moduleSourceReplacementTreeV1 || replaced {
			continue
		}
		if _, err := h.ensureModuleAvailable(ctx, module); err != nil {
			return NewArtifactIOError("materialize recorded module", modKey(module), err)
		}
	}
	return nil
}

// withoutRepinnedModules drops recorded modules that the current pins select at
// another version or digest. A new image ships only its own pins, and the
// baseline transition selects them, so offline startup does not need the
// recorded artifact.
func withoutRepinnedModules(modules []ResolvedModule, recorded, current []regapi.ResolvedModule) []ResolvedModule {
	pins := make(map[string]regapi.ResolvedModule, len(recorded))
	for _, module := range recorded {
		pins[module.Name] = module
	}
	repinned := make(map[string]struct{})
	for _, module := range current {
		if pin, ok := pins[module.Name]; ok && (pin.Version != module.Version || !sameDigest(pin.Digest, module.Digest)) {
			repinned[module.Name] = struct{}{}
		}
	}
	if len(repinned) == 0 {
		return modules
	}
	kept := make([]ResolvedModule, 0, len(modules)-len(repinned))
	for _, module := range modules {
		if _, ok := repinned[module.Org+"/"+module.Name]; !ok {
			kept = append(kept, module)
		}
	}
	return kept
}

func lockPins(l *lock.Lock) []regapi.ResolvedModule {
	modules := l.GetModules()
	pins := make([]regapi.ResolvedModule, 0, len(modules))
	for _, module := range modules {
		pins = append(pins, regapi.ResolvedModule{Name: module.Name, Version: module.Version, Digest: module.Hash})
	}
	return pins
}

func sameDigest(a, b string) bool {
	return strings.EqualFold(strings.TrimPrefix(a, "sha256:"), strings.TrimPrefix(b, "sha256:"))
}
