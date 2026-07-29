// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/artifact"
)

var _ regapi.FinalizingEffect = (*artifact.WAPPEffect)(nil)

// buildArtifactEffect binds derived artifact outputs to the same transaction as
// the dependency graph change. Module selection and verification remain owned
// by DependencyHandler; the artifact subsystem only sees exact WAPP paths.
func (h *DependencyHandler) buildArtifactEffect(
	ctx context.Context,
	resolved []ResolvedModule,
) (regapi.Effect, error) {
	if h == nil || h.artifacts == nil {
		return nil, nil
	}

	packs := make([]artifact.WAPP, 0, len(resolved))
	seen := make(map[string]struct{}, len(resolved))
	for _, module := range resolved {
		moduleName := module.Org + "/" + module.Name
		if _, replaced := h.replacementPath(moduleName); replaced ||
			module.Source == moduleSourceReplacementTreeV1 {
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
	if len(packs) == 0 {
		return nil, nil
	}
	return artifact.NewWAPPEffect(h.artifacts, packs, h.artifactRoot)
}
