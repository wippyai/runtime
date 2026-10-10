// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"github.com/wippyai/runtime/api/registry"
)

// DepIndex stores the inverse of "entry E depends on D" so that the source-side
// constraint in SortChangeSet can resolve "what depends on D?" in constant time
// instead of walking every entry in the registry state.
//
// Four kinds of dependency identity are tracked separately because they
// behave differently under entry mutation. Direct dependencies pin a specific
// registry.ID. Group dependencies pin a group name; whoever holds that group
// in registry.TagGroups is part of the group. Namespace dependencies pin a
// namespace; whoever has the same ID.NS is part of it. Environment references
// pin a variable name; env.variable entries declare that name in their data.
//
// The Dependents lookup walks the entry's own identity attributes (its ID,
// its groups, its namespace, its environment name) and unions the entries that declared a
// dependency on any of them.
//
// Maintenance is incremental: OnCreate adds an entry's outbound dependency
// declarations to the index, OnDelete undoes them, OnUpdate diffs old vs new.
// The index never has to revisit unrelated entries on an apply.
type DepIndex struct {
	direct map[registry.ID]map[registry.ID]struct{}
	group  map[string]map[registry.ID]struct{}
	ns     map[string]map[registry.ID]struct{}
	env    map[string]map[registry.ID]struct{}
	// Fallbacks remain symbolic: creating/removing a named variable can
	// shadow/unshadow an ID without rewriting every consumer's declaration.
	envFallback   map[string]map[registry.ID]struct{}
	variables     map[registry.ID]string
	variableNames map[string]map[registry.ID]struct{}
	ownDeps       map[registry.ID]entryDepKeys
}

type entryDepKeys struct {
	direct []registry.ID
	groups []string
	ns     []string
	env    []string
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
// entry (directly, by group, by namespace, or by environment name) into out. The caller is
// expected to pass a reusable set so repeated calls inside SortChangeSet can
// dedupe across multiple delete operations.
func (d *DepIndex) Dependents(entry registry.Entry, out map[registry.ID]struct{}) {
	for id := range d.env[envVariableName(entry)] {
		if !id.Equal(entry.ID) {
			out[id] = struct{}{}
		}
	}
	if len(d.envFallback) > 0 {
		d.environmentFallbackDependents("id:"+entry.ID.String(), entry.ID, out)
	}
	for id := range d.direct[entry.ID] {
		if !id.Equal(entry.ID) {
			out[id] = struct{}{}
		}
	}
	for _, g := range entry.Meta.GetSlice(registry.TagGroups) {
		if len(d.envFallback) > 0 {
			d.environmentFallbackDependents(groupPrefix+g, entry.ID, out)
		}
		for id := range d.group[g] {
			if !id.Equal(entry.ID) {
				out[id] = struct{}{}
			}
		}
	}
	if entry.ID.NS != "" {
		if len(d.envFallback) > 0 {
			d.environmentFallbackDependents(nsPrefix+entry.ID.NS, entry.ID, out)
		}
		for id := range d.ns[entry.ID.NS] {
			if !id.Equal(entry.ID) {
				out[id] = struct{}{}
			}
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
	if name := envVariableName(entry); name != "" {
		if d.variables == nil {
			d.variables = make(map[registry.ID]string)
			d.variableNames = make(map[string]map[registry.ID]struct{})
		}
		d.variables[entry.ID] = name
		addIDToStringMap(d.variableNames, name, entry.ID)
	}
	if len(keys.env) > 0 && d.env == nil {
		d.env = make(map[string]map[registry.ID]struct{})
		d.envFallback = make(map[string]map[registry.ID]struct{})
	}
	for _, name := range keys.env {
		addIDToStringMap(d.env, name, entry.ID)
		addIDToStringMap(d.envFallback, environmentFallbackKey(entry.ID.NS, name), entry.ID)
	}
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
	if name, variable := d.variables[entryID]; variable {
		delete(d.variables, entryID)
		delIDFromStringMap(d.variableNames, name, entryID)
	}
	for _, name := range keys.env {
		delIDFromStringMap(d.env, name, entryID)
		delIDFromStringMap(d.envFallback, environmentFallbackKey(entryID.NS, name), entryID)
	}
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
	declarations := append([]string(nil), entry.Meta.GetSlice(registry.TagDependsOn)...)
	var keys entryDepKeys
	if builtIn, ok := resolver.(*Resolver); ok {
		var registryReferences []string
		registryReferences, keys.env = builtIn.extractForTopology(entry)
		declarations = append(declarations, registryReferences...)
	} else {
		// Custom resolver outputs retain their explicit registry semantics.
		if resolver != nil {
			declarations = append(declarations, resolver.Extract(entry)...)
		}
		if entry.Data != nil {
			seen := make(map[string]struct{})
			resolverExtractEnvReferences(entry.Data.Data(), true, func(name string) {
				if _, duplicate := seen[name]; !duplicate {
					seen[name] = struct{}{}
					keys.env = append(keys.env, name)
				}
			})
		}
	}
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

func (d *DepIndex) environmentFallbackDependents(key string, target registry.ID, out map[registry.ID]struct{}) {
	for id := range d.envFallback[key] {
		if id.Equal(target) {
			continue
		}
		for _, name := range d.ownDeps[id].env {
			if len(d.variableNames[name]) == 0 && environmentFallbackKey(id.NS, name) == key {
				out[id] = struct{}{}
				break
			}
		}
	}
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
