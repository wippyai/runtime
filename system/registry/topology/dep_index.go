// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"github.com/wippyai/runtime/api/registry"
)

// DepIndex stores the inverse of "entry E depends on D" so that the source-side
// constraint in SortChangeSet can resolve "what depends on D?" in constant time
// instead of walking every entry in the registry state.
//
// Three kinds of dependency identity are tracked separately because they
// behave differently under entry mutation. Direct dependencies pin a specific
// registry.ID. Group dependencies pin a group name; whoever holds that group
// in registry.TagGroups is part of the group. Namespace dependencies pin a
// namespace; whoever has the same ID.NS is part of it.
//
// The Dependents lookup walks the entry's own identity attributes (its ID,
// its groups, its namespace) and unions the entries that declared a
// dependency on any of them.
//
// Maintenance is incremental: OnCreate adds an entry's outbound dependency
// declarations to the index, OnDelete undoes them, OnUpdate diffs old vs new.
// The index never has to revisit unrelated entries on an apply.
type DepIndex struct {
	direct  map[registry.ID]map[registry.ID]struct{}
	group   map[string]map[registry.ID]struct{}
	ns      map[string]map[registry.ID]struct{}
	ownDeps map[registry.ID]entryDepKeys
}

type entryDepKeys struct {
	direct []registry.ID
	groups []string
	ns     []string
}

// NewDepIndex returns an empty index. Use BuildDepIndex to seed it from an
// existing registry state.
func NewDepIndex() *DepIndex {
	return &DepIndex{
		direct:  make(map[registry.ID]map[registry.ID]struct{}),
		group:   make(map[string]map[registry.ID]struct{}),
		ns:      make(map[string]map[registry.ID]struct{}),
		ownDeps: make(map[registry.ID]entryDepKeys),
	}
}

// BuildDepIndex constructs a DepIndex from a registry state using the given
// resolver to discover indirect dependencies (e.g. data.imports.*).
//
// Cost is O(N x P x T) where N is the number of entries, P the number of
// resolver patterns, and T the cost of one pattern walk. This is the same
// cost as one full SortChangeSet walk on master today; the index amortizes it
// over the lifetime of the Reg.
func BuildDepIndex(state registry.State, resolver registry.DependencyResolver) *DepIndex {
	idx := NewDepIndex()
	for _, entry := range state {
		idx.add(entry, resolver)
	}
	return idx
}

// Dependents writes every entry ID that declared dependency on the given
// entry (directly, by group, or by namespace) into out. The caller is
// expected to pass a reusable set so repeated calls inside SortChangeSet can
// dedupe across multiple delete operations.
func (d *DepIndex) Dependents(entry registry.Entry, out map[registry.ID]struct{}) {
	for id := range d.direct[entry.ID] {
		out[id] = struct{}{}
	}
	for _, g := range entry.Meta.GetSlice(registry.TagGroups) {
		for id := range d.group[g] {
			out[id] = struct{}{}
		}
	}
	if entry.ID.NS != "" {
		for id := range d.ns[entry.ID.NS] {
			out[id] = struct{}{}
		}
	}
}

// OnCreate records the new entry's outbound dependency declarations.
func (d *DepIndex) OnCreate(entry registry.Entry, resolver registry.DependencyResolver) {
	d.add(entry, resolver)
}

// OnUpdate replaces the old entry's declarations with the new one's. prev is
// the entry as it existed before the update; next is the entry as it now is.
func (d *DepIndex) OnUpdate(prev, next registry.Entry, resolver registry.DependencyResolver) {
	d.remove(prev.ID)
	d.add(next, resolver)
}

// OnDelete removes the entry's outbound dependency declarations.
func (d *DepIndex) OnDelete(entry registry.Entry) {
	d.remove(entry.ID)
}

// Patch folds a successfully-committed changeset into the index. Callers pass
// the original entry (when available, for diff) via op.OriginalEntry.
func (d *DepIndex) Patch(changeSet registry.ChangeSet, resolver registry.DependencyResolver) {
	for _, op := range changeSet {
		switch op.Kind {
		case registry.EntryCreate:
			d.OnCreate(op.Entry, resolver)
		case registry.EntryUpdate:
			if op.OriginalEntry != nil {
				d.OnUpdate(*op.OriginalEntry, op.Entry, resolver)
			} else {
				d.remove(op.Entry.ID)
				d.add(op.Entry, resolver)
			}
		case registry.EntryDelete:
			d.OnDelete(op.Entry)
		}
	}
}

func (d *DepIndex) add(entry registry.Entry, resolver registry.DependencyResolver) {
	keys := extractDepKeys(entry, resolver)
	d.ownDeps[entry.ID] = keys
	for _, id := range keys.direct {
		addIDToSetMap(d.direct, id, entry.ID)
	}
	for _, g := range keys.groups {
		addIDToStringMap(d.group, g, entry.ID)
	}
	for _, n := range keys.ns {
		addIDToStringMap(d.ns, n, entry.ID)
	}
}

func (d *DepIndex) remove(entryID registry.ID) {
	keys, ok := d.ownDeps[entryID]
	if !ok {
		return
	}
	delete(d.ownDeps, entryID)
	for _, id := range keys.direct {
		delIDFromSetMap(d.direct, id, entryID)
	}
	for _, g := range keys.groups {
		delIDFromStringMap(d.group, g, entryID)
	}
	for _, n := range keys.ns {
		delIDFromStringMap(d.ns, n, entryID)
	}
}

func extractDepKeys(entry registry.Entry, resolver registry.DependencyResolver) entryDepKeys {
	declarations := entry.Meta.GetSlice(registry.TagDependsOn)
	if resolver != nil {
		declarations = append(declarations, resolver.Extract(entry)...)
	}
	var keys entryDepKeys
	for _, dep := range declarations {
		depType, value := parseDependency(dep)
		switch depType {
		case "direct":
			id := resolveDependencyID(entry.ID.NS, value)
			if id.Equal(entry.ID) {
				continue
			}
			keys.direct = append(keys.direct, id)
		case "group":
			keys.groups = append(keys.groups, value)
		case "namespace":
			keys.ns = append(keys.ns, value)
		}
	}
	return keys
}

func addIDToSetMap(m map[registry.ID]map[registry.ID]struct{}, key, value registry.ID) {
	set := m[key]
	if set == nil {
		set = make(map[registry.ID]struct{})
		m[key] = set
	}
	set[value] = struct{}{}
}

func delIDFromSetMap(m map[registry.ID]map[registry.ID]struct{}, key, value registry.ID) {
	set := m[key]
	if set == nil {
		return
	}
	delete(set, value)
	if len(set) == 0 {
		delete(m, key)
	}
}

func addIDToStringMap(m map[string]map[registry.ID]struct{}, key string, value registry.ID) {
	set := m[key]
	if set == nil {
		set = make(map[registry.ID]struct{})
		m[key] = set
	}
	set[value] = struct{}{}
}

func delIDFromStringMap(m map[string]map[registry.ID]struct{}, key string, value registry.ID) {
	set := m[key]
	if set == nil {
		return
	}
	delete(set, value)
	if len(set) == 0 {
		delete(m, key)
	}
}
