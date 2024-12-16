package history

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
)

// MemoryStorage is an in-memory implementation of the registry.History interface.
type MemoryStorage struct {
	versions map[uint]registry.Version
	actions  map[uint]registry.ChangeSet
	head     registry.Version
	mutex    sync.RWMutex
}

// NewMemory creates a new MemoryStorage.
func NewMemory() *MemoryStorage {
	return &MemoryStorage{
		versions: map[uint]registry.Version{},
		actions:  make(map[uint]registry.ChangeSet),
	}
}

// Versions returns a list of all versions in the history.
func (m *MemoryStorage) Versions() ([]registry.Version, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	versions := make([]registry.Version, 0, len(m.versions))
	for _, v := range m.versions {
		versions = append(versions, v)
	}
	return versions, nil
}

// Get returns the ChangeSet associated with a specific version.
func (m *MemoryStorage) Get(version registry.Version) (registry.ChangeSet, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	actions, ok := m.actions[version.ID()]
	if !ok {
		return nil, fmt.Errorf("version not found: %s", version)
	}

	// Return a copy to prevent external modification
	actionsCopy := make(registry.ChangeSet, len(actions))
	copy(actionsCopy, actions) // todo: not sure this is good
	return actionsCopy, nil
}

// Save records a set of actions and creates a new version.
func (m *MemoryStorage) Save(newVersion registry.Version, actions registry.ChangeSet, head bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.actions[newVersion.ID()] = actions
	m.versions[newVersion.ID()] = newVersion

	if head {
		m.head = newVersion
	}

	return nil
}

// Head returns the current head version of the history.
func (m *MemoryStorage) Head() (registry.Version, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.head == nil {
		return nil, fmt.Errorf("no head version set")
	}

	return m.head, nil
}
