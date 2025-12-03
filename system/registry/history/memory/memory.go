package memory

import (
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
)

// Storage is an in-memory implementation of the registry.History interface.
type Storage struct {
	versions map[uint]registry.Version
	actions  map[uint]registry.ChangeSet
	head     registry.Version
	mutex    sync.RWMutex
}

// New creates a new Storage.
func New() *Storage {
	// Create v0 as the root version
	v0 := version.New(0)

	m := &Storage{
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
func (m *Storage) Versions() ([]registry.Version, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	versions := make([]registry.Version, 0, len(m.versions))
	for _, v := range m.versions {
		versions = append(versions, v)
	}
	return versions, nil
}

// Get returns the ChangeSet associated with a specific version.
func (m *Storage) Get(version registry.Version) (registry.ChangeSet, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	actions, ok := m.actions[version.ID()]
	if !ok {
		return nil, NewVersionNotFoundError(version.String())
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
	var meta attrs.Bag
	if e.Meta != nil {
		meta = make(attrs.Bag, len(e.Meta))
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
func (m *Storage) Save(newVersion registry.Version, actions registry.ChangeSet, head bool) error {
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
func (m *Storage) Head() (registry.Version, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.head == nil {
		return nil, ErrNoHeadVersion
	}

	return m.head, nil
}

// SetHead sets head version.
func (m *Storage) SetHead(v registry.Version) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.versions[v.ID()]; !ok {
		return NewVersionNotFoundError(v.String())
	}
	m.head = v
	return nil
}
