package history

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
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
	// Create v0 as the root version
	v0 := version.New(0)

	m := &MemoryStorage{
		versions: map[uint]registry.Version{
			0: v0,
		},
		actions: map[uint]registry.ChangeSet{
			0: {}, // Empty changeset for v0
		},
	}

	return m
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

	actionsCopy := make(registry.ChangeSet, len(actions))
	for i, op := range actions {
		actionsCopy[i] = registry.Operation{
			Kind:  op.Kind,
			Entry: cloneEntry(op.Entry),
		}
		if op.OriginalEntry != nil {
			clonedOriginal := cloneEntry(*op.OriginalEntry)
			actionsCopy[i].OriginalEntry = &clonedOriginal
		}
	}
	return actionsCopy, nil
}

func cloneEntry(e registry.Entry) registry.Entry {
	var meta registry.Metadata
	if e.Meta != nil {
		meta = make(registry.Metadata, len(e.Meta))
		for k, v := range e.Meta {
			meta[k] = v
		}
	}
	return registry.Entry{
		ID:   e.ID,
		Kind: e.Kind,
		Meta: meta,
		Data: e.Data,
	}
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

// SetHead sets head version.
func (m *MemoryStorage) SetHead(v registry.Version) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.versions[v.ID()]; !ok {
		return fmt.Errorf("version not found: %s", v)
	}
	m.head = v
	return nil
}
