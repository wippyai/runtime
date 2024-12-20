package registry

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"go.uber.org/zap"
	"reflect"
	"sort"
)

type StateBuilder struct {
	log *zap.Logger
}

func NewStateBuilder(log *zap.Logger) registry.StateBuilder {
	return &StateBuilder{
		log: log,
	}
}

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

	state := make(registry.State, 0)
	stateMap := make(map[registry.ID]registry.Entry) // Use a map for efficient lookups and overwrites

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
	state = make(registry.State, 0, len(stateMap))

	// Extract keys for sorting
	paths := make([]string, 0, len(stateMap))
	for path := range stateMap {
		paths = append(paths, string(path))
	}

	// Sort the paths
	sort.Strings(paths)

	// Append entries to state in sorted order
	for _, path := range paths {
		state = append(state, stateMap[registry.ID(path)])
	}

	return state, nil
}

func (b *StateBuilder) BuildDelta(from, to registry.State) (registry.ChangeSet, error) {
	// Convert the states to maps for easier lookup.
	fromStateMap := make(map[registry.ID]registry.Entry)
	for _, entry := range from {
		fromStateMap[entry.ID] = entry
	}

	toStateMap := make(map[registry.ID]registry.Entry)
	for _, entry := range to {
		toStateMap[entry.ID] = entry
	}

	// Calculate the delta.
	creates := make(registry.ChangeSet, 0)
	updates := make(registry.ChangeSet, 0)
	deletes := make(registry.ChangeSet, 0)

	// Find new and updated entries.
	for _, toEntry := range to {
		fromEntry, exists := fromStateMap[toEntry.ID]
		if !exists {
			// Entry exists in 'to' but not in 'from' - Create operation.
			creates = append(creates, registry.Operation{Kind: registry.Create, Entry: toEntry})
		} else if !reflect.DeepEqual(fromEntry, toEntry) {
			// Entry exists in both but is different - Update operation.
			// todo: check it carefully
			updates = append(updates, registry.Operation{Kind: registry.Update, Entry: toEntry})
		}
	}

	// Find deleted entries.
	for _, fromEntry := range from {
		if _, exists := toStateMap[fromEntry.ID]; !exists {
			deletes = append(deletes, registry.Operation{Kind: registry.Delete, Entry: fromEntry})
		}
	}

	// Sort operations for correct order of execution:
	// 1. Deletes: Sort by path in reverse order (children first).
	sort.Slice(deletes, func(i, j int) bool {
		return deletes[i].Entry.ID > deletes[j].Entry.ID
	})

	// 2. Updates: No specific order needed for updates.

	// 3. Creates: Sort by path in ascending order (parents first).
	sort.Slice(creates, func(i, j int) bool {
		return creates[i].Entry.ID < creates[j].Entry.ID
	})

	// Concatenate operations to form the final ChangeSet.
	delta := append(deletes, updates...)
	delta = append(delta, creates...)

	return delta, nil
}
