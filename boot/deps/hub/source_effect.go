// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"path/filepath"
	"sort"

	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
)

// sourceEffect keeps deployment sources and their backing generation inside
// the same prepare/rollback boundary as the registry transition and history.
type sourceEffect struct {
	desired  moduleapi.Sources
	previous moduleapi.Sources
	modules  []string
	prepared bool
}

func (e *sourceEffect) Prepare(ctx context.Context) error {
	return e.prepareWith(ctx, nil)
}

func (e *sourceEffect) prepareWith(ctx context.Context, transition func() error) error {
	if e.prepared {
		return nil
	}
	previous, err := transitionSources(ctx, e.desired, transition, e.modules...)
	if err != nil {
		return err
	}
	e.previous = previous
	e.prepared = true
	return nil
}

func (e *sourceEffect) Commit(context.Context) error { return nil }

func (e *sourceEffect) Rollback(ctx context.Context) error {
	return e.rollbackWith(ctx, nil)
}

func (e *sourceEffect) rollbackWith(ctx context.Context, transition func() error) error {
	if !e.prepared {
		if transition != nil {
			return transition()
		}
		return nil
	}
	if _, err := transitionSources(ctx, e.previous, transition, e.modules...); err != nil {
		return err
	}
	e.previous = nil
	e.prepared = false
	return nil
}

func (h *DependencyHandler) buildSourceEffect(
	resolved []ResolvedModule,
	controlled map[string]struct{},
) (*sourceEffect, error) {
	desired := make(moduleapi.Sources)
	for _, mod := range resolved {
		module := mod.Org + "/" + mod.Name
		name := graph.Name{Organization: mod.Org, Module: mod.Name}
		var path, root string
		replacementSource := false
		if replacement, ok := h.replacementPath(module); ok && (mod.Source == "" || mod.Source == moduleSourceReplacementTreeV1) {
			root = replacement
			path = lock.ModuleEntryLoadPath(root)
			replacementSource = true
		} else if h.shouldUnpackModules() {
			var err error
			root, err = containedPath(h.vendorDir, lock.ModulePath(name))
			if err != nil {
				return nil, err
			}
			path = lock.ModuleEntryLoadPath(root)
		} else {
			var available bool
			var err error
			path, available, err = h.cachedModuleArtifact(mod)
			if err != nil {
				return nil, err
			}
			if !available {
				continue
			}
		}
		if path == "" {
			continue
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		absoluteRoot := ""
		if root != "" {
			absoluteRoot, err = filepath.Abs(root)
			if err != nil {
				return nil, err
			}
		}
		owner := ""
		if absoluteRoot != "" {
			owner = module
		}
		desired[module] = moduleapi.Source{
			LoadPath:       absolutePath,
			ResourceRoot:   absoluteRoot,
			Owner:          owner,
			Version:        mod.Version,
			Digest:         mod.Digest,
			DeploymentRoot: h.isDeploymentRoot(module),
			Replacement:    replacementSource,
		}
	}
	if len(desired) == 0 && len(controlled) == 0 {
		return nil, nil
	}
	// Detach the caller-owned map because effects outlive planning.
	controlledCopy := make(map[string]struct{}, len(controlled)+len(resolved))
	modules := make([]string, 0, len(controlled)+len(resolved))
	for module := range controlled {
		controlledCopy[module] = struct{}{}
		modules = append(modules, module)
	}
	for _, mod := range resolved {
		module := mod.Org + "/" + mod.Name
		if mod.Org != "" && mod.Name != "" {
			if _, ok := controlledCopy[module]; !ok {
				controlledCopy[module] = struct{}{}
				modules = append(modules, module)
			}
		}
	}
	sort.Strings(modules)
	return &sourceEffect{desired: desired, modules: modules}, nil
}

func transitionSources(
	ctx context.Context,
	desired moduleapi.Sources,
	transition func() error,
	ids ...string,
) (moduleapi.Sources, error) {
	registry := moduleapi.GetSourceRegistry(ctx)
	if registry == nil {
		if transition != nil {
			return nil, transition()
		}
		return nil, nil
	}
	return registry.Transition(desired, transition, ids...)
}
