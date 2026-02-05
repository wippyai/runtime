package expansion

import (
	"context"
	"sort"

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

	seenIDs := make(map[registry.ID]struct{}, len(changes))
	scoped := make([]ScopedOp, 0, len(changes))
	for _, op := range changes {
		seenIDs[op.Entry.ID] = struct{}{}
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
				if _, exists := seenIDs[add.Operation.Entry.ID]; exists {
					return nil, NewDirectiveExpansionConflictError(add.Operation.Entry.ID)
				}
				seenIDs[add.Operation.Entry.ID] = struct{}{}
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

// SortOps sorts scoped operations by dependency order.
func (p *Planner) SortOps(fromState registry.State, ops []ScopedOp) []ScopedOp {
	if len(ops) == 0 {
		return ops
	}

	deleteOps := make([]ScopedOp, 0, len(ops))
	otherOps := make([]ScopedOp, 0, len(ops))
	for _, op := range ops {
		if op.Operation.Kind == registry.EntryDelete {
			deleteOps = append(deleteOps, op)
		} else {
			otherOps = append(otherOps, op)
		}
	}

	result := make([]ScopedOp, 0, len(ops))
	if len(deleteOps) > 0 {
		result = append(result, sortScopedDeletes(fromState, deleteOps, p.Resolver)...)
	}
	if len(otherOps) > 0 {
		result = append(result, sortScopedCreates(otherOps, p.Resolver)...)
	}
	return result
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

func sortScopedDeletes(fromState registry.State, ops []ScopedOp, resolver registry.DependencyResolver) []ScopedOp {
	fromStateMap := make(map[registry.ID]registry.Entry, len(fromState))
	for _, entry := range fromState {
		fromStateMap[entry.ID] = entry
	}

	entries := make([]registry.Entry, 0, len(ops))
	for _, op := range ops {
		if entry, ok := fromStateMap[op.Operation.Entry.ID]; ok {
			entries = append(entries, entry)
		} else {
			entries = append(entries, op.Operation.Entry)
		}
	}

	sortedEntries := sortEntriesWithFallback(entries, resolver)

	byID := make(map[registry.ID]ScopedOp, len(ops))
	for _, op := range ops {
		byID[op.Operation.Entry.ID] = op
	}

	result := make([]ScopedOp, 0, len(ops))
	for i := len(sortedEntries) - 1; i >= 0; i-- {
		entry := sortedEntries[i]
		if op, ok := byID[entry.ID]; ok {
			result = append(result, op)
		}
	}

	return result
}

func sortScopedCreates(ops []ScopedOp, resolver registry.DependencyResolver) []ScopedOp {
	entries := make([]registry.Entry, 0, len(ops))
	for _, op := range ops {
		entries = append(entries, op.Operation.Entry)
	}

	sortedEntries := sortEntriesWithFallback(entries, resolver)

	byID := make(map[registry.ID]ScopedOp, len(ops))
	for _, op := range ops {
		byID[op.Operation.Entry.ID] = op
	}

	result := make([]ScopedOp, 0, len(ops))
	for _, entry := range sortedEntries {
		if op, ok := byID[entry.ID]; ok {
			result = append(result, op)
		}
	}

	return result
}

func sortEntriesWithFallback(entries []registry.Entry, resolver registry.DependencyResolver) []registry.Entry {
	sorted, err := regtop.SortEntriesByDependency(entries, resolver)
	if err == nil {
		return sorted
	}

	fallback := make([]registry.Entry, len(entries))
	copy(fallback, entries)
	sort.Slice(fallback, func(i, j int) bool {
		return fallback[i].ID.String() < fallback[j].ID.String()
	})
	return fallback
}

func entryFromSnapshot(snapshot registry.State, id registry.ID) (registry.Entry, bool) {
	for _, entry := range snapshot {
		if entry.ID.Equal(id) {
			return entry, true
		}
	}
	return registry.Entry{}, false
}
