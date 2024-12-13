package registry

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
)

type memreg struct {
	history        registry.History
	runner         registry.Runner
	stateBuilder   registry.StateBuilder
	state          registry.State
	mu             sync.RWMutex
	currentVersion registry.Version
}

func NewRegistry(history registry.History, runner registry.Runner, stateBuilder registry.StateBuilder) registry.Registry {
	return &memreg{
		history:      history,
		runner:       runner,
		stateBuilder: stateBuilder,
		state:        registry.State{},
	}
}

// --- EntryReader Interface Implementation ---

func (r *memreg) GetAllEntries() ([]registry.Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state, nil
}

func (r *memreg) GetEntry(path registry.Path) (registry.Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, entry := range r.state {
		if entry.Path == path {
			return entry, nil
		}
	}
	return registry.Entry{}, fmt.Errorf("entry not found: %s", path)
}

// --- StateWriter Interface Implementation ---

func (r *memreg) Apply(changes registry.ChangeSet) (registry.Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get current head version
	head, err := r.history.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get head version: %w", err)
	}

	newVersion := version.FromParent(r.currentVersion, nextVersionID(head))

	newState, err := r.runner.Run(r.state, changes)
	if err != nil {
		return nil, fmt.Errorf("failed to apply changes: %w", err)
	}

	err = r.history.Save(newVersion, changes, true)
	if err != nil {
		// try to rollback
		_, rollbackErr := r.runner.Run(newState, changes)
		if rollbackErr != nil {
			return nil, fmt.Errorf("failed to save new version: %w, failed to rollback: %w", err, rollbackErr)
		}

		return nil, fmt.Errorf("failed to save new version: %w", err)
	}

	r.state = newState // This now use the state directly from the runner
	r.currentVersion = newVersion

	return newVersion, nil
}

func (r *memreg) ApplyVersion(v registry.Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	changes, err := r.stateBuilder.BuildDelta(r.history, r.currentVersion, v)
	if err != nil {
		return fmt.Errorf("failed to calculate delta for version %s: %w", v, err)
	}

	// Run the changes through the runner
	newState, err := r.runner.Run(r.state, changes)
	if err != nil {
		return fmt.Errorf("failed to apply changes for version %s: %w", v, err)
	}

	r.state = newState
	r.currentVersion = v

	return nil
}

func (r *memreg) Current() (registry.Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentVersion, nil
}

// --- Helper Functions ---

func nextVersionID(head registry.Version) uint {
	if head == nil {
		return 1
	}
	return head.ID() + 1
}
