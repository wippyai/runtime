package registry

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
)

type Reg struct {
	history        registry.History
	runner         registry.Runner
	builder        registry.StateBuilder
	resolver       registry.DependencyResolver
	state          registry.State
	mu             sync.RWMutex
	currentVersion registry.Version
	versionNum     atomic.Uint64
	log            *zap.Logger
}

// NewRegistry creates a new registry instance.
func NewRegistry(
	history registry.History,
	runner registry.Runner,
	builder registry.StateBuilder,
	resolver registry.DependencyResolver,
	log *zap.Logger,
) *Reg {
	reg := &Reg{
		history:        history,
		runner:         runner,
		builder:        builder,
		resolver:       resolver,
		state:          registry.State{},
		log:            log,
		currentVersion: version.FromParent(nil, 0), // initial version
	}

	reg.versionNum.Store(0)

	return reg
}

// --- EntryReader Interface Implementation ---

func (r *Reg) GetAllEntries() ([]registry.Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state, nil
}

func (r *Reg) GetEntry(path registry.ID) (registry.Entry, error) {
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

func (r *Reg) Apply(ctx context.Context, changes registry.ChangeSet) (registry.Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log.Info("apply started", zap.Int("change_count", len(changes)))

	newVersion := version.FromParent(r.currentVersion, r.nextVersionID(r.currentVersion))

	r.log.Debug("calling runner.Transition")
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

func (r *Reg) ApplyVersion(ctx context.Context, v registry.Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.currentVersion.ID() == v.ID() {
		return nil
	}

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

	if err := r.history.SetHead(v); err != nil {
		return fmt.Errorf("history set head to %d: %w", v.ID(), err)
	}

	r.state = newState
	r.currentVersion = v

	return nil
}

// rollback state desync between actual state in system and state in history
func (r *Reg) rollback(ctx context.Context, from, to registry.State) error {
	r.log.Debug("attempting to rollback")

	partial, err := r.transitionState(ctx, from, to)
	if err == nil {
		return nil // success
	}

	r.state = partial // we remain in a desynced state

	return err
}

func (r *Reg) transitionState(ctx context.Context, from, to registry.State) (registry.State, error) {
	r.log.Debug("transitioning state")

	cs, terr := r.builder.BuildDelta(from, to)
	if terr != nil {
		return nil, fmt.Errorf("failed to compute transition: %w", terr)
	}

	if len(cs) == 0 {
		return from, nil
	}

	return r.runner.Transition(ctx, from, cs)
}

func (r *Reg) Current() (registry.Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.currentVersion == nil {
		return nil, fmt.Errorf("no current version")
	}

	return r.currentVersion, nil
}

func (r *Reg) History() registry.History {
	return r.history
}

// RegisterDependencyPattern adds a pattern for dependency extraction.
// Implements registry.Registry interface.
func (r *Reg) RegisterDependencyPattern(pattern registry.DependencyPattern) error {
	if r.resolver == nil {
		return fmt.Errorf("dependency resolver not initialized")
	}
	return r.resolver.RegisterPattern(pattern)
}

// --- Helper Functions ---

func (r *Reg) nextVersionID(head registry.Version) uint {
	if head == nil {
		return 0
	}
	return uint(r.versionNum.Add(1))
}
