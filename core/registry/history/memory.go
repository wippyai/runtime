package history

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
)

// MemoryHistory is an in-memory implementation of the History interface.
type MemoryHistory struct {
	versions  []registry.Version
	actions   map[registry.Version][]registry.Action
	current   registry.Version
	lastMajor uint
	mutex     sync.RWMutex
}

// NewMemory creates a new MemoryHistory.
func NewMemory() *MemoryHistory {
	initialVersion := version.New(1, 0) // Start at v1.0
	return &MemoryHistory{
		versions:  []registry.Version{initialVersion},
		actions:   make(map[registry.Version][]registry.Action),
		current:   initialVersion,
		lastMajor: 0,
	}
}

// Versions returns a list of all versions in the history.
func (h *MemoryHistory) Versions() ([]registry.Version, error) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	versionsCopy := make([]registry.Version, len(h.versions))
	copy(versionsCopy, h.versions)
	return versionsCopy, nil
}

// Current returns the current version.
func (h *MemoryHistory) Current() (registry.Version, error) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.current, nil
}

// Seek sets the current version to the specified version.
func (h *MemoryHistory) Seek(to registry.Version) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if !h.versionExists(to) {
		return fmt.Errorf("version not found: %s", to)
	}

	h.current = to
	return nil
}

// Record records a set of actions and creates a new version.
func (h *MemoryHistory) Record(actions ...registry.Action) (registry.Version, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Determine the new version
	newVersion := version.New(h.lastMajor+1, uint(len(actions)))
	h.lastMajor++

	// Validate actions - ensure no conflicts for Create within this set of actions
	createdPaths := make(map[registry.Path]bool)
	for _, action := range actions {
		if action.Kind == registry.Create {
			if _, exists := createdPaths[action.Entry.Path]; exists {
				return nil, fmt.Errorf("conflict: multiple create actions for path '%s' in the same version", action.Entry.Path)
			}
			createdPaths[action.Entry.Path] = true
		}
	}

	// Clone actions to prevent external modification
	clonedActions := make([]registry.Action, len(actions))
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

	h.actions[newVersion] = clonedActions
	h.versions = append(h.versions, newVersion)
	h.current = newVersion

	return newVersion, nil
}

// GetActions returns the actions associated with a specific version.
func (h *MemoryHistory) GetActions(version registry.Version) ([]registry.Action, error) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	actions, ok := h.actions[version]
	if !ok {
		return nil, fmt.Errorf("version not found: %s", version)
	}

	actionsCopy := make([]registry.Action, len(actions))
	copy(actionsCopy, actions)
	return actionsCopy, nil
}

// versionExists checks if a version exists in the history.
func (h *MemoryHistory) versionExists(version registry.Version) bool {
	for _, v := range h.versions {
		if v.Equals(version) {
			return true
		}
	}
	return false
}
