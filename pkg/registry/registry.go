package registry

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
)

type reg struct {
	history        registry.History
	runner         registry.Runner
	builder        registry.StateBuilder
	state          registry.State
	mu             sync.RWMutex
	currentVersion registry.Version
	log            *zap.Logger
}

func NewRegistry(
	history registry.History,
	runner registry.Runner,
	builder registry.StateBuilder,
	log *zap.Logger,
) registry.Registry {
	return &reg{
		history: history,
		runner:  runner,
		builder: builder,
		state:   registry.State{},
		log:     log,
	}
}

// --- EntryReader Interface Implementation ---

func (r *reg) GetAllEntries() ([]registry.Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state, nil
}

func (r *reg) GetEntry(path registry.ID) (registry.Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, entry := range r.state {
		if entry.ID == path {
			return entry, nil
		}
	}

	return registry.Entry{}, fmt.Errorf("entry not found: %s", path)
}

// --- StateWriter Interface Implementation ---

func (r *reg) Apply(ctx context.Context, changes registry.ChangeSet) (registry.Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	newVersion := version.FromParent(r.currentVersion, nextVersionID(r.currentVersion))

	r.log.Debug("applying changes", zap.Any("changes", changes), zap.Any("new_version", newVersion))

	newState, err := r.runner.Transition(ctx, r.state, changes)
	if err != nil {
		r.log.Error("failed to apply changes", zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				return nil, fmt.Errorf("failed to apply changes: %w, failed to rollback: %w", err, rerr)
			}
		}

		return nil, fmt.Errorf("failed to apply changes: %w", err)
	}

	r.log.Debug("saving new version", zap.Any("new_version", newVersion))

	err = r.history.Save(newVersion, changes, true)
	if err != nil {
		r.log.Error("failed to save new version", zap.Error(err))
		if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
			return nil, fmt.Errorf("failed to save new version: %w, failed to rollback: %w", err, rerr)
		}

		return nil, fmt.Errorf("failed to save new version: %w, recovered", err)
	}

	r.state = newState // This now use the state directly from the runner
	r.currentVersion = newVersion

	return newVersion, nil
}

func (r *reg) ApplyVersion(ctx context.Context, v registry.Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	target, err := r.builder.BuildState(r.history, v)
	if err != nil {
		return fmt.Errorf("failed build state of version %s: %w", v, err)
	}

	// Transition the changes through the runner
	newState, err := r.transitionState(ctx, r.state, target)
	if err != nil {
		r.log.Error("failed transition to version", zap.String("version", v.String()), zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				return fmt.Errorf("failed transition to version %s: %w, failed to rollback: %w", v, err, rerr)
			}
		}

		return fmt.Errorf("failed transition to version %s: %w", v, err)
	}

	r.state = newState
	r.currentVersion = v

	return nil
}

// rollback state desync between actual state in system and state in history
func (r *reg) rollback(ctx context.Context, from, to registry.State) error {
	r.log.Debug("attempting to rollback", zap.Any("from", from), zap.Any("to", to))

	partial, err := r.transitionState(ctx, from, to)
	if err == nil {
		return nil // success
	}

	r.state = partial // we remain in a desynced state

	return err
}

func (r *reg) transitionState(ctx context.Context, from, to registry.State) (registry.State, error) {
	r.log.Debug("transitioning state", zap.Any("from", from), zap.Any("to", to))

	cs, terr := r.builder.BuildDelta(from, to)
	if terr != nil {
		return nil, fmt.Errorf("failed to compute transition: %w", terr)
	}

	if len(cs) == 0 {
		return from, nil
	}

	return r.runner.Transition(ctx, from, cs)
}

func (r *reg) Current() (registry.Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.currentVersion == nil {
		return nil, fmt.Errorf("no current version")
	}

	return r.currentVersion, nil
}

// --- Helper Functions ---

func nextVersionID(head registry.Version) uint {
	if head == nil {
		return 1
	}
	return head.ID() + 1
}
