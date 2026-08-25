// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
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
	currentVersion    registry.Version
	runner            registry.Runner
	builder           registry.StateBuilder
	resolver          registry.DependencyResolver
	history           registry.History
	overlays          map[string]registry.State
	baselineProv      registry.ProvMap
	stateIndex        map[registry.ID]int
	directivesByKind  map[registry.Kind][]registry.Directive
	log               *zap.Logger
	depIndex          *topology.DepIndex
	overlayOwners     map[registry.ID]string
	overlayGeneration map[string]uint64
	prov              atomic.Pointer[registry.ProvMap]
	currentResolution *registry.DependencyResolution
	state             registry.State
	baseline          registry.State
	overlayEpoch      uint64
	overlayFloor      uint64
	versionNum        atomic.Uint64
	mu                sync.RWMutex
	applyMu           sync.Mutex
	stateLoaded       bool
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
		history:           history,
		runner:            runner,
		builder:           builder,
		resolver:          resolver,
		state:             registry.State{},
		stateIndex:        make(map[registry.ID]int),
		overlays:          make(map[string]registry.State),
		overlayOwners:     make(map[registry.ID]string),
		overlayGeneration: make(map[string]uint64),
		log:               log,
		currentVersion:    version.FromParent(nil, 0), // initial version
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
	path = canonicalEntryID(path)
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
	changes = append(registry.ChangeSet(nil), changes...)
	canonicalizeChangeSetIDs(changes)

	r.log.Info("apply started", zap.Int("change_count", len(changes)))

	var (
		allOps            registry.ChangeSet
		historyOps        registry.ChangeSet
		preparedEff       []registry.Effect
		planner           *regexp.Planner
		snapshot          registry.State
		baseVersion       registry.Version
		resolution        *registry.DependencyResolution
		resolutionChanged bool
		planProvenance    registry.ProvMap
	)

	r.mu.RLock()
	snapshot = make(registry.State, len(r.state))
	copy(snapshot, r.state)
	baseVersion = r.currentVersion
	resolution = r.currentResolution
	liveProv := r.Provenance()
	r.mu.RUnlock()

	if len(r.directivesByKind) > 0 {
		planner = regexp.NewPlanner(r.directivesByKind, r.resolver, r.log.Named("expansion"))

		plan, err := planner.Expand(ctx, changes, registry.ProvenancedState{Entries: snapshot, Prov: liveProv})
		if err != nil {
			return nil, NewExpandChangesError(err)
		}

		plan.Ops, err = planner.SortOps(snapshot, plan.Ops)
		if err != nil {
			planner.RollbackEffects(ctx, plan.Effects)
			return nil, NewSortChangesError(err)
		}

		allOps, historyOps = plan.SplitScopes()
		planProvenance = plan.Provenance
		if plan.Resolution != nil {
			candidate := plan.Resolution.Canonical()
			if resolution == nil || candidate.Digest != resolution.Digest {
				resolution = candidate
				resolutionChanged = true
			}
		}

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
	} else {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return nil, NewSortChangesError(sortErr)
	}
	if err := r.validateDurableTransitionAgainstOverlays(allOps); err != nil {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return nil, err
	}

	newProv := applyOpsToProvenance(liveProv, allOps)
	for id, record := range planProvenance {
		newProv[canonicalEntryID(id)] = record
	}
	annotateChangeSet(allOps, liveProv, newProv)

	rollbackOutcome := &registry.RollbackOutcome{}
	ctx = registry.WithRollbackOutcome(ctx, rollbackOutcome)

	r.mu.Lock()
	defer r.mu.Unlock()

	if baseVersion != nil && r.currentVersion != nil && r.currentVersion.ID() != baseVersion.ID() {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return nil, NewConcurrentApplyError(baseVersion.ID(), r.currentVersion.ID())
	}

	provenanceAdvanced := len(planProvenance) > 0
	var newVersion registry.Version
	if len(historyOps) > 0 || resolutionChanged || provenanceAdvanced {
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

	if len(historyOps) > 0 || resolutionChanged || provenanceAdvanced {
		r.log.Debug("saving new version", zap.Any("new_version", newVersion))

		enrichedChanges := r.enrichChangeset(historyOps)
		var saveErr error
		if resolutionChanged {
			resolutionHistory, ok := r.history.(registry.ResolutionHistory)
			if !ok {
				saveErr = ErrDurableResolutionUnsupported
			} else {
				saveErr = resolutionHistory.SaveWithDependencyResolution(newVersion, enrichedChanges, resolution, true)
			}
		} else {
			saveErr = r.history.Save(newVersion, enrichedChanges, true)
		}
		if saveErr != nil {
			r.log.Error("failed to save new version", zap.Error(saveErr))
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				if planner != nil {
					planner.RollbackEffects(ctx, preparedEff)
				}
				return nil, NewSaveVersionError(saveErr, rerr)
			}
			if planner != nil {
				planner.RollbackEffects(ctx, preparedEff)
			}
			return nil, NewSaveVersionError(saveErr, nil)
		}
		if planner != nil {
			if finalizeErr := planner.FinalizeEffects(ctx, preparedEff); finalizeErr != nil {
				r.log.Warn("failed to finalize effects after saving version", zap.Error(finalizeErr))
			}
		}

		r.state = newState
		r.rebuildIndex()
		r.patchDepIndex(allOps)
		r.publishProvenance(newProv)
		r.currentVersion = newVersion
		r.currentResolution = resolution
		return newVersion, nil
	}

	r.state = newState
	r.rebuildIndex()
	r.patchDepIndex(allOps)
	r.publishProvenance(newProv)
	if planner != nil {
		if finalizeErr := planner.FinalizeEffects(ctx, preparedEff); finalizeErr != nil {
			r.log.Warn("failed to finalize effects after baseline transition", zap.Error(finalizeErr))
		}
	}
	return r.currentVersion, nil
}

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
		if cas, ok := r.history.(registry.HeadCASHistory); ok {
			if err := cas.CompareAndSetHead(baseVersion, baseVersion); err != nil {
				return NewSetHeadError(baseVersion.ID(), err)
			}
		}
		return nil
	}

	var currentVersionID uint
	if baseVersion != nil {
		currentVersionID = baseVersion.ID()
	}

	targetVersion, err := r.findStoredVersion(v)
	if err != nil {
		return err
	}
	targetState, targetProv, err := r.stateAtVersion(ctx, targetVersion)
	if err != nil {
		return err
	}

	r.log.Debug("resolving version transition",
		zap.Uint("from", currentVersionID),
		zap.Uint("to", targetVersion.ID()))

	var (
		allOps           registry.ChangeSet
		preparedEff      []registry.Effect
		planner          *regexp.Planner
		targetResolution *registry.DependencyResolution
	)
	if resolutionHistory, ok := r.history.(registry.ResolutionHistory); ok {
		stored, resolutionErr := resolutionHistory.GetDependencyResolution(targetVersion)
		if resolutionErr == nil {
			targetResolution = stored
		} else if !errors.Is(resolutionErr, registry.ErrDependencyResolutionNotFound) {
			return NewGetChangesetError(targetVersion.ID(), resolutionErr)
		}
	}

	if targetResolution != nil {
		if len(r.directivesByKind) == 0 {
			return NewExpandChangesError(errors.New("stored dependency resolution has no configured reconciler"))
		}
		planner = regexp.NewPlanner(r.directivesByKind, r.resolver, r.log.Named("expansion"))
		stateMap := topology.NewStateMap(targetState)
		reconciled := false
		for _, directive := range r.directivesByKind[registry.NamespaceDependency] {
			result, ok, reconcileErr := reconcileStoredResolution(ctx, directive,
				registry.ProvenancedState{Entries: snapshot, Prov: r.Provenance()},
				registry.ProvenancedState{Entries: topology.StateMapToSlice(stateMap), Prov: targetProv},
				targetResolution)
			if !ok {
				continue
			}
			if reconcileErr != nil {
				planner.RollbackEffects(ctx, preparedEff)
				return NewExpandChangesError(reconcileErr)
			}
			reconciled = reconciled || result.Applied
			if result.Resolution != nil {
				targetResolution = result.Resolution.Canonical()
			}
			prepared, prepareErr := planner.PrepareEffects(ctx, result.Effects)
			if prepareErr != nil {
				planner.RollbackEffects(ctx, prepared)
				planner.RollbackEffects(ctx, preparedEff)
				return NewPrepareEffectsError(prepareErr)
			}
			preparedEff = append(preparedEff, prepared...)
			additional := make(registry.ChangeSet, 0, len(result.Additional))
			for _, operation := range result.Additional {
				additional = append(additional, operation.Operation)
			}
			applyStateOperations(stateMap, additional)
			targetProv = applyOpsToProvenance(targetProv, additional)
			for id, record := range result.Provenance {
				targetProv[canonicalEntryID(id)] = record
			}
		}
		if !reconciled {
			planner.RollbackEffects(ctx, preparedEff)
			return NewExpandChangesError(errors.New("stored dependency resolution has no configured reconciler"))
		}
		var buildErr error
		if composeErr := r.composeOverlays(stateMap); composeErr != nil {
			planner.RollbackEffects(ctx, preparedEff)
			return NewComputeTransitionError(composeErr)
		}
		allOps, buildErr = r.builder.BuildDelta(snapshot, topology.StateMapToSlice(stateMap))
		if buildErr != nil {
			planner.RollbackEffects(ctx, preparedEff)
			return NewComputeTransitionError(buildErr)
		}
	} else if len(r.directivesByKind) > 0 {
		planner = regexp.NewPlanner(r.directivesByKind, r.resolver, r.log.Named("expansion"))
		stateMap := topology.NewStateMap(targetState)
		declarations := topology.StateMapToSlice(stateMap)
		for _, entry := range declarations {
			if targetResolution != nil && entry.Kind == registry.NamespaceDependency {
				continue // One dependency expansion resolves the complete root graph.
			}
			if len(r.directivesByKind[entry.Kind]) == 0 {
				continue
			}
			intermediate := topology.StateMapToSlice(stateMap)
			plan, expandErr := planner.Expand(ctx, registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: entry}}, registry.ProvenancedState{Entries: intermediate, Prov: targetProv})
			if expandErr != nil {
				planner.RollbackEffects(ctx, preparedEff)
				return NewExpandChangesError(expandErr)
			}
			if !plan.Expanded {
				continue
			}
			plan.Ops, expandErr = planner.SortOps(intermediate, plan.Ops)
			if expandErr != nil {
				planner.RollbackEffects(ctx, plan.Effects)
				planner.RollbackEffects(ctx, preparedEff)
				return NewSortChangesError(expandErr)
			}
			ops, _ := plan.SplitScopes()
			prepared, prepareErr := planner.PrepareEffects(ctx, plan.Effects)
			if prepareErr != nil {
				planner.RollbackEffects(ctx, prepared)
				planner.RollbackEffects(ctx, preparedEff)
				return NewPrepareEffectsError(prepareErr)
			}
			preparedEff = append(preparedEff, prepared...)
			if plan.Resolution != nil {
				targetResolution = plan.Resolution.Canonical()
			}
			applyStateOperations(stateMap, ops)
			targetProv = applyOpsToProvenance(targetProv, ops)
			for id, record := range plan.Provenance {
				targetProv[canonicalEntryID(id)] = record
			}
		}
		if composeErr := r.composeOverlays(stateMap); composeErr != nil {
			planner.RollbackEffects(ctx, preparedEff)
			return NewComputeTransitionError(composeErr)
		}
		allOps, err = r.builder.BuildDelta(snapshot, topology.StateMapToSlice(stateMap))
		if err != nil {
			planner.RollbackEffects(ctx, preparedEff)
			return NewComputeTransitionError(err)
		}
	} else {
		stateMap := topology.NewStateMap(targetState)
		if composeErr := r.composeOverlays(stateMap); composeErr != nil {
			return NewComputeTransitionError(composeErr)
		}
		delta, err := r.builder.BuildDelta(snapshot, topology.StateMapToSlice(stateMap))
		if err != nil {
			return NewComputeTransitionError(err)
		}
		allOps = delta
	}

	resolutionHeadCAS, supportsAtomicResolutionHead := r.history.(registry.ResolutionHeadCASHistory)
	if targetResolution != nil && !supportsAtomicResolutionHead {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return NewSaveVersionError(ErrDurableResolutionUnsupported, nil)
	}

	// Topologically sort before dispatching to the runner. Same invariant as
	// Apply: reverse-dep order for deletes, forward-dep for creates/updates.
	// Rollback paths hit this branch since backward changesets rarely go
	// through expansion.
	if sorted, sortErr := r.builder.SortChangeSet(snapshot, allOps); sortErr == nil {
		allOps = sorted
	} else {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return NewSortChangesError(sortErr)
	}
	if preflightErr := r.validateDurableTransitionAgainstOverlays(allOps); preflightErr != nil {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return preflightErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if baseVersion != nil && r.currentVersion != nil && r.currentVersion.ID() != baseVersion.ID() {
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return NewConcurrentApplyError(baseVersion.ID(), r.currentVersion.ID())
	}

	annotateChangeSet(allOps, r.Provenance(), targetProv)
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

	var headUpdateErr error
	if targetResolution != nil {
		headUpdateErr = resolutionHeadCAS.CompareAndSetHeadWithDependencyResolution(baseVersion, targetVersion, targetResolution)
	} else {
		headUpdateErr = compareAndSetHistoryHead(r.history, baseVersion, targetVersion)
	}
	if headUpdateErr != nil {
		headErr := NewSetHeadError(targetVersion.ID(), headUpdateErr)
		var compensationErr error
		if rollbackErr := r.rollback(ctx, newState, r.state); rollbackErr != nil {
			compensationErr = errors.Join(compensationErr, rollbackErr)
		}
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		if compensationErr != nil {
			return NewApplyVersionChangesError(headErr, compensationErr)
		}
		return headErr
	}
	if planner != nil {
		if finalizeErr := planner.FinalizeEffects(ctx, preparedEff); finalizeErr != nil {
			r.log.Warn("failed to finalize effects after applying version", zap.Error(finalizeErr))
		}
	}

	r.state = newState
	r.rebuildIndex()
	r.rebuildDepIndex()
	r.publishProvenance(provenanceForState(newState, targetProv, allOps))
	r.currentVersion = targetVersion
	r.currentResolution = targetResolution

	r.log.Debug("version applied successfully", zap.Uint("version", targetVersion.ID()))
	return nil
}

// stateAtVersion composes the immutable deployment baseline with the authored
// history at target. Live version selection must use the same authority model
// as cold boot; reversing operations against the expanded resident state can
// otherwise delete baseline entries hidden by an overlay.
func (r *Reg) stateAtVersion(ctx context.Context, target registry.Version) (registry.State, registry.ProvMap, error) {
	stateMap := topology.NewStateMap(r.baseline)
	prov := r.baselineProv.Clone()
	if prov == nil {
		prov = make(registry.ProvMap)
	}
	if target == nil || target.ID() == registry.RootVersion {
		return topology.StateMapToSlice(stateMap), prov, nil
	}
	apply := func(changes registry.ChangeSet) error {
		canonicalizeChangeSetIDs(changes)
		applyStateOperations(stateMap, changes)
		prov = applyOpsToProvenance(prov, changes)
		return nil
	}
	if replayer, ok := r.history.(registry.ChangeSetReplayer); ok {
		if err := replayer.ReplayChanges(ctx, target, apply); err != nil {
			return nil, nil, NewGetChangesetError(target.ID(), err)
		}
		return topology.StateMapToSlice(stateMap), prov, nil
	}
	var lineage []registry.Version
	for current := target; current != nil && current.ID() > registry.RootVersion; current = current.Previous() {
		lineage = append(lineage, current)
	}
	for i := len(lineage) - 1; i >= 0; i-- {
		changes, err := r.history.Get(lineage[i])
		if err != nil {
			return nil, nil, NewGetChangesetError(lineage[i].ID(), err)
		}
		if err := apply(changes); err != nil {
			return nil, nil, err
		}
	}
	return topology.StateMapToSlice(stateMap), prov, nil
}

func compareAndSetHistoryHead(history registry.History, expected, target registry.Version) error {
	if expected == nil {
		return history.SetHead(target)
	}
	cas, ok := history.(registry.HeadCASHistory)
	if !ok {
		return errors.New("history does not support atomic head updates")
	}
	return cas.CompareAndSetHead(expected, target)
}

func (r *Reg) findStoredVersion(v registry.Version) (registry.Version, error) {
	if lookup, ok := r.history.(registry.VersionLookup); ok {
		stored, err := lookup.GetVersion(v.ID())
		if err != nil {
			return nil, NewGetVersionsError(err)
		}
		return stored, nil
	}
	versions, err := r.history.Versions()
	if err != nil {
		return nil, NewGetVersionsError(err)
	}
	for _, ver := range versions {
		if ver.ID() == v.ID() {
			return ver, nil
		}
	}
	return nil, NewVersionNotFoundError(v.ID())
}

// LoadState initializes registry state from baseline and history without
// creating new version records: the baseline applies directly and changesets
// v1..targetVersion replay on top. The baseline carries its provenance;
// replayed and reconciled operations fold theirs in, so the published
// provenance map matches the final state.
func (r *Reg) LoadState(ctx context.Context, baselineState registry.ProvenancedState, targetVersion registry.Version) error {
	baseline := baselineState.Entries
	if err := baselineState.Validate(); err != nil {
		return NewLoadStateError(err, nil)
	}
	if registry.DependencyAccessFromContext(ctx) == registry.DependencyAccessUnspecified {
		ctx = registry.WithDependencyAccess(ctx, registry.DependencyAccessVerifiedOffline)
	}

	r.applyMu.Lock()
	defer r.applyMu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	allocatorVersion := targetVersion.ID()
	if bounds, ok := r.history.(registry.VersionIDBounds); ok {
		maxID, err := bounds.MaxVersionID()
		if err != nil {
			return NewGetVersionsError(err)
		}
		if maxID > allocatorVersion {
			allocatorVersion = maxID
		}
	} else if versionsInHistory, versionsErr := r.history.Versions(); versionsErr != nil {
		// Forward-only histories cannot enumerate versions and have no branches to
		// preserve. Durable histories must enumerate successfully so a rewind does
		// not make the allocator reuse an existing branch ID.
		if targetVersion.ID() > registry.RootVersion {
			return NewGetVersionsError(versionsErr)
		}
	} else {
		for _, storedVersion := range versionsInHistory {
			if storedVersion.ID() > allocatorVersion {
				allocatorVersion = storedVersion.ID()
			}
		}
	}

	// Establish the registry's canonical-ID invariant once at the external
	// state boundary. Internal indexes and overlay ownership maps can then use
	// IDs directly without allocating and interning them on every scan.
	baseline = append(registry.State(nil), baseline...)
	for i := range baseline {
		baseline[i].ID = canonicalEntryID(baseline[i].ID)
	}
	if err := topology.ValidateUniqueEntryIDs("baseline", baseline); err != nil {
		return err
	}

	stateMap := topology.NewStateMap(baseline)
	targetProv := make(registry.ProvMap, len(baseline))
	for id, p := range baselineState.Prov {
		targetProv[canonicalEntryID(id)] = p
	}
	var planner *regexp.Planner
	var preparedEff []registry.Effect
	var resolution *registry.DependencyResolution
	if len(r.directivesByKind) > 0 {
		planner = regexp.NewPlanner(r.directivesByKind, r.resolver, r.log.Named("expansion"))
	}

	// History contains declarative operations. Reduce it to the target state
	// without expanding directives: expansion can consult external systems and
	// must never run once per historical version during boot.
	if targetVersion.ID() > 0 {
		applyChanges := func(cs registry.ChangeSet) error {
			canonicalizeChangeSetIDs(cs)
			applyStateOperations(stateMap, cs)
			targetProv = applyOpsToProvenance(targetProv, cs)
			return nil
		}
		if replayer, ok := r.history.(registry.ChangeSetReplayer); ok {
			r.log.Debug("streaming history changesets on baseline", zap.Uint("target_version", targetVersion.ID()))
			if err := replayer.ReplayChanges(ctx, targetVersion, applyChanges); err != nil {
				return NewGetChangesetError(targetVersion.ID(), err)
			}
		} else {
			current := targetVersion
			var versions []registry.Version
			for current != nil && current.ID() > 0 {
				versions = append(versions, current)
				current = current.Previous()
			}
			for i := len(versions) - 1; i >= 0; i-- {
				cs, err := r.history.Get(versions[i])
				if err != nil {
					return NewGetChangesetError(versions[i].ID(), err)
				}
				if err := applyChanges(cs); err != nil {
					return NewGetChangesetError(versions[i].ID(), err)
				}
			}
		}
	}

	if resolutionHistory, ok := r.history.(registry.ResolutionHistory); ok {
		stored, err := resolutionHistory.GetDependencyResolution(targetVersion)
		if err == nil {
			resolution = stored
		} else if !errors.Is(err, registry.ErrDependencyResolutionNotFound) {
			return NewGetChangesetError(targetVersion.ID(), err)
		}
	}

	// A modern history version carries its exact graph. Reconcile that graph
	// once. Legacy versions are expanded once from their final declarations and
	// checkpointed after a successful transition.
	if resolution != nil {
		if planner == nil {
			return NewExpandChangesError(errors.New("stored dependency resolution has no configured reconciler"))
		}
		reconciled := false
		for _, directive := range r.directivesByKind[registry.NamespaceDependency] {
			snapshot := topology.StateMapToSlice(stateMap)
			result, ok, err := reconcileStoredResolution(ctx, directive,
				registry.ProvenancedState{Entries: baseline, Prov: targetProvBaselineClone(baselineState.Prov)},
				registry.ProvenancedState{Entries: snapshot, Prov: targetProv},
				resolution)
			if !ok {
				continue
			}
			if err != nil {
				planner.RollbackEffects(ctx, preparedEff)
				return NewExpandChangesError(err)
			}
			reconciled = reconciled || result.Applied
			if result.Resolution != nil {
				resolution = result.Resolution.Canonical()
			}
			plan := &regexp.Plan{Effects: result.Effects, Resolution: result.Resolution, Expanded: result.Applied}
			for _, additional := range result.Additional {
				plan.Ops = append(plan.Ops, regexp.ScopedOp{Operation: additional.Operation, Scope: additional.Scope})
			}
			plan.Ops, err = planner.SortOps(snapshot, plan.Ops)
			if err != nil {
				planner.RollbackEffects(ctx, plan.Effects)
				planner.RollbackEffects(ctx, preparedEff)
				return NewSortChangesError(err)
			}
			ops, _ := plan.SplitScopes()
			prepared, err := planner.PrepareEffects(ctx, plan.Effects)
			if err != nil {
				planner.RollbackEffects(ctx, prepared)
				planner.RollbackEffects(ctx, preparedEff)
				return NewPrepareEffectsError(err)
			}
			preparedEff = append(preparedEff, prepared...)
			applyStateOperations(stateMap, ops)
			targetProv = applyOpsToProvenance(targetProv, ops)
			for id, record := range result.Provenance {
				targetProv[canonicalEntryID(id)] = record
			}
		}
		if !reconciled {
			planner.RollbackEffects(ctx, preparedEff)
			return NewExpandChangesError(errors.New("stored dependency resolution has no configured reconciler"))
		}
	} else if planner != nil {
		declarations := topology.StateMapToSlice(stateMap)
		for _, entry := range declarations {
			if resolution != nil && entry.Kind == registry.NamespaceDependency {
				continue // One dependency expansion resolves the complete legacy root graph.
			}
			if len(r.directivesByKind[entry.Kind]) == 0 {
				continue
			}
			snapshot := topology.StateMapToSlice(stateMap)
			plan, err := planner.Expand(ctx, registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: entry}}, registry.ProvenancedState{Entries: snapshot, Prov: targetProv})
			if err != nil {
				planner.RollbackEffects(ctx, preparedEff)
				return NewExpandChangesError(err)
			}
			if !plan.Expanded {
				continue
			}
			plan.Ops, err = planner.SortOps(snapshot, plan.Ops)
			if err != nil {
				planner.RollbackEffects(ctx, plan.Effects)
				planner.RollbackEffects(ctx, preparedEff)
				return NewSortChangesError(err)
			}
			ops, _ := plan.SplitScopes()
			prepared, err := planner.PrepareEffects(ctx, plan.Effects)
			if err != nil {
				planner.RollbackEffects(ctx, prepared)
				planner.RollbackEffects(ctx, preparedEff)
				return NewPrepareEffectsError(err)
			}
			preparedEff = append(preparedEff, prepared...)
			if plan.Resolution != nil {
				resolution = plan.Resolution.Canonical()
			}
			applyStateOperations(stateMap, ops)
			targetProv = applyOpsToProvenance(targetProv, ops)
			for id, record := range plan.Provenance {
				targetProv[canonicalEntryID(id)] = record
			}
		}
	}

	if resolution != nil {
		if _, ok := r.history.(registry.ResolutionHeadCASHistory); !ok {
			if planner != nil {
				planner.RollbackEffects(ctx, preparedEff)
			}
			return NewSaveVersionError(ErrDurableResolutionUnsupported, nil)
		}
	}

	finalState := topology.StateMapToSlice(stateMap)
	newState, err := r.transitionState(ctx, r.state, finalState, r.Provenance(), targetProv)
	if err != nil {
		r.log.Error("failed to load state", zap.String("version", targetVersion.String()), zap.Error(err))
		if newState != nil && ctx.Err() == nil {
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				if planner != nil {
					planner.RollbackEffects(ctx, preparedEff)
				}
				return NewLoadStateError(err, rerr)
			}
		}
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return NewLoadStateError(err, nil)
	}

	if planner != nil {
		if err := planner.CommitEffects(ctx, preparedEff); err != nil {
			r.log.Error("failed to commit load-state effects", zap.Error(err))
			if rerr := r.rollback(ctx, newState, r.state); rerr != nil {
				planner.RollbackEffects(ctx, preparedEff)
				return NewCommitEffectsError(err, rerr)
			}
			planner.RollbackEffects(ctx, preparedEff)
			return NewCommitEffectsError(err, nil)
		}
	}

	var headCheckErr error
	if resolution != nil {
		headCheckErr = r.history.(registry.ResolutionHeadCASHistory).
			CompareAndSetHeadWithDependencyResolution(targetVersion, targetVersion, resolution)
	} else if cas, ok := r.history.(registry.HeadCASHistory); ok {
		headCheckErr = cas.CompareAndSetHead(targetVersion, targetVersion)
	}
	if headCheckErr != nil {
		var rollbackErr error
		if transitionErr := r.rollback(ctx, newState, r.state); transitionErr != nil {
			rollbackErr = transitionErr
		}
		if planner != nil {
			planner.RollbackEffects(ctx, preparedEff)
		}
		return NewSaveVersionError(headCheckErr, rollbackErr)
	}
	if planner != nil {
		if finalizeErr := planner.FinalizeEffects(ctx, preparedEff); finalizeErr != nil {
			r.log.Warn("failed to finalize effects after loading state", zap.Error(finalizeErr))
		}
	}

	r.state = newState
	r.baseline = append(registry.State(nil), baseline...)
	r.baselineProv = targetProvBaselineClone(baselineState.Prov)
	r.publishProvenance(provenanceForState(newState, targetProv, nil))
	// LoadState is the cold/reinitialization boundary. Overlays are deliberately
	// process-local and their owning controllers reconcile them after boot.
	r.overlays = make(map[string]registry.State)
	r.overlayOwners = make(map[registry.ID]string)
	r.overlayGeneration = make(map[string]uint64)
	// Invalidate snapshots retained across an explicit reload. A newly
	// constructed registry starts at epoch zero; a live registry never reuses a
	// generation token.
	if r.stateLoaded || r.overlayEpoch > 0 {
		r.overlayEpoch++
	}
	r.overlayFloor = r.overlayEpoch
	r.stateLoaded = true
	r.rebuildIndex()
	r.rebuildDepIndex()
	r.currentVersion = targetVersion
	r.currentResolution = resolution
	r.versionNum.Store(uint64(allocatorVersion))

	return nil
}

// targetProvBaselineClone clones the baseline provenance under canonical IDs.
func targetProvBaselineClone(prov registry.ProvMap) registry.ProvMap {
	out := make(registry.ProvMap, len(prov))
	for id, p := range prov {
		out[canonicalEntryID(id)] = p
	}
	return out
}

func reconcileStoredResolution(
	ctx context.Context,
	directive registry.Directive,
	current registry.ProvenancedState,
	target registry.ProvenancedState,
	resolution *registry.DependencyResolution,
) (registry.DirectiveResult, bool, error) {
	if reconciler, ok := directive.(registry.ResolutionTransitionDirective); ok {
		result, err := reconciler.ReconcileResolutionTransition(ctx, current, target, resolution)
		return result, true, err
	}
	if reconciler, ok := directive.(registry.ResolutionDirective); ok {
		result, err := reconciler.ReconcileResolution(ctx, target, resolution)
		return result, true, err
	}
	return registry.DirectiveResult{}, false, nil
}

func applyStateOperations(stateMap registry.StateMap, ops registry.ChangeSet) {
	for _, op := range ops {
		id := canonicalEntryID(op.Entry.ID)
		switch op.Kind {
		case registry.EntryCreate, registry.EntryUpdate:
			op.Entry.ID = id
			stateMap[id] = op.Entry
		case registry.EntryDelete:
			delete(stateMap, id)
		}
	}
}

func canonicalizeChangeSetIDs(changes registry.ChangeSet) {
	for i := range changes {
		changes[i].Entry.ID = canonicalEntryID(changes[i].Entry.ID)
		if changes[i].OriginalEntry != nil {
			original := *changes[i].OriginalEntry
			original.ID = canonicalEntryID(original.ID)
			changes[i].OriginalEntry = &original
		}
	}
}

func canonicalEntryID(id registry.ID) registry.ID {
	return id.Canonical()
}

// rollback state desync between actual state in system and state in history.
// The runner records what its compensation actually did into the context's
// RollbackOutcome; a desynced state's provenance is reconstructed from the
// surviving operations, never guessed.
func (r *Reg) rollback(ctx context.Context, from, to registry.State) error {
	r.log.Debug("attempting to rollback")

	published := r.Provenance()
	outcome := &registry.RollbackOutcome{}
	ctx = registry.WithRollbackOutcome(ctx, outcome)
	partial, err := r.transitionState(ctx, from, to, provenanceForState(from, published, nil), published)
	if err == nil {
		return nil // success
	}
	err = errors.Join(err, outcome.Err())

	r.state = partial // we remain in a desynced state
	r.rebuildIndex()
	r.rebuildDepIndex()
	partialProv := applyOpsToProvenance(published, outcome.Surviving())
	r.publishProvenance(provenanceForState(partial, partialProv, nil))
	r.reconcileKnownOverlaysAfterFailedRollback()

	return err
}

func (r *Reg) transitionState(ctx context.Context, from, to registry.State, fromProv, toProv registry.ProvMap) (registry.State, error) {
	r.log.Debug("transitioning state")

	cs, terr := r.builder.BuildDelta(from, to)
	if terr != nil {
		return nil, NewComputeTransitionError(terr)
	}

	if len(cs) == 0 {
		return from, nil
	}

	annotateChangeSet(cs, fromProv, toProv)
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
