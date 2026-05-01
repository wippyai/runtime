// SPDX-License-Identifier: MPL-2.0

// Package modules exposes runtime metadata about modules loaded into the app.
package modules

import (
	"context"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var sourceRootsKey = &ctxapi.Key{Name: "modules.source_roots"}

// SourceRoots maps module names in org/module form to their local load roots.
type SourceRoots map[string]string

// SourceRootRegistry stores module roots behind a mutex so runtime loaders can
// add roots after AppContext is sealed without mutating the AppContext itself.
type SourceRootRegistry struct {
	roots SourceRoots
	mu    sync.RWMutex
}

// NewSourceRootRegistry creates an empty source root registry.
func NewSourceRootRegistry() *SourceRootRegistry {
	return &SourceRootRegistry{
		roots: SourceRoots{},
	}
}

// SetAll records roots, ignoring empty module names and empty paths.
func (r *SourceRootRegistry) SetAll(roots SourceRoots) {
	if r == nil || len(roots) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.roots == nil {
		r.roots = SourceRoots{}
	}
	for module, root := range roots {
		if module == "" || root == "" {
			continue
		}
		r.roots[module] = root
	}
}

// Get returns a module root.
func (r *SourceRootRegistry) Get(module string) (string, bool) {
	if r == nil || module == "" {
		return "", false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	root, ok := r.roots[module]
	return root, ok && root != ""
}

// WithSourceRootRegistry stores an empty registry in AppContext during boot.
func WithSourceRootRegistry(ctx context.Context) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(sourceRootsKey) != nil || ac.IsSealed() {
		return ctx
	}
	ac.With(sourceRootsKey, NewSourceRootRegistry())
	return ctx
}

// WithSourceRoots records module source roots in AppContext. Existing roots are
// preserved unless replaced by a non-empty value from roots.
func WithSourceRoots(ctx context.Context, roots SourceRoots) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil || len(roots) == 0 {
		return ctx
	}

	reg, _ := ac.Get(sourceRootsKey).(*SourceRootRegistry)
	if reg == nil {
		if ac.IsSealed() {
			return ctx
		}
		reg = NewSourceRootRegistry()
		ac.With(sourceRootsKey, reg)
	}

	reg.SetAll(roots)

	return ctx
}

// SourceRoot returns the local load root for a module, when one is available.
func SourceRoot(ctx context.Context, module string) (string, bool) {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil || module == "" {
		return "", false
	}

	reg, ok := ac.Get(sourceRootsKey).(*SourceRootRegistry)
	if !ok || reg == nil {
		return "", false
	}

	return reg.Get(module)
}
