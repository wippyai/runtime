// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/artifact"
)

var _ regapi.FinalizingEffect = (*artifact.Effect)(nil)

// buildArtifactEffect binds derived artifact outputs to the same transaction as
// the dependency graph change. Module selection and verification remain owned
// by DependencyHandler; the artifact subsystem only sees exact WAPP paths.
func (h *DependencyHandler) buildArtifactEffect(
	ctx context.Context,
	resolved []ResolvedModule,
	state regapi.State,
) (regapi.Effect, error) {
	if h == nil || h.artifacts == nil {
		return nil, nil
	}

	packs := make([]artifact.WAPP, 0, len(resolved))
	replacementVersions := make(map[string]string)
	seen := make(map[string]struct{}, len(resolved))
	for _, module := range resolved {
		moduleName := module.Org + "/" + module.Name
		if _, replaced := h.replacementPath(moduleName); replaced ||
			module.Source == moduleSourceReplacementTreeV1 {
			replacementVersions[moduleName] = module.Version
			continue
		}
		path, err := h.ensureModuleAvailable(ctx, module)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		packs = append(packs, artifact.WAPP{
			Path:          path,
			ModuleVersion: module.Version,
		})
	}
	resources, err := h.replacementArtifactResources(ctx, state, replacementVersions)
	if err != nil {
		return nil, err
	}
	return artifact.NewEffect(h.artifacts, packs, resources, h.artifactRoot)
}

func (h *DependencyHandler) replacementArtifactResources(
	ctx context.Context,
	state regapi.State,
	versions map[string]string,
) ([]artifact.Resource, error) {
	if len(versions) == 0 {
		return nil, nil
	}
	roots := make(map[string]string, len(versions))
	for module := range versions {
		root, ok := h.replacementPath(module)
		if ok {
			roots[module] = root
		}
	}
	return artifact.DirectoryResources(ctx, state, roots, versions)
}
