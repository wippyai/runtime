// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/registry"
	regtop "github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// ScopedOp ties an operation to its persistence scope.
type ScopedOp struct {
	Operation registry.Operation
	Scope     registry.Scope
}

// Plan contains expanded operations and effects.
type Plan struct {
	Resolution *registry.DependencyResolution
	Ops        []ScopedOp
	Effects    []registry.Effect
	Expanded   bool
}

// SplitScopes separates all operations from history-only operations.
func (p *Plan) SplitScopes() (all registry.ChangeSet, history registry.ChangeSet) {
	if p == nil || len(p.Ops) == 0 {
		return nil, nil
	}

	all = make(registry.ChangeSet, 0, len(p.Ops))
	history = make(registry.ChangeSet, 0, len(p.Ops))

	for _, sop := range p.Ops {
		all = append(all, sop.Operation)
		if sop.Scope == registry.ScopeHistory {
			history = append(history, sop.Operation)
		}
	}

	return all, history
}

// Planner expands operations and handles sorting and effects.
type Planner struct {
	DirectivesByKind map[registry.Kind][]registry.Directive
	Resolver         registry.DependencyResolver
	Log              *zap.Logger
}

// NewPlanner creates a planner with given directives and resolver.
func NewPlanner(directivesByKind map[registry.Kind][]registry.Directive, resolver registry.DependencyResolver, log *zap.Logger) *Planner {
	if log == nil {
		log = zap.NewNop()
	}
	return &Planner{
		DirectivesByKind: directivesByKind,
		Resolver:         resolver,
		Log:              log,
	}
}

// Expand turns a changeset into a plan by applying registered directives.
func (p *Planner) Expand(ctx context.Context, changes registry.ChangeSet, snapshot registry.ProvenancedState) (*Plan, error) {
	if len(changes) == 0 {
		return &Plan{}, nil
	}

	originalIDs := make(map[registry.ID]struct{}, len(changes))
	expandedOpsByID := make(map[registry.ID]map[string]struct{})
	scoped := make([]ScopedOp, 0, len(changes))
	for _, op := range changes {
		originalIDs[op.Entry.ID] = struct{}{}
		scoped = append(scoped, ScopedOp{Operation: op, Scope: registry.ScopeHistory})
	}

	if len(p.DirectivesByKind) == 0 {
		return &Plan{Ops: scoped}, nil
	}

	var effects []registry.Effect
	var ownedEffects []registry.Effect
	succeeded := false
	defer func() {
		if !succeeded {
			p.RollbackEffects(ctx, ownedEffects)
		}
	}()
	var resolution *registry.DependencyResolution
	expanded := false
	originalCount := len(scoped)
	type batchKey struct {
		kind           registry.Kind
		directiveIndex int
	}
	type batchExpansion struct {
		result     registry.DirectiveResult
		firstIndex int
	}
	batchExpansions := make(map[batchKey]batchExpansion)
	for kind, directives := range p.DirectivesByKind {
		var changes registry.ChangeSet
		firstIndex := -1
		for i := 0; i < originalCount; i++ {
			op := scoped[i].Operation
			opKind := op.Entry.Kind
			if opKind == "" {
				if entry, ok := entryFromSnapshot(snapshot, op.Entry.ID); ok {
					opKind = entry.Kind
				}
			}
			if opKind != kind {
				continue
			}
			if firstIndex < 0 {
				firstIndex = i
			}
			changes = append(changes, op)
		}
		if len(changes) < 2 {
			continue
		}
		for directiveIndex, directive := range directives {
			batch, ok := directive.(registry.ChangesDirective)
			if !ok {
				continue
			}
			result, err := batch.ExpandChanges(ctx, changes, snapshot)
			if err != nil {
				return nil, err
			}
			ownedEffects = append(ownedEffects, result.Effects...)
			if !result.Applied && result.OriginalScope == nil && result.Resolution == nil && len(result.Additional) == 0 && len(result.Effects) == 0 {
				continue // Capability is present but not configured; use per-op expansion.
			}
			if result.OriginalScope != nil {
				return nil, NewDirectiveResultInvalidError(changes[0].Entry.ID, kind)
			}
			batchExpansions[batchKey{kind: kind, directiveIndex: directiveIndex}] = batchExpansion{result: result, firstIndex: firstIndex}
		}
	}

	for i := 0; i < originalCount; i++ {
		op := scoped[i].Operation
		kind := op.Entry.Kind
		if kind == "" {
			if entry, ok := entryFromSnapshot(snapshot, op.Entry.ID); ok {
				kind = entry.Kind
			}
		}

		directives := p.DirectivesByKind[kind]
		if len(directives) == 0 {
			continue
		}

		for directiveIndex, directive := range directives {
			var res registry.DirectiveResult
			if batch, ok := batchExpansions[batchKey{kind: kind, directiveIndex: directiveIndex}]; ok {
				if i != batch.firstIndex {
					continue
				}
				res = batch.result
			} else {
				var err error
				res, err = directive.Expand(ctx, op, snapshot)
				if err != nil {
					return nil, err
				}
				ownedEffects = append(ownedEffects, res.Effects...)
			}
			if !res.Applied {
				if res.OriginalScope != nil || res.Resolution != nil || len(res.Additional) > 0 || len(res.Effects) > 0 {
					return nil, NewDirectiveResultInvalidError(op.Entry.ID, kind)
				}
				continue
			}
			expanded = true
			if res.Resolution != nil {
				canonical := res.Resolution.Canonical()
				if resolution != nil && resolution.Digest != canonical.Digest {
					return nil, NewDirectiveExpansionConflictError(op.Entry.ID)
				}
				resolution = canonical
			}
			if res.OriginalScope != nil {
				scoped[i].Scope = *res.OriginalScope
			}
			for _, add := range res.Additional {
				addID := add.Operation.Entry.ID
				if _, exists := originalIDs[addID]; exists {
					continue
				}
				if !recordExpandedOperation(expandedOpsByID, add.Operation) {
					return nil, NewDirectiveExpansionConflictError(addID)
				}
				scoped = append(scoped, ScopedOp{
					Operation: add.Operation,
					Scope:     add.Scope,
				})
			}
			if len(res.Effects) > 0 {
				effects = append(effects, res.Effects...)
			}
		}
	}

	succeeded = true
	return &Plan{Ops: scoped, Effects: effects, Resolution: resolution, Expanded: expanded}, nil
}

func recordExpandedOperation(opsByID map[registry.ID]map[string]struct{}, op registry.Operation) bool {
	id := op.Entry.ID
	kinds := opsByID[id]
	if kinds == nil {
		opsByID[id] = map[string]struct{}{op.Kind: {}}
		return true
	}
	if _, exists := kinds[op.Kind]; exists {
		return false
	}
	if len(kinds) != 1 {
		return false
	}

	if op.Kind == registry.EntryCreate {
		if _, ok := kinds[registry.EntryDelete]; ok {
			kinds[op.Kind] = struct{}{}
			return true
		}
	}
	if op.Kind == registry.EntryDelete {
		if _, ok := kinds[registry.EntryCreate]; ok {
			kinds[op.Kind] = struct{}{}
			return true
		}
	}
	return false
}

// SortOps sorts scoped operations with the canonical registry operation sorter.
func (p *Planner) SortOps(fromState registry.State, ops []ScopedOp) ([]ScopedOp, error) {
	if len(ops) == 0 {
		return ops, nil
	}

	changes := make(registry.ChangeSet, 0, len(ops))
	scopes := make(map[operationKey][]registry.Scope, len(ops))
	for _, op := range ops {
		changes = append(changes, op.Operation)
		key := operationKey{kind: op.Operation.Kind, id: op.Operation.Entry.ID}
		scopes[key] = append(scopes[key], op.Scope)
	}

	stateBuilder := regtop.NewStateBuilder(p.Log, p.Resolver)
	sorted, err := stateBuilder.SortChangeSet(fromState, changes)
	if err != nil {
		return nil, err
	}

	result := make([]ScopedOp, 0, len(sorted))
	for _, op := range sorted {
		key := operationKey{kind: op.Kind, id: op.Entry.ID}
		queue := scopes[key]
		if len(queue) == 0 {
			return nil, NewSortedOperationScopeMissingError(op.Entry.ID, op.Kind)
		}
		result = append(result, ScopedOp{Operation: op, Scope: queue[0]})
		scopes[key] = queue[1:]
	}

	return result, nil
}

type operationKey struct {
	kind string
	id   registry.ID
}

// PrepareEffects runs Prepare on each effect and returns prepared effects.
func (p *Planner) PrepareEffects(ctx context.Context, effects []registry.Effect) ([]registry.Effect, error) {
	if len(effects) == 0 {
		return nil, nil
	}

	prepared := make([]registry.Effect, 0, len(effects))
	for _, eff := range effects {
		if err := eff.Prepare(ctx); err != nil {
			return prepared, err
		}
		prepared = append(prepared, eff)
	}
	return prepared, nil
}

// CommitEffects runs Commit on each effect.
func (p *Planner) CommitEffects(ctx context.Context, effects []registry.Effect) error {
	for _, eff := range effects {
		if err := eff.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// FinalizeEffects runs optional irreversible cleanup after history persistence.
// Callers deliberately treat failures as cleanup leaks rather than rolling back
// an already durable registry transition.
func (p *Planner) FinalizeEffects(ctx context.Context, effects []registry.Effect) error {
	var errs []error
	for _, eff := range effects {
		finalizer, ok := eff.(registry.FinalizingEffect)
		if !ok {
			continue
		}
		if err := finalizer.Finalize(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RollbackEffects runs Rollback on each effect in reverse order.
func (p *Planner) RollbackEffects(ctx context.Context, effects []registry.Effect) {
	for i := len(effects) - 1; i >= 0; i-- {
		if err := effects[i].Rollback(ctx); err != nil {
			p.Log.Warn("failed to rollback effect", zap.Error(err))
		}
	}
}

func entryFromSnapshot(snapshot registry.ProvenancedState, id registry.ID) (registry.Entry, bool) {
	for _, entry := range snapshot.Entries {
		if entry.ID.Equal(id) {
			return entry, true
		}
	}
	return registry.Entry{}, false
}
