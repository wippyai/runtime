// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sort"

	"github.com/wippyai/runtime/api/registry"
)

// SortChangeSet sorts a changeset by dependencies to ensure proper application order.
//
// Registry listeners apply each operation against the live subsystem graph. That
// means an entry that is still imported by another live entry cannot be deleted
// until the importer has been updated or deleted. Creates/updates form the
// target-side graph, deletes clean up the source-side graph, and extra
// operation-level edges preserve same-ID replacement and
// importer-before-dependency-delete cases.
func (b *StateBuilder) SortChangeSet(fromState registry.State, changeSet registry.ChangeSet) (registry.ChangeSet, error) {
	if len(changeSet) == 0 {
		return changeSet, nil
	}

	// Defense in depth: operate on a copy normalized to a stable lexicographic
	// order before computing constraint indexes. stableTopologicalOrder breaks
	// ties between independent operations by their index in changeSet, so a
	// caller passing the same logical set in different orders would otherwise
	// produce different output. Normalizing here makes SortChangeSet
	// input-order-invariant regardless of how upstream code populates its
	// slices.
	changeSet = sortChangeSetInputForStableOrder(changeSet)

	constraints := make(map[int]map[int]struct{})
	addConstraint := func(before, after int) {
		if before == after {
			return
		}
		if constraints[before] == nil {
			constraints[before] = make(map[int]struct{})
		}
		constraints[before][after] = struct{}{}
	}

	opIndexesByID := make(map[registry.ID][]int, len(changeSet))
	createUpdateIndexesByID := make(map[registry.ID][]int, len(changeSet))
	deleteIndexesByID := make(map[registry.ID][]int, len(changeSet))
	createUpdateEntries := make([]registry.Entry, 0, len(changeSet))

	for i, operation := range changeSet {
		opIndexesByID[operation.Entry.ID] = append(opIndexesByID[operation.Entry.ID], i)
		if operation.Kind == registry.EntryDelete {
			deleteIndexesByID[operation.Entry.ID] = append(deleteIndexesByID[operation.Entry.ID], i)
		} else {
			createUpdateIndexesByID[operation.Entry.ID] = append(createUpdateIndexesByID[operation.Entry.ID], i)
			createUpdateEntries = append(createUpdateEntries, operation.Entry)
		}
	}

	// Same-ID replacement needs the old entry removed before the new entry can
	// be created. If live dependents still point at the old ID, the dependent
	// update/delete constraints below must run first.
	for id, deleteIndexes := range deleteIndexesByID {
		for _, deleteIndex := range deleteIndexes {
			for _, createUpdateIndex := range createUpdateIndexesByID[id] {
				if changeSet[createUpdateIndex].Kind == registry.EntryCreate {
					addConstraint(deleteIndex, createUpdateIndex)
				}
			}
		}
	}

	// Target-side dependencies: create/update dependencies before their
	// dependents, but only when both sides are part of this changeset.
	createUpdateUniverse := dependencyUniverse(createUpdateEntries)
	for i, operation := range changeSet {
		if operation.Kind == registry.EntryDelete {
			continue
		}
		for _, depID := range b.entryDependencyIDs(operation.Entry, createUpdateUniverse) {
			for _, depIndex := range createUpdateIndexesByID[depID] {
				addConstraint(depIndex, i)
			}
		}
	}

	// Source-side dependencies: every live dependent must be updated or deleted
	// before a dependency can be removed. Missing dependent operations are left
	// unconstrained so the normal listener validation rejects invalid changesets.
	fromUniverse := dependencyUniverse(fromState)
	for _, entry := range fromState {
		for _, depID := range b.entryDependencyIDs(entry, fromUniverse) {
			deleteIndexes := deleteIndexesByID[depID]
			if len(deleteIndexes) == 0 {
				continue
			}
			for _, dependentIndex := range opIndexesByID[entry.ID] {
				kind := changeSet[dependentIndex].Kind
				if kind != registry.EntryUpdate && kind != registry.EntryDelete {
					continue
				}
				for _, deleteIndex := range deleteIndexes {
					addConstraint(dependentIndex, deleteIndex)
				}
			}
		}
	}

	order, ok := stableTopologicalOrder(len(changeSet), constraints)
	if !ok {
		b.log.Warn("operation dependency cycle detected while sorting changeset; using deterministic fallback")
		return b.fallbackSortChangeSet(fromState, changeSet), nil
	}

	sortedChangeSet := make(registry.ChangeSet, 0, len(changeSet))
	for _, index := range order {
		sortedChangeSet = append(sortedChangeSet, changeSet[index])
	}

	return sortedChangeSet, nil
}

type dependencySet struct {
	entries map[registry.ID]registry.Entry
	groups  map[string][]registry.ID
	ns      map[string][]registry.ID
}

func dependencyUniverse(entries []registry.Entry) dependencySet {
	universe := dependencySet{
		entries: make(map[registry.ID]registry.Entry, len(entries)),
		groups:  make(map[string][]registry.ID),
		ns:      make(map[string][]registry.ID),
	}
	for _, entry := range entries {
		universe.entries[entry.ID] = entry
		for _, group := range entry.Meta.GetSlice(registry.TagGroups) {
			universe.groups[group] = append(universe.groups[group], entry.ID)
		}
		if entry.ID.NS != "" {
			universe.ns[entry.ID.NS] = append(universe.ns[entry.ID.NS], entry.ID)
		}
	}
	return universe
}

func (b *StateBuilder) entryDependencyIDs(entry registry.Entry, universe dependencySet) []registry.ID {
	dependencies := entry.Meta.GetSlice(registry.TagDependsOn)
	if b.resolver != nil {
		dependencies = append(dependencies, b.resolver.Extract(entry)...)
	}

	seen := make(map[registry.ID]struct{}, len(dependencies))
	out := make([]registry.ID, 0, len(dependencies))
	add := func(id registry.ID) {
		if id.Equal(entry.ID) {
			return
		}
		if _, ok := universe.entries[id]; !ok {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	for _, dep := range dependencies {
		depType, value := parseDependency(dep)
		switch depType {
		case "direct":
			add(resolveDependencyID(entry.ID.NS, value))
		case "group":
			for _, id := range universe.groups[value] {
				add(id)
			}
		case "namespace":
			for _, id := range universe.ns[value] {
				add(id)
			}
		}
	}

	return out
}

func stableTopologicalOrder(count int, constraints map[int]map[int]struct{}) ([]int, bool) {
	indegree := make([]int, count)
	dependents := make(map[int][]int, len(constraints))
	for before, afterSet := range constraints {
		for after := range afterSet {
			dependents[before] = append(dependents[before], after)
			indegree[after]++
		}
	}
	for _, list := range dependents {
		sort.Ints(list)
	}

	ready := make([]int, 0, count)
	for i := 0; i < count; i++ {
		if indegree[i] == 0 {
			ready = append(ready, i)
		}
	}

	order := make([]int, 0, count)
	for len(ready) > 0 {
		index := ready[0]
		ready = ready[1:]
		order = append(order, index)
		for _, dependent := range dependents[index] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = appendSortedInt(ready, dependent)
			}
		}
	}

	return order, len(order) == count
}

func appendSortedInt(values []int, value int) []int {
	pos := sort.SearchInts(values, value)
	values = append(values, 0)
	copy(values[pos+1:], values[pos:])
	values[pos] = value
	return values
}

// sortChangeSetInputForStableOrder returns a copy of changeSet sorted by
// (entry.ID.NS, entry.ID.Name, kind). The output of SortChangeSet uses element
// indexes to break topological-sort ties, so without this normalization the
// caller's slice order leaks into the sorted result.
func sortChangeSetInputForStableOrder(changeSet registry.ChangeSet) registry.ChangeSet {
	normalized := make(registry.ChangeSet, len(changeSet))
	copy(normalized, changeSet)
	sort.SliceStable(normalized, func(i, j int) bool {
		a, b := normalized[i].Entry.ID, normalized[j].Entry.ID
		if a.NS != b.NS {
			return a.NS < b.NS
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return normalized[i].Kind < normalized[j].Kind
	})
	return normalized
}

// fallbackSortChangeSet keeps SortChangeSet's historical no-error behavior for
// cyclic dependency graphs. It prefers install/update before cleanup, but the
// result may still be invalid and must be rejected by normal apply validation.
func (b *StateBuilder) fallbackSortChangeSet(fromState registry.State, changeSet registry.ChangeSet) registry.ChangeSet {
	deleteOps := make([]registry.Operation, 0, len(changeSet))
	createUpdateOps := make([]registry.Operation, 0, len(changeSet))
	for _, operation := range changeSet {
		if operation.Kind == registry.EntryDelete {
			deleteOps = append(deleteOps, operation)
		} else {
			createUpdateOps = append(createUpdateOps, operation)
		}
	}

	sortedChangeSet := make(registry.ChangeSet, 0, len(changeSet))
	if len(createUpdateOps) > 0 {
		sortedCreateUpdates := b.sortCreateUpdateOperations(createUpdateOps)
		sortedChangeSet = append(sortedChangeSet, sortedCreateUpdates...)
	}
	if len(deleteOps) > 0 {
		sortedDeletes := b.sortDeleteOperations(fromState, deleteOps)
		sortedChangeSet = append(sortedChangeSet, sortedDeletes...)
	}
	return sortedChangeSet
}

// sortDeleteOperations sorts delete operations in reverse dependency order.
func (b *StateBuilder) sortDeleteOperations(fromState registry.State, deleteOps []registry.Operation) []registry.Operation {
	// Build map for O(1) lookup of current state entries
	fromStateMap := make(map[registry.ID]registry.Entry, len(fromState))
	for _, entry := range fromState {
		fromStateMap[entry.ID] = entry
	}

	// Extract entries with correct dependency information from current state
	deleteEntries := make([]registry.Entry, 0, len(deleteOps))
	for _, operation := range deleteOps {
		if stateEntry, exists := fromStateMap[operation.Entry.ID]; exists {
			// Use entry from current state (has correct dependencies)
			deleteEntries = append(deleteEntries, stateEntry)
		} else {
			// Fallback to operation entry if not found in current state
			deleteEntries = append(deleteEntries, operation.Entry)
		}
	}

	// Sort by dependencies with cycle fallback
	sortedEntries := b.sortEntriesWithFallback(deleteEntries)

	// Map back to operations in reverse order (dependents before dependencies)
	result := make([]registry.Operation, 0, len(deleteOps))
	for i := len(sortedEntries) - 1; i >= 0; i-- {
		entry := sortedEntries[i]
		for _, operation := range deleteOps {
			if operation.Entry.ID.Equal(entry.ID) {
				result = append(result, operation)
				break
			}
		}
	}

	return result
}

// sortCreateUpdateOperations sorts create and update operations in forward dependency order
func (b *StateBuilder) sortCreateUpdateOperations(createUpdateOps []registry.Operation) []registry.Operation {
	// Extract entries from operations
	entries := make([]registry.Entry, 0, len(createUpdateOps))
	for _, operation := range createUpdateOps {
		entries = append(entries, operation.Entry)
	}

	// Sort by dependencies with cycle fallback
	sortedEntries := b.sortEntriesWithFallback(entries)

	// Map back to operations in forward order (dependencies before dependents)
	result := make([]registry.Operation, 0, len(createUpdateOps))
	for _, entry := range sortedEntries {
		for _, operation := range createUpdateOps {
			if operation.Entry.ID.Equal(entry.ID) {
				result = append(result, operation)
				break
			}
		}
	}

	return result
}

// sortEntriesWithFallback sorts entries by dependencies with graceful cycle handling
func (b *StateBuilder) sortEntriesWithFallback(entries []registry.Entry) []registry.Entry {
	sortedEntries, err := SortEntriesByDependency(entries, b.resolver)
	if err != nil {
		// On cycle detection, fall back to lexicographical sort
		sortedEntries = make([]registry.Entry, len(entries))
		copy(sortedEntries, entries)
		sort.Slice(sortedEntries, func(i, j int) bool {
			return sortedEntries[i].ID.String() < sortedEntries[j].ID.String()
		})
	}
	return sortedEntries
}
