package registry

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"go.uber.org/zap"
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
	for _, entry := range stateMap {
		state = append(state, entry)
	}

	return state, nil
}

func (b *StateBuilder) BuildDelta(history registry.History, from, to registry.Version) (registry.ChangeSet, error) {
	// Leave empty for now
	return nil, nil
}
