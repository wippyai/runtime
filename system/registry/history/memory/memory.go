// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
)

// Storage is an in-memory implementation of the registry.History interface.
type Storage struct {
	resolutions map[uint]*registry.DependencyResolution
	versions    map[uint]registry.Version
	actions     map[uint]registry.ChangeSet
	head        registry.Version
	mutex       sync.RWMutex
}

// New creates a new Storage.
func New() *Storage {
	// Create v0 as the root version
	v0 := version.New(0)

	m := &Storage{
		resolutions: make(map[uint]*registry.DependencyResolution),
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
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID() < versions[j].ID() })
	return versions, nil
}

func (m *Storage) MaxVersionID() (uint, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	var maxID uint
	for id := range m.versions {
		if id > maxID {
			maxID = id
		}
	}
	return maxID, nil
}

func (m *Storage) GetVersion(id uint) (registry.Version, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	stored, ok := m.versions[id]
	if !ok {
		return nil, NewVersionNotFoundError(fmt.Sprintf("%d", id))
	}
	return stored, nil
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

// ReplayChanges snapshots root-to-target changesets so callbacks may safely
// call back into the history. The structural root version is not replayed.
func (m *Storage) ReplayChanges(ctx context.Context, target registry.Version, apply func(registry.ChangeSet) error) error {
	m.mutex.RLock()
	changesets, err := m.replayChanges(ctx, target)
	m.mutex.RUnlock()
	if err != nil {
		return err
	}
	for _, changes := range changesets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := apply(changes); err != nil {
			return err
		}
	}
	return nil
}

func (m *Storage) replayChanges(ctx context.Context, target registry.Version) ([]registry.ChangeSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stored, ok := m.versions[target.ID()]
	if !ok {
		return nil, NewVersionNotFoundError(target.String())
	}
	lineage := make([]uint, 0)
	for current := stored; current != nil && current.ID() > registry.RootVersion; current = current.Previous() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineage = append(lineage, current.ID())
	}
	changesets := make([]registry.ChangeSet, 0, len(lineage))
	for i := len(lineage) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		actions, ok := m.actions[lineage[i]]
		if !ok {
			return nil, NewVersionNotFoundError(fmt.Sprintf("%d", lineage[i]))
		}
		changesets = append(changesets, cloneChangeSet(actions))
	}
	return changesets, nil
}

func cloneEntry(e registry.Entry) registry.Entry {
	var meta attrs.Bag
	if e.Meta != nil {
		meta = make(attrs.Bag, len(e.Meta))
		for k, v := range e.Meta {
			meta[k] = snapshotValue(v)
		}
	}
	var data payload.Payload
	if e.Data != nil {
		data = payload.NewPayload(snapshotValue(e.Data.Data()), e.Data.Format())
	}
	return registry.Entry{
		ID:             e.ID,
		Kind:           e.Kind,
		Meta:           meta,
		Data:           data,
		DependencyRoot: e.DependencyRoot,
	}
}

func snapshotValue(value any) any {
	switch v := value.(type) {
	case attrs.Bag:
		cloned := make(attrs.Bag, len(v))
		for key, item := range v {
			cloned[key] = snapshotValue(item)
		}
		return cloned
	case map[string]any:
		cloned := make(map[string]any, len(v))
		for key, item := range v {
			cloned[key] = snapshotValue(item)
		}
		return cloned
	case map[any]any:
		cloned := make(map[any]any, len(v))
		for key, item := range v {
			cloned[snapshotValue(key)] = snapshotValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(v))
		for i, item := range v {
			cloned[i] = snapshotValue(item)
		}
		return cloned
	case []string:
		return append([]string(nil), v...)
	case []byte:
		return append([]byte(nil), v...)
	case payload.Payload:
		return payload.NewPayload(snapshotValue(v.Data()), v.Format())
	default:
		return value
	}
}

func cloneChangeSet(actions registry.ChangeSet) registry.ChangeSet {
	cloned := make(registry.ChangeSet, len(actions))
	for i, op := range actions {
		cloned[i] = registry.Operation{Kind: op.Kind, Entry: cloneEntry(op.Entry)}
		if op.OriginalEntry != nil {
			original := cloneEntry(*op.OriginalEntry)
			cloned[i].OriginalEntry = &original
		}
	}
	return cloned
}

// Save records a set of actions and creates a new version.
func (m *Storage) Save(newVersion registry.Version, actions registry.ChangeSet, head bool) error {
	return m.SaveWithDependencyResolution(newVersion, actions, nil, head)
}

func (m *Storage) SaveWithDependencyResolution(newVersion registry.Version, actions registry.ChangeSet, resolution *registry.DependencyResolution, head bool) error {
	var canonicalResolution *registry.DependencyResolution
	if resolution != nil {
		canonicalResolution = resolution.Canonical()
		if !canonicalResolution.Valid() {
			return registry.ErrInvalidDependencyResolution
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if newVersion.ID() != registry.RootVersion && newVersion.Previous() == nil {
		return fmt.Errorf("non-root version %d has no parent", newVersion.ID())
	}
	if _, exists := m.versions[newVersion.ID()]; exists {
		if newVersion.ID() == registry.RootVersion && len(actions) == 0 && resolution == nil {
			if head {
				m.head = m.versions[registry.RootVersion]
			}
			return nil
		}
		return fmt.Errorf("version %d already exists", newVersion.ID())
	}
	storedVersion := newVersion
	if previous := newVersion.Previous(); previous != nil {
		storedParent, exists := m.versions[previous.ID()]
		if !exists {
			return NewVersionNotFoundError(previous.String())
		}
		if head && !memoryHeadMatchesParent(m.head, previous) {
			return fmt.Errorf("history head changed: expected version %d", previous.ID())
		}
		// Preserve the caller's version object when it was constructed directly
		// from the canonical stored parent. Rebuild foreign or truncated caller
		// lineages in O(1), keeping sequential saves cheap for long histories.
		if !sameVersionInstance(previous, storedParent) {
			storedVersion = version.FromParent(storedParent, newVersion.ID())
		}
	}

	m.actions[newVersion.ID()] = cloneChangeSet(actions)
	m.versions[newVersion.ID()] = storedVersion
	if canonicalResolution != nil {
		m.resolutions[newVersion.ID()] = canonicalResolution
	} else if previous := newVersion.Previous(); previous != nil {
		if inherited, ok := m.resolutions[previous.ID()]; ok {
			m.resolutions[newVersion.ID()] = inherited.Canonical()
		}
	}

	if head {
		m.head = storedVersion
	}

	return nil
}

func sameVersionInstance(left, right registry.Version) bool {
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	return leftValue.IsValid() && rightValue.IsValid() &&
		leftValue.Kind() == reflect.Pointer && rightValue.Kind() == reflect.Pointer &&
		leftValue.Type() == rightValue.Type() && leftValue.Pointer() == rightValue.Pointer()
}

func (m *Storage) GetDependencyResolution(v registry.Version) (*registry.DependencyResolution, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	resolution, ok := m.resolutions[v.ID()]
	if !ok {
		return nil, registry.ErrDependencyResolutionNotFound
	}
	return resolution.Canonical(), nil
}

func (m *Storage) CheckpointDependencyResolution(v registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	canonical := resolution.Canonical()
	if !canonical.Valid() {
		return registry.ErrInvalidDependencyResolution
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.versions[v.ID()]; !ok {
		return NewVersionNotFoundError(v.String())
	}
	if existing, ok := m.resolutions[v.ID()]; ok && existing.Digest != canonical.Digest {
		return fmt.Errorf("version %d already references dependency resolution %s, refusing %s", v.ID(), existing.Digest, canonical.Digest)
	}
	m.resolutions[v.ID()] = canonical
	return nil
}

func memoryHeadMatchesParent(head, parent registry.Version) bool {
	if head == nil {
		return parent != nil && parent.ID() == registry.RootVersion
	}
	return parent != nil && head.ID() == parent.ID()
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

	stored, ok := m.versions[v.ID()]
	if !ok {
		return NewVersionNotFoundError(v.String())
	}
	m.head = stored
	return nil
}

func (m *Storage) CompareAndSetHead(expected, target registry.Version) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	stored, ok := m.versions[target.ID()]
	if !ok {
		return NewVersionNotFoundError(target.String())
	}
	if !memoryHeadMatchesParent(m.head, expected) {
		return fmt.Errorf("history head changed: expected version %d", expected.ID())
	}
	m.head = stored
	return nil
}

func (m *Storage) CompareAndSetHeadWithDependencyResolution(expected, target registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	canonical := resolution.Canonical()
	if !canonical.Valid() {
		return registry.ErrInvalidDependencyResolution
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	stored, ok := m.versions[target.ID()]
	if !ok {
		return NewVersionNotFoundError(target.String())
	}
	if !memoryHeadMatchesParent(m.head, expected) {
		return fmt.Errorf("history head changed: expected version %d", expected.ID())
	}
	if existing, ok := m.resolutions[target.ID()]; ok && existing.Digest != canonical.Digest {
		return fmt.Errorf("version %d already references dependency resolution %s, refusing %s", target.ID(), existing.Digest, canonical.Digest)
	}
	m.resolutions[target.ID()] = canonical
	m.head = stored
	return nil
}
