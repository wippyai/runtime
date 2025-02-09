package registry

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"github.com/ponyruntime/pony/system/registry/loader"
	"go.uber.org/zap"
)

// StateBuilder constructs registry states and calculates state transitions
type StateBuilder struct {
	log *zap.Logger
}

// NewStateBuilder creates a new StateBuilder instance with the provided logger
func NewStateBuilder(log *zap.Logger) registry.StateBuilder {
	return &StateBuilder{
		log: log,
	}
}

// BuildState constructs a registry State by applying the version history up to targetVersion.
// It processes operations in version order and handles create/update/delete operations.
func (b *StateBuilder) BuildState(history registry.History, targetVersion registry.Version) (registry.State, error) {
	vm := version.NewVersionMap()
	versions, err := history.Versions()
	if err != nil {
		return nil, fmt.Errorf("failed to get versions from history: %w", err)
	}

	for _, v := range versions {
		err := vm.Add(v)
		if err != nil {
			b.log.Error("failed to add version to version map",
				zap.String("version", v.String()),
				zap.Error(err),
			)
		}
	}

	path, err := vm.Path(version.New(registry.RootVersion), targetVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to get path from root to version %v: %w", targetVersion, err)
	}

	stateMap := make(map[registry.Name]registry.Entry) // Use a map for efficient lookups and overwrites

	for _, ver := range path {
		b.log.Debug("building version transition", zap.String("version", ver.String()))

		changeSet, err := history.Get(ver)
		if err != nil {
			return nil, fmt.Errorf("failed to get changeset for version %v: %w", ver, err)
		}

		for _, operation := range changeSet {
			switch operation.Kind {
			case registry.Create:
				if _, exists := stateMap[operation.Entry.ID]; exists {
					b.log.Error("conflict: entry already exists",
						zap.String("path", string(operation.Entry.ID)),
						zap.String("version", ver.String()),
					)
				} else {
					stateMap[operation.Entry.ID] = operation.Entry
				}
			case registry.Update:
				if _, exists := stateMap[operation.Entry.ID]; !exists {
					b.log.Warn("update on non-existent entry",
						zap.String("path", string(operation.Entry.ID)),
						zap.String("version", ver.String()),
					)
				}
				// Update even if it doesn't exist (effectively a create)
				stateMap[operation.Entry.ID] = operation.Entry
			case registry.Delete:
				if _, exists := stateMap[operation.Entry.ID]; !exists {
					b.log.Warn("delete on non-existent entry",
						zap.String("path", string(operation.Entry.ID)),
						zap.String("version", ver.String()),
					)
				}
				delete(stateMap, operation.Entry.ID)
			}
		}
	}

	// Convert the state map back to a slice
	state := make(registry.State, 0, len(stateMap))

	// Extract keys for sorting
	paths := make([]string, 0, len(stateMap))
	for path := range stateMap {
		paths = append(paths, string(path))
	}

	// Sort the paths
	sort.Strings(paths)

	// Append entries to state in sorted order
	for _, path := range paths {
		state = append(state, stateMap[registry.Name(path)])
	}

	return state, nil
}

// BuildDelta calculates the changes required to transition from one state to another.
// It returns a ChangeSet containing create, update, and delete operations in dependency order.
func (b *StateBuilder) BuildDelta(from, to registry.State) (registry.ChangeSet, error) {
	// Convert the states to maps for easier lookup
	fromStateMap := make(map[registry.Name]registry.Entry)
	for _, entry := range from {
		fromStateMap[entry.ID] = entry
	}

	toStateMap := make(map[registry.Name]registry.Entry)
	for _, entry := range to {
		toStateMap[entry.ID] = entry
	}

	// Collect entries for each operation type
	var creates, updates, deletes []registry.Entry

	// Find new and updated entries
	for _, toEntry := range to {
		fromEntry, exists := fromStateMap[toEntry.ID]
		if !exists {
			creates = append(creates, toEntry)
		} else if !reflect.DeepEqual(fromEntry, toEntry) {
			updates = append(updates, toEntry)
		}
	}

	// Find deleted entries
	for _, fromEntry := range from {
		if _, exists := toStateMap[fromEntry.ID]; !exists {
			deletes = append(deletes, fromEntry)
		}
	}

	// Build final changeset in correct order
	delta := make(registry.ChangeSet, 0, len(creates)+len(updates)+len(deletes))

	// 1. Handle Deletes - in reverse dependency order
	sortedDeletes := loader.SortEntriesByDependency(deletes)
	// Process deletes in reverse order to ensure dependents are deleted first
	for i := len(sortedDeletes) - 1; i >= 0; i-- {
		delta = append(delta, registry.Operation{
			Kind:  registry.Delete,
			Entry: sortedDeletes[i],
		})
	}

	// 2. Handle Updates - dependency order based on final state
	sortedUpdates := loader.SortEntriesByDependency(updates)
	for _, entry := range sortedUpdates {
		delta = append(delta, registry.Operation{
			Kind:  registry.Update,
			Entry: entry,
		})
	}

	// 3. Handle Creates - dependency order
	sortedCreates := loader.SortEntriesByDependency(creates)
	for _, entry := range sortedCreates {
		delta = append(delta, registry.Operation{
			Kind:  registry.Create,
			Entry: entry,
		})
	}

	return delta, nil
}
