// SPDX-License-Identifier: MPL-2.0

// Package cdc contains the process-local registry of configured CDC sources.
// It deliberately knows nothing about source construction or any particular
// database driver; service/cdc owns that responsibility.
package cdc

import (
	"errors"
	"reflect"
	"sort"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
	"go.uber.org/zap"
)

var (
	ErrSourceExists  = errors.New("cdc source already registered")
	ErrSourceMissing = errors.New("cdc source not registered")
)

type entry struct {
	source api.Source
	kind   registry.Kind
}

// Registry is a concurrency-safe, driver-neutral source registry. The map is
// keyed only by canonical registry.ID values. Drivers must not introduce
// process-wide aliases such as PostgreSQL slot names into this layer.
type Registry struct {
	log     *zap.Logger
	mu      sync.RWMutex
	sources map[registry.ID]entry
}

func NewRegistry(log *zap.Logger) *Registry {
	if log == nil {
		log = zap.NewNop()
	}
	return &Registry{
		log:     log,
		sources: make(map[registry.ID]entry),
	}
}

// Register adds a source. It does not replace an existing source; callers
// must use Replace for an update so an accidental duplicate cannot orphan a
// running source.
func (r *Registry) Register(id registry.ID, source api.Source, kind registry.Kind) error {
	if nilSource(source) {
		return errors.New("cdc source is nil")
	}
	id = canonicalID(id)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[id]; exists {
		return ErrSourceExists
	}
	r.sources[id] = entry{source: source, kind: kind}
	r.log.Debug("cdc source registered", zap.String("id", id.String()), zap.String("kind", kind))
	return nil
}

// Replace atomically makes source visible under id and returns the previously
// visible source. The old source is not stopped here: lifecycle ownership stays
// with service/cdc, which can stop it after the visibility swap.
func (r *Registry) Replace(id registry.ID, source api.Source, kind registry.Kind) (api.Source, bool, error) {
	if nilSource(source) {
		return nil, false, errors.New("cdc source is nil")
	}
	id = canonicalID(id)

	r.mu.Lock()
	old, exists := r.sources[id]
	if !exists {
		r.mu.Unlock()
		return nil, false, ErrSourceMissing
	}
	r.sources[id] = entry{source: source, kind: kind}
	r.mu.Unlock()

	r.log.Info("cdc source replaced", zap.String("id", id.String()), zap.String("kind", kind))
	return old.source, true, nil
}

// Unregister removes a source and returns it to the lifecycle owner.
func (r *Registry) Unregister(id registry.ID) (api.Source, bool) {
	id = canonicalID(id)
	r.mu.Lock()
	old, exists := r.sources[id]
	if exists {
		delete(r.sources, id)
	}
	r.mu.Unlock()
	if exists {
		r.log.Debug("cdc source unregistered", zap.String("id", id.String()))
		return old.source, true
	}
	return nil, false
}

func (r *Registry) Get(id registry.ID) (api.Source, bool) {
	id = canonicalID(id)
	r.mu.RLock()
	item, ok := r.sources[id]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return item.source, true
}

// List returns a deterministic snapshot. Metadata from the registry is merged
// over the source's own Info so a source cannot accidentally publish a
// different global identity or kind.
func (r *Registry) List() []api.SourceInfo {
	r.mu.RLock()
	items := make([]struct {
		id     registry.ID
		kind   registry.Kind
		source api.Source
	}, 0, len(r.sources))
	for id, item := range r.sources {
		items = append(items, struct {
			id     registry.ID
			kind   registry.Kind
			source api.Source
		}{id: id, kind: item.kind, source: item.source})
	}
	r.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		return items[i].id.String() < items[j].id.String()
	})
	out := make([]api.SourceInfo, 0, len(items))
	for _, item := range items {
		info := item.source.Info()
		info.ID = item.id
		info.Kind = item.kind
		info.Name = item.id.String()
		out = append(out, info)
	}
	return out
}

func canonicalID(id registry.ID) registry.ID {
	return registry.ParseID(id.String())
}

func nilSource(source api.Source) bool {
	if source == nil {
		return true
	}
	v := reflect.ValueOf(source)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

var _ api.Registry = (*Registry)(nil)
