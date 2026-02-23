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
	head registry.Version
	mu   sync.RWMutex
}

// New creates a new nil History instance.
func New() *History {
	return &History{}
}

// Save accepts a new version and updates the current head version.
// The changeset is not persisted. Setting head to true updates the current version.
func (n *History) Save(newVersion registry.Version, _ registry.ChangeSet, head bool) error {
	if head {
		n.mu.Lock()
		n.head = newVersion
		n.mu.Unlock()
	}
	return nil
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
