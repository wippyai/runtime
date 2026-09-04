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

// ApplicationSourceID identifies the application deployment source.
const ApplicationSourceID = "application"

var sourceRegistryKey = &ctxapi.Key{Name: "deployment.sources"}

// ErrSourceLoaderUnavailable indicates that boot did not register the
// deployment source loader.
var ErrSourceLoaderUnavailable = errors.New("deployment source loader unavailable")

// Source describes one input to the normalized deployment baseline. Paths are
// Runtime-private and never exposed to guest code.
type Source struct {
	LoadPath       string
	ResourceRoot   string
	Owner          string
	Version        string
	Digest         string
	Sequence       uint64
	DeploymentRoot bool
	Replacement    bool
}

// Sources maps stable source identifiers to their current load identities.
type Sources map[string]Source

// LoadedSources is one atomic view of the normalized deployment baseline and
// the sources authoritative over its entries.
type LoadedSources struct {
	Owners  []string
	Entries []regapi.Entry
}

// SourceLoader rebuilds the normalized deployment baseline from an atomic
// snapshot of its current sources.
type SourceLoader func(context.Context, Sources) ([]regapi.Entry, error)

// SourceRegistry owns deployment source identities and coordinates source
// reloads with backing-store transitions.
type SourceRegistry struct {
	sources    Sources
	loader     SourceLoader
	mu         sync.RWMutex
	transition sync.RWMutex
}

// NewSourceRegistry creates an empty deployment source registry.
func NewSourceRegistry() *SourceRegistry {
	return &SourceRegistry{sources: Sources{}}
}

// Set replaces the complete deployment source snapshot.
func (r *SourceRegistry) Set(sources Sources) {
	if r == nil {
		return
	}
	r.transition.Lock()
	defer r.transition.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources = cloneSources(sources)
	assignMissingSourceSequences(r.sources, 0)
}

// Transition changes a controlled subset of source identities and its backing
// generation as one operation. Reloads observe either the previous generation
// or the next one, never a mixture.
func (r *SourceRegistry) Transition(
	desired Sources,
	backingTransition func() error,
	ids ...string,
) (Sources, error) {
	if r == nil {
		if backingTransition != nil {
			return nil, backingTransition()
		}
		return nil, nil
	}

	r.transition.Lock()
	defer r.transition.Unlock()
	if len(ids) == 0 {
		if backingTransition != nil {
			return nil, backingTransition()
		}
		return nil, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	nextSequence := maxSourceSequence(r.sources)
	if backingTransition != nil {
		if err := backingTransition(); err != nil {
			return nil, err
		}
	}

	previous := make(Sources)
	controlled := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, seen := controlled[id]; seen {
			continue
		}
		controlled[id] = struct{}{}
		if source := r.sources[id]; source.LoadPath != "" {
			previous[id] = source
		}
		delete(r.sources, id)
	}
	if r.sources == nil {
		r.sources = Sources{}
	}
	desiredIDs := make([]string, 0, len(desired))
	for id := range desired {
		desiredIDs = append(desiredIDs, id)
	}
	sort.Strings(desiredIDs)
	for _, id := range desiredIDs {
		source := desired[id]
		if source.LoadPath == "" {
			continue
		}
		if _, ok := controlled[id]; ok {
			if source.Sequence == 0 {
				source.Sequence = previous[id].Sequence
			}
			if source.Sequence == 0 {
				nextSequence++
				source.Sequence = nextSequence
			}
			r.sources[id] = source
		}
	}
	return previous, nil
}

// ResourceRoot returns the local root used for module-relative resources.
func (r *SourceRegistry) ResourceRoot(module string) (string, bool) {
	if r == nil || module == "" {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	root := r.sources[module].ResourceRoot
	return root, root != ""
}

func authoritativeOwners(sources Sources) []string {
	unique := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source.LoadPath != "" && source.Owner != "" {
			unique[source.Owner] = struct{}{}
		}
	}
	owners := make([]string, 0, len(unique))
	for owner := range unique {
		owners = append(owners, owner)
	}
	sort.Slice(owners, func(i, j int) bool {
		leftApplication := owners[i] == ApplicationSourceID
		rightApplication := owners[j] == ApplicationSourceID
		if leftApplication != rightApplication {
			return leftApplication
		}
		return owners[i] < owners[j]
	})
	return owners
}

func cloneSources(sources Sources) Sources {
	cloned := make(Sources, len(sources))
	for id, source := range sources {
		if id != "" && source.LoadPath != "" {
			cloned[id] = source
		}
	}
	return cloned
}

func maxSourceSequence(sources Sources) uint64 {
	var maxSequence uint64
	for _, source := range sources {
		if source.Sequence > maxSequence {
			maxSequence = source.Sequence
		}
	}
	return maxSequence
}

func assignMissingSourceSequences(sources Sources, nextSequence uint64) {
	if current := maxSourceSequence(sources); current > nextSequence {
		nextSequence = current
	}
	ids := make([]string, 0, len(sources))
	for id, source := range sources {
		if source.Sequence == 0 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		source := sources[id]
		nextSequence++
		source.Sequence = nextSequence
		sources[id] = source
	}
}

// SetLoader registers the deployment source loader.
func (r *SourceRegistry) SetLoader(loader SourceLoader) {
	if r == nil || loader == nil {
		return
	}
	r.mu.Lock()
	r.loader = loader
	r.mu.Unlock()
}

// Snapshot returns a detached view of the current deployment sources.
func (r *SourceRegistry) Snapshot() Sources {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSources(r.sources)
}

// Load rebuilds the normalized deployment baseline from one stable source
// generation.
func (r *SourceRegistry) Load(ctx context.Context) (LoadedSources, error) {
	if r == nil {
		return LoadedSources{}, ErrSourceLoaderUnavailable
	}
	r.transition.RLock()
	defer r.transition.RUnlock()
	r.mu.RLock()
	loader := r.loader
	sources := cloneSources(r.sources)
	r.mu.RUnlock()
	if loader == nil {
		return LoadedSources{}, ErrSourceLoaderUnavailable
	}
	entries, err := loader(ctx, sources)
	if err != nil {
		return LoadedSources{}, err
	}
	return LoadedSources{
		Owners:  authoritativeOwners(sources),
		Entries: entries,
	}, nil
}

// WithSourceRegistry stores a source registry in AppContext during boot.
func WithSourceRegistry(ctx context.Context, registry *SourceRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil || registry == nil || ac.Get(sourceRegistryKey) != nil || ac.IsSealed() {
		return ctx
	}
	ac.With(sourceRegistryKey, registry)
	return ctx
}

// GetSourceRegistry returns the deployment source registry from AppContext.
func GetSourceRegistry(ctx context.Context) *SourceRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	registry, _ := ac.Get(sourceRegistryKey).(*SourceRegistry)
	return registry
}
