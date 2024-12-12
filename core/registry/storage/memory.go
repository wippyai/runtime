package storage

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
)

// MemoryStorage is an in-memory implementation of the registry.Storage interface.
type MemoryStorage struct {
	versions map[uint]registry.Version // Use map for efficient lookup
	actions  map[uint]registry.OperationSet
	mutex    sync.RWMutex
}

// NewMemory creates a new MemoryStorage.
func NewMemory() *MemoryStorage {
	return &MemoryStorage{
		versions: map[uint]registry.Version{},
		actions:  make(map[uint]registry.OperationSet),
	}
}

// Versions returns a list of all versions in the storage.
func (m *MemoryStorage) Versions() ([]registry.Version, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	versions := make([]registry.Version, 0, len(m.versions))
	for _, v := range m.versions {
		versions = append(versions, v)
	}
	return versions, nil
}

// Get returns the OperationSet associated with a specific version.
func (m *MemoryStorage) Get(version registry.Version) (registry.OperationSet, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	actions, ok := m.actions[version.ID()]
	if !ok {
		return nil, fmt.Errorf("version not found: %s", version)
	}

	// Return a copy to prevent external modification
	actionsCopy := make(registry.OperationSet, len(actions))
	copy(actionsCopy, actions)
	return actionsCopy, nil
}

// Save records a set of actions and creates a new version.
func (m *MemoryStorage) Save(newVersion registry.Version, actions registry.OperationSet) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Validate actions - ensure no conflicts for Create within this set of actions
	createdPaths := make(map[registry.Path]bool)
	for _, action := range actions {
		if action.Kind == registry.Create {
			if _, exists := createdPaths[action.Entry.Path]; exists {
				return fmt.Errorf("conflict: multiple create actions for path '%s' in the same version", action.Entry.Path)
			}
			createdPaths[action.Entry.Path] = true
		}
	}

	clonedActions := make(registry.OperationSet, len(actions))
	for i, action := range actions {
		clonedActions[i] = registry.Action{
			Kind: action.Kind,
			Entry: registry.Entry{
				Path: action.Entry.Path,
				Kind: action.Entry.Kind,
				Meta: action.Entry.Meta,
				Data: action.Entry.Data,
			},
		}
	}

	m.actions[newVersion.ID()] = actions
	m.versions[newVersion.ID()] = newVersion
	return nil
}
