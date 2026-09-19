// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	"go.uber.org/zap"
)

// planExpectation is what ApplyPlan holds an apply to: the version the plan
// was computed against and the digest of everything it would do.
type planExpectation struct {
	base   registry.Version
	digest string
	// measured reports that digest was produced by a Plan and must match.
	measured bool
}

// verify compares the re-expanded apply with the reviewed plan before any
// effect is prepared, so a refused plan leaves nothing staged.
func (e *planExpectation) verify(base registry.Version, all, history registry.ChangeSet, resolution *registry.DependencyResolution, effects []registry.Effect) error {
	if base == nil || base.ID() != e.base.ID() {
		var actual uint
		if base != nil {
			actual = base.ID()
		}
		return NewPlanBaseConflictError(e.base.ID(), actual)
	}
	if !e.measured {
		return nil
	}
	_, digest, err := measurePlan(all, history, resolution, effects)
	if err != nil {
		return err
	}
	if digest != e.digest {
		return NewPlanDriftError(e.digest, digest)
	}
	return nil
}

// expandLocked runs directive expansion and orders the result. The returned
// effects are unprepared and belong to the caller, including their staging.
// Caller holds applyMu.
func (r *Reg) expandLocked(ctx context.Context, changes registry.ChangeSet, snapshot registry.State) (*regexp.Planner, *regexp.Plan, error) {
	planner := regexp.NewPlanner(r.directivesByKind, r.resolver, r.log.Named("expansion"))
	plan, err := planner.Expand(ctx, changes, snapshot)
	if err != nil {
		return nil, nil, NewExpandChangesError(err)
	}
	plan.Ops, err = planner.SortOps(snapshot, plan.Ops)
	if err != nil {
		planner.RollbackEffects(ctx, plan.Effects)
		return nil, nil, NewSortChangesError(err)
	}
	return planner, plan, nil
}

// Plan computes what applying changes on top of base would do and releases
// every staged resource before returning. It prepares nothing.
func (r *Reg) Plan(ctx context.Context, base registry.Version, changes registry.ChangeSet) (*registry.Plan, error) {
	if base == nil {
		return nil, ErrPlanBaseRequired
	}
	r.applyMu.Lock()
	defer r.applyMu.Unlock()

	// The plan is computed from the same copy it carries as Requested, so a
	// later ApplyPlan measures exactly the payload forms measured here.
	requested := cloneChangeSet(changes)
	changes = append(registry.ChangeSet(nil), requested...)
	canonicalizeChangeSetIDs(changes)

	r.mu.RLock()
	snapshot := make(registry.State, len(r.state))
	copy(snapshot, r.state)
	current := r.currentVersion
	resolution := r.currentResolution
	r.mu.RUnlock()
	if current == nil || current.ID() != base.ID() {
		var actual uint
		if current != nil {
			actual = current.ID()
		}
		return nil, NewPlanBaseConflictError(base.ID(), actual)
	}
	changes = normalizeRegistryMetadata(changes, snapshot)

	var (
		allOps, historyOps registry.ChangeSet
		effects            []registry.Effect
	)
	if len(r.directivesByKind) > 0 {
		planner, plan, err := r.expandLocked(ctx, changes, snapshot)
		if err != nil {
			return nil, err
		}
		defer planner.RollbackEffects(context.WithoutCancel(ctx), plan.Effects)
		effects = plan.Effects
		allOps, historyOps = plan.SplitScopes()
		if plan.Resolution != nil {
			resolution = plan.Resolution.Canonical()
		}
	} else {
		sorted, err := r.sortWithIndex(snapshot, changes)
		if err != nil {
			return nil, NewSortChangesError(err)
		}
		allOps, historyOps = sorted, sorted
	}
	sorted, err := r.sortWithIndex(snapshot, allOps)
	if err != nil {
		return nil, NewSortChangesError(err)
	}
	allOps = sorted
	if err := r.validateDurableTransitionAgainstOverlays(allOps); err != nil {
		return nil, err
	}
	targets, digest, err := measurePlan(allOps, historyOps, resolution, effects)
	if err != nil {
		return nil, err
	}
	r.log.Debug("plan computed", zap.Int("requested", len(requested)), zap.Int("expanded", len(allOps)), zap.String("digest", digest))
	return &registry.Plan{
		Base:       base,
		Requested:  requested,
		Changes:    cloneChangeSet(allOps),
		History:    cloneChangeSet(historyOps),
		Resolution: resolution,
		Effects:    targets,
		Digest:     digest,
	}, nil
}

// ApplyPlan applies the operations a plan was computed from, refusing when the
// registry or the plan's external inputs moved since it was computed.
func (r *Reg) ApplyPlan(ctx context.Context, plan *registry.Plan) (registry.Version, error) {
	if plan == nil || plan.Base == nil {
		return nil, ErrPlanBaseRequired
	}
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	return r.applyLocked(ctx, plan.Requested, &planExpectation{base: plan.Base, digest: plan.Digest, measured: true})
}

// ApplyAt applies changes decided against base, refusing if the registry has
// moved since. It expands once; a caller that reviewed a Plan uses ApplyPlan.
func (r *Reg) ApplyAt(ctx context.Context, base registry.Version, changes registry.ChangeSet) (registry.Version, error) {
	if base == nil {
		return nil, ErrPlanBaseRequired
	}
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	return r.applyLocked(ctx, changes, &planExpectation{base: base})
}

// measurePlan digests everything an apply would do: the ordered operations,
// the history subset, the module graph, and each effect's target. Two plans
// with the same digest do the same thing.
func measurePlan(all, history registry.ChangeSet, resolution *registry.DependencyResolution, effects []registry.Effect) ([]registry.EffectTarget, string, error) {
	targets := make([]registry.EffectTarget, 0, len(effects))
	for _, effect := range effects {
		target, err := effect.Target()
		if err != nil {
			return nil, "", NewPlanDigestError(err)
		}
		targets = append(targets, target)
	}
	type operation struct {
		Kind      event.Kind    `json:"kind"`
		ID        string        `json:"id"`
		EntryKind registry.Kind `json:"entry_kind"`
		Meta      any           `json:"meta,omitempty"`
		Data      any           `json:"data,omitempty"`
		Format    string        `json:"format,omitempty"`
	}
	describe := func(changes registry.ChangeSet) []operation {
		out := make([]operation, 0, len(changes))
		for _, op := range changes {
			item := operation{Kind: op.Kind, ID: op.Entry.ID.String(), EntryKind: op.Entry.Kind, Meta: op.Entry.Meta}
			if op.Entry.Data != nil {
				item.Format = op.Entry.Data.Format()
				item.Data = op.Entry.Data.Data()
			}
			out = append(out, item)
		}
		return out
	}
	var canonical *registry.DependencyResolution
	if resolution != nil {
		canonical = resolution.Canonical()
	}
	encoded, err := json.Marshal(struct {
		Changes    []operation                    `json:"changes"`
		History    []operation                    `json:"history"`
		Resolution *registry.DependencyResolution `json:"resolution,omitempty"`
		Effects    []registry.EffectTarget        `json:"effects"`
	}{Changes: describe(all), History: describe(history), Resolution: canonical, Effects: targets})
	if err != nil {
		return nil, "", NewPlanDigestError(err)
	}
	sum := sha256.Sum256(encoded)
	return targets, hex.EncodeToString(sum[:]), nil
}

func cloneChangeSet(changes registry.ChangeSet) registry.ChangeSet {
	out := append(registry.ChangeSet(nil), changes...)
	for i := range out {
		out[i].Entry = cloneOverlayEntry(out[i].Entry)
		if out[i].OriginalEntry != nil {
			original := cloneOverlayEntry(*out[i].OriginalEntry)
			out[i].OriginalEntry = &original
		}
	}
	return out
}

func (r *Reg) applyLocked(ctx context.Context, changes registry.ChangeSet, expect *planExpectation) (registry.Version, error) {
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
	)

	r.mu.RLock()
	snapshot = make(registry.State, len(r.state))
	copy(snapshot, r.state)
	baseVersion = r.currentVersion
	resolution = r.currentResolution
	r.mu.RUnlock()
	changes = normalizeRegistryMetadata(changes, snapshot)

	var effects []registry.Effect
	if len(r.directivesByKind) > 0 {
		var plan *regexp.Plan
		var err error
		planner, plan, err = r.expandLocked(ctx, changes, snapshot)
		if err != nil {
			return nil, err
		}
		effects = plan.Effects
		allOps, historyOps = plan.SplitScopes()
		if plan.Resolution != nil {
			resolution = plan.Resolution.Canonical()
			resolutionChanged = true
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
			planner.RollbackEffects(ctx, effects)
		}
		return nil, NewSortChangesError(sortErr)
	}
	if err := r.validateDurableTransitionAgainstOverlays(allOps); err != nil {
		if planner != nil {
			planner.RollbackEffects(ctx, effects)
		}
		return nil, err
	}
	if expect != nil {
		if err := expect.verify(baseVersion, allOps, historyOps, resolution, effects); err != nil {
			if planner != nil {
				planner.RollbackEffects(ctx, effects)
			}
			return nil, err
		}
	}
	if planner != nil {
		var err error
		preparedEff, err = planner.PrepareEffects(ctx, effects)
		if err != nil {
			planner.RollbackEffects(ctx, preparedEff)
			return nil, NewPrepareEffectsError(err)
		}
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
		r.currentVersion = newVersion
		r.currentResolution = resolution
		r.publishSnapshot()
		return newVersion, nil
	}

	r.state = newState
	r.rebuildIndex()
	r.patchDepIndex(allOps)
	r.publishSnapshot()
	if planner != nil {
		if finalizeErr := planner.FinalizeEffects(ctx, preparedEff); finalizeErr != nil {
			r.log.Warn("failed to finalize effects after baseline transition", zap.Error(finalizeErr))
		}
	}
	return r.currentVersion, nil
}
