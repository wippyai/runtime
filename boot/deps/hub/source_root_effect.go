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

// sourceRootEffect keeps module-relative directory resolution inside the same
// prepare/rollback boundary as the registry transition and durable history.
type sourceRootEffect struct {
	desired  moduleapi.SourceRoots
	previous moduleapi.SourceRoots
	modules  []string
	prepared bool
}

func (e *sourceRootEffect) Prepare(ctx context.Context) error {
	if e.prepared {
		return nil
	}
	e.previous = moduleapi.SwapSourceRoots(ctx, e.desired, e.modules...)
	e.prepared = true
	return nil
}

func (e *sourceRootEffect) Commit(context.Context) error { return nil }

func (e *sourceRootEffect) Rollback(ctx context.Context) error {
	if !e.prepared {
		return nil
	}
	moduleapi.SwapSourceRoots(ctx, e.previous, e.modules...)
	e.previous = nil
	e.prepared = false
	return nil
}

func (h *DependencyHandler) buildSourceRootEffect(
	resolved []ResolvedModule,
	controlled map[string]struct{},
) (*sourceRootEffect, error) {
	desired := make(moduleapi.SourceRoots)
	for _, mod := range resolved {
		module := mod.Org + "/" + mod.Name
		var root string
		if replacement, ok := h.replacementPath(module); ok && (mod.Source == "" || mod.Source == moduleSourceReplacementTreeV1) {
			root = replacement
		} else if h.shouldUnpackModules() {
			name := graph.Name{Organization: mod.Org, Module: mod.Name}
			path, err := containedPath(h.vendorDir, lock.ModulePath(name))
			if err != nil {
				return nil, err
			}
			root = path
		}
		if root == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		desired[module] = absolute
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
	return &sourceRootEffect{desired: desired, modules: modules}, nil
}
