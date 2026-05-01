// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"context"

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
	Ops      []ScopedOp
	Effects  []registry.Effect
	Expanded bool
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
func (p *Planner) Expand(ctx context.Context, changes registry.ChangeSet, snapshot registry.State) (*Plan, error) {
	if len(changes) == 0 {
		return &Plan{}, nil
	}

	originalIDs := make(map[registry.ID]struct{}, len(changes))
	expandedIDs := make(map[registry.ID]struct{})
	scoped := make([]ScopedOp, 0, len(changes))
	for _, op := range changes {
		originalIDs[op.Entry.ID] = struct{}{}
		scoped = append(scoped, ScopedOp{Operation: op, Scope: registry.ScopeHistory})
	}

	if len(p.DirectivesByKind) == 0 {
		return &Plan{Ops: scoped}, nil
	}

	var effects []registry.Effect
	expanded := false
	originalCount := len(scoped)

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

		for _, directive := range directives {
			res, err := directive.Expand(ctx, op, snapshot)
			if err != nil {
				return nil, err
			}
			if !res.Applied {
				if res.OriginalScope != nil || len(res.Additional) > 0 || len(res.Effects) > 0 {
					return nil, NewDirectiveResultInvalidError(op.Entry.ID, kind)
				}
				continue
			}
			expanded = true
			if res.OriginalScope != nil {
				scoped[i].Scope = *res.OriginalScope
			}
			for _, add := range res.Additional {
				addID := add.Operation.Entry.ID
				if _, exists := originalIDs[addID]; exists {
					continue
				}
				if _, exists := expandedIDs[addID]; exists {
					return nil, NewDirectiveExpansionConflictError(addID)
				}
				expandedIDs[addID] = struct{}{}
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

	return &Plan{Ops: scoped, Effects: effects, Expanded: expanded}, nil
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

// RollbackEffects runs Rollback on each effect in reverse order.
func (p *Planner) RollbackEffects(ctx context.Context, effects []registry.Effect) {
	for i := len(effects) - 1; i >= 0; i-- {
		if err := effects[i].Rollback(ctx); err != nil {
			p.Log.Warn("failed to rollback effect", zap.Error(err))
		}
	}
}

func entryFromSnapshot(snapshot registry.State, id registry.ID) (registry.Entry, bool) {
	for _, entry := range snapshot {
		if entry.ID.Equal(id) {
			return entry, true
		}
	}
	return registry.Entry{}, false
}
