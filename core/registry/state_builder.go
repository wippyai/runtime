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

func NewStateBuilder(log *zap.Logger) registry.Builder {
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
	stateMap := make(map[registry.Path]registry.Entry) // Use a map for efficient lookups and overwrites

	for _, ver := range path {
		b.log.Debug("building version transition", zap.String("version", ver.String()))

		changeSet, err := history.Get(ver)
		if err != nil {
			return nil, fmt.Errorf("failed to get changeset for version %v: %w", ver, err)
		}

		for _, operation := range changeSet {
			switch operation.Kind {
			case registry.Create:
				if _, exists := stateMap[operation.Entry.Path]; exists {
					b.log.Error("conflict: entry already exists",
						zap.String("path", string(operation.Entry.Path)),
						zap.String("version", ver.String()),
					)
				} else {
					stateMap[operation.Entry.Path] = operation.Entry
				}
			case registry.Update:
				if _, exists := stateMap[operation.Entry.Path]; !exists {
					b.log.Warn("update on non-existent entry",
						zap.String("path", string(operation.Entry.Path)),
						zap.String("version", ver.String()),
					)
				}
				// Update even if it doesn't exist (effectively a create)
				stateMap[operation.Entry.Path] = operation.Entry
			case registry.Delete:
				if _, exists := stateMap[operation.Entry.Path]; !exists {
					b.log.Warn("delete on non-existent entry",
						zap.String("path", string(operation.Entry.Path)),
						zap.String("version", ver.String()),
					)
				}
				delete(stateMap, operation.Entry.Path)
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
		state = append(state, stateMap[registry.Path(path)])
	}

	return state, nil
}

func (b *StateBuilder) BuildDelta(history registry.History, from, to registry.Version) (registry.ChangeSet, error) {
	// Note: This implementation of BuildDelta relies on building the full states at both the 'from' and 'to' versions
	// using BuildState, which is less efficient than directly processing changesets. However, it prioritizes
	// correctness by ensuring a minimal ChangeSet and handling parent-child relationships correctly during
	// creation and deletion.

	// Build the state at the 'from' version.
	fromState, err := b.BuildState(history, from)
	if err != nil {
		return nil, fmt.Errorf("failed to build state at 'from' version %v: %w", from, err)
	}

	// Build the state at the 'to' version.
	toState, err := b.BuildState(history, to)
	if err != nil {
		return nil, fmt.Errorf("failed to build state at 'to' version %v: %w", to, err)
	}

	// Convert the states to maps for easier lookup.
	fromStateMap := make(map[registry.Path]registry.Entry)
	for _, entry := range fromState {
		fromStateMap[entry.Path] = entry
	}

	toStateMap := make(map[registry.Path]registry.Entry)
	for _, entry := range toState {
		toStateMap[entry.Path] = entry
	}

	// Calculate the delta.
	creates := make(registry.ChangeSet, 0)
	updates := make(registry.ChangeSet, 0)
	deletes := make(registry.ChangeSet, 0)

	// Find new and updated entries.
	for _, toEntry := range toState {
		fromEntry, exists := fromStateMap[toEntry.Path]
		if !exists {
			// Entry exists in 'to' but not in 'from' - Create operation.
			creates = append(creates, registry.Operation{Kind: registry.Create, Entry: toEntry})
		} else if !reflect.DeepEqual(fromEntry, toEntry) {
			// Entry exists in both but is different - Update operation.
			updates = append(updates, registry.Operation{Kind: registry.Update, Entry: toEntry})
		}
	}

	// Find deleted entries.
	for _, fromEntry := range fromState {
		if _, exists := toStateMap[fromEntry.Path]; !exists {
			deletes = append(deletes, registry.Operation{Kind: registry.Delete, Entry: fromEntry})
		}
	}

	// Sort operations for correct order of execution:
	// 1. Deletes: Sort by path in reverse order (children first).
	sort.Slice(deletes, func(i, j int) bool {
		return deletes[i].Entry.Path > deletes[j].Entry.Path
	})

	// 2. Creates: Sort by path in ascending order (parents first).
	sort.Slice(creates, func(i, j int) bool {
		return creates[i].Entry.Path < creates[j].Entry.Path
	})

	// Concatenate operations to form the final ChangeSet.
	delta := append(deletes, updates...)
	delta = append(delta, creates...)

	return delta, nil
}
