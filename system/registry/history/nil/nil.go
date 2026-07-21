// SPDX-License-Identifier: MPL-2.0

package nil

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
)

// History is a minimal History implementation that only tracks the current version
// without persisting any version history. It supports forward-only operations and
// returns errors when attempting to access historical data or rewind to previous versions.
//
// Use cases:
//   - When you need a Registry but don't require version history
//   - When you want to minimize memory overhead
//   - When you only need forward progression without rollback capability
type History struct {
	head              registry.Version
	resolution        *registry.DependencyResolution
	resolutionVersion uint
	mu                sync.RWMutex
}

// New creates a new nil History instance.
func New() *History {
	return &History{}
}

// Save accepts a new version and updates the current head version.
// The changeset is not persisted. Setting head to true updates the current version.
func (n *History) Save(newVersion registry.Version, _ registry.ChangeSet, head bool) error {
	return n.SaveWithDependencyResolution(newVersion, nil, nil, head)
}

func (n *History) SaveWithDependencyResolution(newVersion registry.Version, _ registry.ChangeSet, resolution *registry.DependencyResolution, head bool) error {
	var canonical *registry.DependencyResolution
	if resolution != nil {
		canonical = resolution.Canonical()
		if !canonical.Valid() {
			return registry.ErrInvalidDependencyResolution
		}
	}
	if head {
		n.mu.Lock()
		defer n.mu.Unlock()
		if previous := newVersion.Previous(); previous != nil && !nilHeadMatchesParent(n.head, previous) {
			return ErrRollbackNotSupported
		}
		n.head = newVersion
		if canonical != nil {
			n.resolution = canonical
			n.resolutionVersion = newVersion.ID()
		} else if n.resolution != nil {
			n.resolutionVersion = newVersion.ID()
		}
	}
	return nil
}

func (n *History) GetDependencyResolution(v registry.Version) (*registry.DependencyResolution, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.resolution == nil || n.resolutionVersion != v.ID() {
		return nil, registry.ErrDependencyResolutionNotFound
	}
	return n.resolution.Canonical(), nil
}

func (n *History) CheckpointDependencyResolution(v registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	canonical := resolution.Canonical()
	if !canonical.Valid() {
		return registry.ErrInvalidDependencyResolution
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.head != nil && n.head.ID() != v.ID() {
		return ErrHistoryNotAvailable
	}
	if n.head == nil && v.ID() != registry.RootVersion {
		return ErrHistoryNotAvailable
	}
	if n.resolution != nil && n.resolutionVersion == v.ID() && n.resolution.Digest != canonical.Digest {
		return ErrHistoryNotAvailable
	}
	n.resolution = canonical
	n.resolutionVersion = v.ID()
	return nil
}

func (n *History) CompareAndSetHeadWithDependencyResolution(expected, target registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	canonical := resolution.Canonical()
	if !canonical.Valid() {
		return registry.ErrInvalidDependencyResolution
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !nilHeadMatchesParent(n.head, expected) {
		return ErrRollbackNotSupported
	}
	if target.ID() != expected.ID() {
		return ErrHistoryNotAvailable
	}
	if n.resolution != nil && n.resolutionVersion == target.ID() && n.resolution.Digest != canonical.Digest &&
		!registry.CanRebaseDependencyResolution(n.resolution, canonical) {
		return ErrHistoryNotAvailable
	}
	n.resolution = canonical
	n.resolutionVersion = target.ID()
	n.head = target
	return nil
}

func nilHeadMatchesParent(head, parent registry.Version) bool {
	if head == nil {
		return parent != nil && parent.ID() == registry.RootVersion
	}
	return parent != nil && head.ID() == parent.ID()
}

// Get returns an error as version history is not available with nil History.
func (n *History) Get(_ registry.Version) (registry.ChangeSet, error) {
	return nil, ErrHistoryNotAvailable
}

// Versions returns an error as version history is not available with nil History.
func (n *History) Versions() ([]registry.Version, error) {
	return nil, ErrHistoryNotAvailable
}

// Head returns the current head version.
func (n *History) Head() (registry.Version, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.head == nil {
		return nil, ErrNoHeadVersion
	}

	return n.head, nil
}

// SetHead returns an error as rewinding is not supported with nil History.
func (n *History) SetHead(_ registry.Version) error {
	return ErrRollbackNotSupported
}
