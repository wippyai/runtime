package topology

import (
	"sort"

	"github.com/ponyruntime/pony/api/registry"
)

// SortChangeSet sorts a changeset by dependencies to ensure proper application order
func (b *StateBuilder) SortChangeSet(fromState registry.State, changeSet registry.ChangeSet) (registry.ChangeSet, error) {
	if len(changeSet) == 0 {
		return changeSet, nil
	}

	// Separate operations by type with pre-allocated capacity
	deleteOps := make([]registry.Operation, 0, len(changeSet))
	createUpdateOps := make([]registry.Operation, 0, len(changeSet))

	for _, operation := range changeSet {
		if operation.Kind == registry.Delete {
			deleteOps = append(deleteOps, operation)
		} else {
			createUpdateOps = append(createUpdateOps, operation)
		}
	}

	// Pre-allocate result with exact capacity
	sortedChangeSet := make(registry.ChangeSet, 0, len(changeSet))

	// Process deletes first (reverse dependency order)
	if len(deleteOps) > 0 {
		sortedDeletes := b.sortDeleteOperations(fromState, deleteOps)
		sortedChangeSet = append(sortedChangeSet, sortedDeletes...)
	}

	// Process creates and updates (forward dependency order)
	if len(createUpdateOps) > 0 {
		sortedCreateUpdates := b.sortCreateUpdateOperations(createUpdateOps)
		sortedChangeSet = append(sortedChangeSet, sortedCreateUpdates...)
	}

	return sortedChangeSet, nil
}

// sortDeleteOperations sorts delete operations in reverse dependency order
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
			if operation.Entry.ID == entry.ID {
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
			if operation.Entry.ID == entry.ID {
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
