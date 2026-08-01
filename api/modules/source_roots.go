// SPDX-License-Identifier: MPL-2.0

// Package modules exposes runtime metadata about modules loaded into the app.
package modules

import (
	"context"
	"errors"
	"sort"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	regapi "github.com/wippyai/runtime/api/registry"
)

var sourceRootsKey = &ctxapi.Key{Name: "modules.source_roots"}

// ErrSourceLoaderUnavailable indicates that boot did not register the
// deployment's normalized source reloader.
var ErrSourceLoaderUnavailable = errors.New("effective deployment source loader unavailable")

// SourceLoader rebuilds the normalized deployment baseline from its established
// application, packed dependency, and local replacement sources.
type SourceLoader func(context.Context) ([]regapi.Entry, error)

// SourceRoots maps module names in org/module form to their local load roots.
type SourceRoots map[string]string

// SourceRootRegistry stores module roots behind a mutex so runtime loaders can
// add roots after AppContext is sealed without mutating the AppContext itself.
type SourceRootRegistry struct {
	roots  SourceRoots
	loader SourceLoader
	mu     sync.RWMutex
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

// SwapSubset atomically replaces the roots for modules with desired and
// returns the roots that existed before the replacement. Empty module names,
// roots, and desired entries outside modules are ignored.
func (r *SourceRootRegistry) SwapSubset(desired SourceRoots, modules ...string) SourceRoots {
	if r == nil || len(modules) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	previous := make(SourceRoots)
	controlled := make(map[string]struct{}, len(modules))
	for _, module := range modules {
		if module == "" {
			continue
		}
		if _, seen := controlled[module]; seen {
			continue
		}
		controlled[module] = struct{}{}
		if root := r.roots[module]; root != "" {
			previous[module] = root
		}
		delete(r.roots, module)
	}
	if r.roots == nil {
		r.roots = SourceRoots{}
	}
	for module, root := range desired {
		if root == "" {
			continue
		}
		if _, ok := controlled[module]; ok {
			r.roots[module] = root
		}
	}
	return previous
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

// Modules returns the names of modules with local directory sources.
// The paths remain private to Runtime; callers receive capability identifiers.
func (r *SourceRootRegistry) Modules() []string {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	modules := make([]string, 0, len(r.roots))
	for module, root := range r.roots {
		if module != "" && root != "" {
			modules = append(modules, module)
		}
	}
	sort.Strings(modules)
	return modules
}

// SetLoader atomically registers the deployment's normalized source reloader.
func (r *SourceRootRegistry) SetLoader(loader SourceLoader) {
	if r == nil || loader == nil {
		return
	}
	r.mu.Lock()
	r.loader = loader
	r.mu.Unlock()
}

// Load rebuilds the normalized deployment baseline through the registered
// source reloader.
func (r *SourceRootRegistry) Load(ctx context.Context) ([]regapi.Entry, error) {
	if r == nil {
		return nil, ErrSourceLoaderUnavailable
	}
	r.mu.RLock()
	loader := r.loader
	r.mu.RUnlock()
	if loader == nil {
		return nil, ErrSourceLoaderUnavailable
	}
	return loader(ctx)
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

// SwapSourceRoots atomically replaces the controlled module roots and returns
// the roots that existed before the replacement. It is a no-op when ctx has no
// source-root registry.
func SwapSourceRoots(ctx context.Context, desired SourceRoots, modules ...string) SourceRoots {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	reg, _ := ac.Get(sourceRootsKey).(*SourceRootRegistry)
	if reg == nil {
		return nil
	}
	return reg.SwapSubset(desired, modules...)
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

// SourceModules returns stable capability identifiers for modules whose
// effective source is locally available as a directory.
func SourceModules(ctx context.Context) []string {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}

	reg, ok := ac.Get(sourceRootsKey).(*SourceRootRegistry)
	if !ok || reg == nil {
		return nil
	}
	return reg.Modules()
}

// WithSourceLoader registers the deployment's authoritative source reloader.
func WithSourceLoader(ctx context.Context, loader SourceLoader) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil || loader == nil {
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
	reg.SetLoader(loader)
	return ctx
}

// LoadSources rebuilds the same normalized baseline used by a restart.
func LoadSources(ctx context.Context) ([]regapi.Entry, error) {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil, ErrSourceLoaderUnavailable
	}
	reg, ok := ac.Get(sourceRootsKey).(*SourceRootRegistry)
	if !ok || reg == nil {
		return nil, ErrSourceLoaderUnavailable
	}
	return reg.Load(ctx)
}
