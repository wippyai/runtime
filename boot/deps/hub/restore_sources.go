// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"path/filepath"

	moduleapi "github.com/wippyai/runtime/api/modules"
)

// prepareRestoreSources materializes the stored graph and publishes the same
// source registry used by live dependency transitions.
func (h *DependencyHandler) prepareRestoreSources(ctx context.Context, modules []ResolvedModule) error {
	sources := make(moduleapi.Sources, len(modules))
	for index, module := range modules {
		path, err := h.ensureModuleAvailable(ctx, module)
		if err != nil {
			return fmt.Errorf("materialize recorded module %s: %w", modKey(module), err)
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve recorded module %s: %w", modKey(module), err)
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

func (h *DependencyHandler) materializeRestoreModules(ctx context.Context, modules []ResolvedModule) error {
	for _, module := range modules {
		if _, err := h.ensureModuleAvailable(ctx, module); err != nil {
			return fmt.Errorf("materialize recorded module %s: %w", modKey(module), err)
		}
	}
	return nil
}
