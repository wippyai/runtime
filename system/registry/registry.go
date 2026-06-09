// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	"github.com/wippyai/runtime/system/registry/topology"
)

// indexedSortBuilder is the optional capability that lets Reg.Apply call
// SortChangeSetWithIndex instead of the legacy SortChangeSet. The in-tree
// *topology.StateBuilder satisfies it; out-of-tree builders fall back to the
// O(N x P x T) legacy path automatically.
type indexedSortBuilder interface {
	SortChangeSetWithIndex(fromState registry.State, cs registry.ChangeSet, depIdx *topology.DepIndex) (registry.ChangeSet, error)
}

type Reg struct {
	history          registry.History
	runner           registry.Runner
	builder          registry.StateBuilder
	resolver         registry.DependencyResolver
	directivesByKind map[registry.Kind][]registry.Directive
	currentVersion   registry.Version
	stateIndex       map[registry.ID]int
	depIndex         *topology.DepIndex
	log              *zap.Logger
	state            registry.State
	versionNum       atomic.Uint64
	applyMu          sync.Mutex
	mu               sync.RWMutex
}

// NewRegistry creates a new registry instance.
func NewRegistry(
	history registry.History,
	runner registry.Runner,
	builder registry.StateBuilder,
	resolver registry.DependencyResolver,
	log *zap.Logger,
	opts ...Option,
) *Reg {
	if log == nil {
		log = zap.NewNop()
	}
	reg := &Reg{
		history:        history,
		runner:         runner,
		builder:        builder,
		resolver:       resolver,
		state:          registry.State{},
		stateIndex:     make(map[registry.ID]int),
		log:            log,
		currentVersion: version.FromParent(nil, 0), // initial version
	}

	reg.versionNum.Store(0)

	for _, opt := range opts {
		if opt != nil {
			opt(reg)
		}
	}

	return reg
}

// rebuildIndex rebuilds the stateIndex from the current state.
// Must be called with write lock held.
func (r *Reg) rebuildIndex() {
	r.stateIndex = make(map[registry.ID]int, len(r.state))
	for i, entry := range r.state {
		r.stateIndex[entry.ID] = i
	}
}

// rebuildDepIndex regenerates the inverse-dependency index from the current
// state. Must be called either with applyMu held (single-writer, no concurrent
// readers) or before the registry is exposed to callers. r.mu alone is not
// sufficient because sortWithIndex reads r.depIndex outside r.mu. Only useful
// when the builder is the in-tree topology builder; for other builders the
// index is left nil and Reg falls back to the legacy O(N x P x T) sort.
func (r *Reg) rebuildDepIndex() {
	if _, ok := r.builder.(indexedSortBuilder); !ok {
		r.depIndex = nil
		return
	}
	r.depIndex = topology.BuildDepIndex(r.state, r.resolver)
}

// sortWithIndex dispatches to SortChangeSetWithIndex when the builder
// supports it and an index is available; otherwise it falls back to the
// legacy SortChangeSet path. Lazily builds the index on first use so that
// callers who skipped LoadState (e.g. tests / no-history hosts) still pay
// the build cost once instead of running the legacy O(N) sort forever.
// Caller must hold applyMu.
func (r *Reg) sortWithIndex(fromState registry.State, cs registry.ChangeSet) (registry.ChangeSet, error) {
	if r.depIndex == nil {
		r.rebuildDepIndex()
	}
	if r.depIndex != nil {
		if swi, ok := r.builder.(indexedSortBuilder); ok {
			return swi.SortChangeSetWithIndex(fromState, cs, r.depIndex)
		}
	}
	return r.builder.SortChangeSet(fromState, cs)
}

// --- EntryReader Interface Implementation ---

func (r *Reg) GetAllEntries() ([]registry.Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]registry.Entry, len(r.state))
	copy(result, r.state)
	return result, nil
}

func (r *Reg) GetEntry(path registry.ID) (registry.Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if idx, ok := r.stateIndex[path]; ok {
		return r.state[idx], nil
	}

	return registry.Entry{}, NewEntryNotFoundError(path)
}

// --- StateWriter Interface Implementation ---

func (r *Reg) Apply(ctx context.Context, changes registry.ChangeSet) (registry.Version, error) {
	r.applyMu.Lock()
	defer r.applyMu.Unlock()

	r.log.Info("apply started", zap.Int("change_count", len(changes)))

	var (
		allOps      registry.ChangeSet
		historyOps  registry.ChangeSet
		preparedEff []registry.Effect
		planner     *regexp.Planner
		snapshot    registry.State
		baseVersion registry.Version
	)

	r.mu.RLock()
	snapshot = make(registry.State, len(r.state))
	copy(snapshot, r.state)
	baseVersion = r.currentVersion
	r.mu.RUnlock()

	if len(r.directivesByKind) > 0 {
		planner = regexp.NewPlanner(r.directivesByKind, r.resolver, r.log.Named("expansion"))

		plan, err := planner.Expand(ctx, changes, snapshot)
		if err != nil {
			return nil, NewExpandChangesError(err)
		}

		plan.Ops, err = planner.SortOps(snapshot, plan.Ops)
		if err != nil {
			return nil, NewSortChangesError(err)
		}

		allOps, historyOps = plan.SplitScopes()

		preparedEff, err = planner.PrepareEffects(ctx, plan.Effects)
		if err != nil {
			planner.RollbackEffects(ctx, preparedEff)
			return nil, NewPrepareEffectsError(err)
		}
	} else {
		sorted, err := r.sortWithIndex(snapshot, changes)
		if err != nil {
			return nil, NewSortChangesError(err)
		}
		allOps = sorted
		historyOps = sorted
	}

	// Topologically sort the changeset before dispatching to the runner so
	// deletes hit the dep graph in reverse-dependency order (dependants
	// first). Planner.SortOps only runs when expansion produced ops; the
	// no-expansion path would otherwise reach the runner unsorted and fail
	// against any dependency-aware runner (memory_graph.RemoveNode).
	if sorted, sortErr := r.sortWithIndex(snapshot, allOps); sortErr == nil {
		allOps = sorted
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if baseVersion != nil && r.currentVersion != nil && r.currentVersion.ID() != baseVersion.ID() {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return nil, NewConcurrentApplyError(baseVersion.ID(), r.currentVersion.ID())
	}

	var newVersion registry.Version
	if len(historyOps) > 0 {
		newVersion = version.FromParent(r.currentVersion, r.nextVersionID(r.currentVersion))
	}

	r.log.Debug("calling runner.Transition")
	newState, err := r.runner.Transition(ctx, r.state, allOps)
	if err != nil {
		r.log.Error("failed to apply changes", zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				if planner != nil {
					planner.RollbackEffects(ctx, preparedEff)
				}
				return nil, NewApplyChangesError(err, rerr)
			}
		}
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return nil, NewApplyChangesError(err, nil)
	}

	if planner != nil {
		if err := planner.CommitEffects(ctx, preparedEff); err != nil {
			r.log.Error("failed to commit effects", zap.Error(err))
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				planner.RollbackEffects(ctx, preparedEff)
				return nil, NewCommitEffectsError(err, rerr)
			}
			planner.RollbackEffects(ctx, preparedEff)
			return nil, NewCommitEffectsError(err, nil)
		}
	}

	if len(historyOps) > 0 {
		r.log.Debug("saving new version", zap.Any("new_version", newVersion))

		enrichedChanges := r.enrichChangeset(historyOps)
		if err := r.history.Save(newVersion, enrichedChanges, true); err != nil {
			r.log.Error("failed to save new version", zap.Error(err))
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				if planner != nil {
					planner.RollbackEffects(ctx, preparedEff)
				}
				return nil, NewSaveVersionError(err, rerr)
			}
			if planner != nil {
				planner.RollbackEffects(ctx, preparedEff)
			}
			return nil, NewSaveVersionError(err, nil)
		}

		r.state = newState
		r.rebuildIndex()
		r.patchDepIndex(allOps)
		r.currentVersion = newVersion
		return newVersion, nil
	}

	r.state = newState
	r.rebuildIndex()
	r.patchDepIndex(allOps)
	return r.currentVersion, nil
}

// patchDepIndex folds committed ops back into the inverse-dependency index so
// the next Apply can keep using O(1) source-side lookups. No-op when the
// builder doesn't expose the indexed sort.
func (r *Reg) patchDepIndex(ops registry.ChangeSet) {
	if r.depIndex == nil {
		return
	}
	r.depIndex.Patch(ops, r.resolver)
}

func (r *Reg) ApplyVersion(ctx context.Context, v registry.Version) error {
	r.applyMu.Lock()
	defer r.applyMu.Unlock()

	var (
		snapshot    registry.State
		baseVersion registry.Version
	)

	r.mu.RLock()
	snapshot = make(registry.State, len(r.state))
	copy(snapshot, r.state)
	baseVersion = r.currentVersion
	r.mu.RUnlock()

	if baseVersion != nil && baseVersion.ID() == v.ID() {
		return nil
	}

	var currentVersionID uint
	if baseVersion != nil {
		currentVersionID = baseVersion.ID()
	}

	targetVersion, path, err := r.computeVersionPath(baseVersion, v, currentVersionID)
	if err != nil {
		return err
	}

	r.log.Debug("computed version path",
		zap.Uint("from", currentVersionID),
		zap.Uint("to", targetVersion.ID()),
		zap.Int("steps", len(path)))

	isForward := currentVersionID < targetVersion.ID()
	var changeset registry.ChangeSet

	if isForward {
		changeset, err = r.collectForwardChangesets(path)
	} else {
		changeset, err = r.collectBackwardChangesets(path, targetVersion)
	}
	if err != nil {
		return err
	}

	var (
		allOps      registry.ChangeSet
		preparedEff []registry.Effect
		planner     *regexp.Planner
	)

	if len(r.directivesByKind) > 0 {
		planner = regexp.NewPlanner(r.directivesByKind, r.resolver, r.log.Named("expansion"))

		plan, err := planner.Expand(ctx, changeset, snapshot)
		if err != nil {
			return NewExpandChangesError(err)
		}

		plan.Ops, err = planner.SortOps(snapshot, plan.Ops)
		if err != nil {
			return NewSortChangesError(err)
		}

		allOps, _ = plan.SplitScopes()
		preparedEff, err = planner.PrepareEffects(ctx, plan.Effects)
		if err != nil {
			planner.RollbackEffects(ctx, preparedEff)
			return NewPrepareEffectsError(err)
		}
	} else {
		sorted, err := r.builder.SortChangeSet(snapshot, changeset)
		if err != nil {
			return NewSortChangesError(err)
		}
		allOps = sorted
	}

	// Topologically sort before dispatching to the runner. Same invariant as
	// Apply: reverse-dep order for deletes, forward-dep for creates/updates.
	// Rollback paths hit this branch since backward changesets rarely go
	// through expansion.
	if sorted, sortErr := r.builder.SortChangeSet(snapshot, allOps); sortErr == nil {
		allOps = sorted
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if baseVersion != nil && r.currentVersion != nil && r.currentVersion.ID() != baseVersion.ID() {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return NewConcurrentApplyError(baseVersion.ID(), r.currentVersion.ID())
	}

	newState, err := r.runner.Transition(ctx, r.state, allOps)
	if err != nil {
		r.log.Error("failed to apply squashed changeset", zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				if planner != nil {
					planner.RollbackEffects(ctx, preparedEff)
				}
				return NewApplyVersionChangesError(err, rerr)
			}
		}
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return NewApplyVersionChangesError(err, nil)
	}

	if planner != nil {
		if err := planner.CommitEffects(ctx, preparedEff); err != nil {
			r.log.Error("failed to commit effects", zap.Error(err))
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				planner.RollbackEffects(ctx, preparedEff)
				return NewCommitEffectsError(err, rerr)
			}
			planner.RollbackEffects(ctx, preparedEff)
			return NewCommitEffectsError(err, nil)
		}
	}

	if err := r.history.SetHead(targetVersion); err != nil {
		return NewSetHeadError(targetVersion.ID(), err)
	}

	r.state = newState
	r.rebuildIndex()
	r.rebuildDepIndex()
	r.currentVersion = targetVersion

	r.log.Debug("version applied successfully", zap.Uint("version", targetVersion.ID()))
	return nil
}

func (r *Reg) computeVersionPath(current registry.Version, v registry.Version, currentVersionID uint) (registry.Version, []registry.Version, error) {
	versions, err := r.history.Versions()
	if err != nil {
		return nil, nil, NewGetVersionsError(err)
	}

	var targetVersion registry.Version
	for _, ver := range versions {
		if ver.ID() == v.ID() {
			targetVersion = ver
			break
		}
	}
	if targetVersion == nil {
		return nil, nil, NewVersionNotFoundError(v.ID())
	}

	vm := version.NewVersionMap()
	for _, ver := range versions {
		if err := vm.Add(ver); err != nil {
			r.log.Warn("failed to add version to map", zap.Uint("version", ver.ID()), zap.Error(err))
		}
	}

	path, err := vm.Path(current, targetVersion)
	if err != nil {
		return nil, nil, NewComputePathError(currentVersionID, targetVersion.ID(), err)
	}
	if len(path) == 0 {
		return nil, nil, NewComputePathError(currentVersionID, targetVersion.ID(), ErrEmptyVersionPath)
	}

	return targetVersion, path, nil
}

func (r *Reg) collectForwardChangesets(path []registry.Version) (registry.ChangeSet, error) {
	changesets := make([]registry.ChangeSet, 0, len(path))
	for _, ver := range path {
		cs, err := r.history.Get(ver)
		if err != nil {
			return nil, NewGetChangesetError(ver.ID(), err)
		}
		changesets = append(changesets, cs)
	}

	return r.builder.SquashChangesets(changesets), nil
}

func (r *Reg) collectBackwardChangesets(path []registry.Version, targetVersion registry.Version) (registry.ChangeSet, error) {
	commonAncestorIdx := -1
	for i, ver := range path {
		if ver.ID() <= r.currentVersion.ID() && ver.ID() <= targetVersion.ID() {
			commonAncestorIdx = i
			break
		}
	}
	if commonAncestorIdx < 0 {
		return nil, NewComputePathError(r.currentVersion.ID(), targetVersion.ID(), ErrNoCommonAncestor)
	}

	var reversedChangesets []registry.ChangeSet
	current := r.currentVersion
	for current != nil && current.ID() > path[commonAncestorIdx].ID() {
		cs, err := r.history.Get(current)
		if err != nil {
			return nil, NewGetChangesetError(current.ID(), err)
		}
		rev, err := r.builder.ReverseChangeset(cs)
		if err != nil {
			return nil, NewReverseChangesetError(err)
		}
		reversedChangesets = append(reversedChangesets, rev)
		current = current.Previous()
	}

	for i := commonAncestorIdx; i < len(path); i++ {
		if path[i].ID() > path[commonAncestorIdx].ID() {
			cs, err := r.history.Get(path[i])
			if err != nil {
				return nil, NewGetChangesetError(path[i].ID(), err)
			}
			reversedChangesets = append(reversedChangesets, cs)
		}
	}

	return r.builder.SquashChangesets(reversedChangesets), nil
}

// LoadState initializes registry state from baseline and history without creating new version records.
// This is used during boot to restore state from lockfile + history replay.
// For v0 (empty history): applies baseline directly
// For v1+: replays changesets v1..targetVersion on top of baseline, then applies final state once
func (r *Reg) LoadState(ctx context.Context, baseline registry.State, targetVersion registry.Version) error {
	r.applyMu.Lock()
	defer r.applyMu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Build state map once from baseline
	stateMap := make(map[registry.ID]registry.Entry, len(baseline))
	for _, entry := range baseline {
		stateMap[entry.ID] = entry
	}

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

		// Apply all operations directly to the map
		for _, ver := range versions {
			cs, err := r.history.Get(ver)
			if err != nil {
				return NewGetChangesetError(ver.ID(), err)
			}

			for _, op := range cs {
				switch op.Kind {
				case registry.EntryCreate, registry.EntryUpdate:
					stateMap[op.Entry.ID] = op.Entry
				case registry.EntryDelete:
					delete(stateMap, op.Entry.ID)
				}
			}
		}
	}

	// Convert map to slice once at the end
	finalState := make(registry.State, 0, len(stateMap))
	for _, entry := range stateMap {
		finalState = append(finalState, entry)
	}

	newState, err := r.transitionState(ctx, r.state, finalState)
	if err != nil {
		r.log.Error("failed to load state", zap.String("version", targetVersion.String()), zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				return NewLoadStateError(err, rerr)
			}
		}
		return NewLoadStateError(err, nil)
	}

	r.state = newState
	r.rebuildIndex()
	r.rebuildDepIndex()
	r.currentVersion = targetVersion
	r.versionNum.Store(uint64(targetVersion.ID()))

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
	r.rebuildIndex()
	r.rebuildDepIndex()

	return err
}

func (r *Reg) transitionState(ctx context.Context, from, to registry.State) (registry.State, error) {
	r.log.Debug("transitioning state")

	cs, terr := r.builder.BuildDelta(from, to)
	if terr != nil {
		return nil, NewComputeTransitionError(terr)
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
		return nil, ErrNoCurrentVersion
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
		return ErrDependencyResolverNotInit
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
		case registry.EntryUpdate, registry.EntryDelete:
			if originalEntry, exists := stateMap[op.Entry.ID]; exists {
				enriched[i].OriginalEntry = &originalEntry
			} else {
				r.log.Warn("entry not found in state for enrichment",
					zap.String("operation", op.Kind),
					zap.String("entry_id", op.Entry.ID.String()))
			}
		}
	}

	return enriched
}

// DependencyResolver returns the registry's dependency resolver for external use
func (r *Reg) DependencyResolver() registry.DependencyResolver {
	return r.resolver
}
