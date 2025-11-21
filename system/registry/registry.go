package registry

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/topology"
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

	enrichedChanges := r.enrichChangeset(changes)
	err = r.history.Save(newVersion, enrichedChanges, true)
	if err != nil {
		r.log.Error("failed to save new version", zap.Error(err))
		if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
			return nil, fmt.Errorf("failed to save new version: %w, failed to rollback: %w", err, rerr)
		}

		return nil, fmt.Errorf("failed to save new version: %w, recovered", err)
	}

	r.state = newState
	r.currentVersion = newVersion

	return newVersion, nil
}

func (r *Reg) ApplyVersion(ctx context.Context, v registry.Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.currentVersion.ID() == v.ID() {
		return nil
	}

	r.log.Debug("applying version", zap.Uint("target_version", v.ID()), zap.Uint("current_version", r.currentVersion.ID()))

	// Lookup the version from history by ID to ensure we use the correct instance
	versions, err := r.history.Versions()
	if err != nil {
		return fmt.Errorf("failed to get versions from history: %w", err)
	}

	var targetVersion registry.Version
	for _, ver := range versions {
		if ver.ID() == v.ID() {
			targetVersion = ver
			break
		}
	}

	if targetVersion == nil {
		return fmt.Errorf("version %d not found in history", v.ID())
	}

	// Build version map to compute path
	vm := version.NewVersionMap()
	for _, ver := range versions {
		if err := vm.Add(ver); err != nil {
			r.log.Warn("failed to add version to map", zap.Uint("version", ver.ID()), zap.Error(err))
		}
	}

	// Compute path from current version to target version
	path, err := vm.Path(r.currentVersion, targetVersion)
	if err != nil {
		return fmt.Errorf("failed to compute path from v%d to v%d: %w",
			r.currentVersion.ID(), targetVersion.ID(), err)
	}

	r.log.Debug("computed version path",
		zap.Uint("from", r.currentVersion.ID()),
		zap.Uint("to", targetVersion.ID()),
		zap.Int("steps", len(path)))

	// Collect changesets and apply/reverse based on direction
	isForward := r.currentVersion.ID() < targetVersion.ID()

	var changeset registry.ChangeSet

	if isForward {
		// Forward: collect and apply changesets in path
		var changesets []registry.ChangeSet
		for _, ver := range path {
			cs, err := r.history.Get(ver)
			if err != nil {
				return fmt.Errorf("failed to get changeset for version v%d: %w", ver.ID(), err)
			}
			changesets = append(changesets, cs)
		}

		// Squash if possible
		if sb, ok := r.builder.(*topology.StateBuilder); ok {
			changeset = sb.SquashChangesets(changesets)
		} else {
			for _, cs := range changesets {
				changeset = append(changeset, cs...)
			}
		}
	} else {
		// Backward: need to handle both direct parent-child and cross-branch transitions
		// Use path to handle cross-branch cases (e.g., v4->v1->v3)
		if sb, ok := r.builder.(*topology.StateBuilder); ok {
			// Split path into two parts: reverse and forward
			var commonAncestorIdx int
			for i, ver := range path {
				if ver.ID() <= r.currentVersion.ID() && ver.ID() <= targetVersion.ID() {
					commonAncestorIdx = i
					break
				}
			}

			// Reverse: from current back to common ancestor
			var reversedChangesets []registry.ChangeSet
			current := r.currentVersion
			for current != nil && current.ID() > path[commonAncestorIdx].ID() {
				cs, err := r.history.Get(current)
				if err != nil {
					return fmt.Errorf("failed to get changeset for version v%d: %w", current.ID(), err)
				}
				rev, err := sb.ReverseChangeset(cs)
				if err != nil {
					return fmt.Errorf("failed to reverse changeset: %w", err)
				}
				reversedChangesets = append(reversedChangesets, rev)
				current = current.Previous()
			}

			// Forward: from common ancestor to target
			var forwardChangesets []registry.ChangeSet
			for i := commonAncestorIdx; i < len(path); i++ {
				if path[i].ID() > path[commonAncestorIdx].ID() {
					cs, err := r.history.Get(path[i])
					if err != nil {
						return fmt.Errorf("failed to get changeset for version v%d: %w", path[i].ID(), err)
					}
					forwardChangesets = append(forwardChangesets, cs)
				}
			}

			// Combine reversed and forward changesets
			reversedChangesets = append(reversedChangesets, forwardChangesets...)
			changeset = sb.SquashChangesets(reversedChangesets)
		} else {
			return fmt.Errorf("builder does not support changeset reversal")
		}
	}

	// Apply the changeset
	newState, err := r.runner.Transition(ctx, r.state, changeset)
	if err != nil {
		r.log.Error("failed to apply squashed changeset", zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				return fmt.Errorf("failed to apply version changes: %w, failed to rollback: %w", err, rerr)
			}
		}
		return fmt.Errorf("failed to apply version changes: %w", err)
	}

	if err := r.history.SetHead(targetVersion); err != nil {
		return fmt.Errorf("history set head to %d: %w", targetVersion.ID(), err)
	}

	r.state = newState
	r.currentVersion = targetVersion

	r.log.Debug("version applied successfully", zap.Uint("version", targetVersion.ID()))

	return nil
}

// LoadState initializes registry state from baseline and history without creating new version records.
// This is used during boot to restore state from lockfile + history replay.
// For v0 (empty history): applies baseline directly
// For v1+: replays changesets v1..targetVersion on top of baseline, then applies final state once
func (r *Reg) LoadState(ctx context.Context, baseline registry.State, targetVersion registry.Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	finalState := baseline

	if targetVersion.ID() > 0 {
		current := targetVersion
		var versions []registry.Version

		for current != nil && current.ID() > 0 {
			versions = append([]registry.Version{current}, versions...)
			current = current.Previous()
		}

		r.log.Debug("replaying changesets on baseline",
			zap.Uint("target_version", targetVersion.ID()),
			zap.Int("changeset_count", len(versions)))

		for _, ver := range versions {
			cs, err := r.history.Get(ver)
			if err != nil {
				return fmt.Errorf("failed to get changeset for version v%d: %w", ver.ID(), err)
			}

			for _, op := range cs {
				finalState = r.applyOperationToState(finalState, op)
			}
		}
	}

	newState, err := r.transitionState(ctx, r.state, finalState)
	if err != nil {
		r.log.Error("failed to load state", zap.String("version", targetVersion.String()), zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				return fmt.Errorf("failed to load state: %w, failed to rollback: %w", err, rerr)
			}
		}
		return fmt.Errorf("failed to load state: %w", err)
	}

	r.state = newState
	r.currentVersion = targetVersion
	r.versionNum.Store(uint64(targetVersion.ID()))

	return nil
}

// applyOperationToState applies a single operation to a state, used during restoration
func (r *Reg) applyOperationToState(state registry.State, op registry.Operation) registry.State {
	stateMap := make(map[registry.ID]registry.Entry, len(state))
	for _, entry := range state {
		stateMap[entry.ID] = entry
	}

	switch op.Kind {
	case registry.Create:
		stateMap[op.Entry.ID] = op.Entry
	case registry.Update:
		stateMap[op.Entry.ID] = op.Entry
	case registry.Delete:
		delete(stateMap, op.Entry.ID)
	}

	result := make(registry.State, 0, len(stateMap))
	for _, entry := range stateMap {
		result = append(result, entry)
	}

	return result
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

// enrichChangeset creates a copy of the changeset with OriginalEntry populated for reversal
func (r *Reg) enrichChangeset(changes registry.ChangeSet) registry.ChangeSet {
	stateMap := make(map[registry.ID]registry.Entry, len(r.state))
	for _, entry := range r.state {
		stateMap[entry.ID] = entry
	}

	enriched := make(registry.ChangeSet, len(changes))
	for i, op := range changes {
		enriched[i] = op
		switch op.Kind {
		case registry.Update, registry.Delete:
			if originalEntry, exists := stateMap[op.Entry.ID]; exists {
				enriched[i].OriginalEntry = &originalEntry
			}
		}
	}

	return enriched
}

// DependencyResolver returns the registry's dependency resolver for external use
func (r *Reg) DependencyResolver() registry.DependencyResolver {
	return r.resolver
}
