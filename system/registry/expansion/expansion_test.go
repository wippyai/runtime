// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type stubDirective struct {
	expandFunc func(context.Context, registry.Operation, registry.State) (registry.DirectiveResult, error)
}

func (s *stubDirective) Expand(ctx context.Context, op registry.Operation, snap registry.State) (registry.DirectiveResult, error) {
	return s.expandFunc(ctx, op, snap)
}

func newEntry(ns, name string, kind registry.Kind) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID(ns, name),
		Kind: kind,
	}
}

func newOp(kind event.Kind, entry registry.Entry) registry.Operation {
	return registry.Operation{Kind: kind, Entry: entry}
}

// --- Plan.SplitScopes ---

func TestPlan_SplitScopes_Nil(t *testing.T) {
	var p *Plan
	all, hist := p.SplitScopes()
	assert.Nil(t, all)
	assert.Nil(t, hist)
}

func TestPlan_SplitScopes_Empty(t *testing.T) {
	p := &Plan{}
	all, hist := p.SplitScopes()
	assert.Nil(t, all)
	assert.Nil(t, hist)
}

func TestPlan_SplitScopes_Mixed(t *testing.T) {
	e1 := newEntry("a", "one", "svc")
	e2 := newEntry("a", "two", "svc")

	p := &Plan{
		Ops: []ScopedOp{
			{Operation: newOp(registry.EntryCreate, e1), Scope: registry.ScopeHistory},
			{Operation: newOp(registry.EntryCreate, e2), Scope: registry.ScopeBaseline},
		},
	}

	all, hist := p.SplitScopes()
	assert.Len(t, all, 2)
	assert.Len(t, hist, 1)
	assert.Equal(t, e1.ID, hist[0].Entry.ID)
}

// --- NewPlanner ---

func TestNewPlanner_NilLogger(t *testing.T) {
	p := NewPlanner(nil, nil, nil)
	require.NotNil(t, p)
	assert.NotNil(t, p.Log)
}

// --- Expand ---

func TestPlanner_Expand_EmptyChanges(t *testing.T) {
	p := NewPlanner(nil, nil, zap.NewNop())
	plan, err := p.Expand(context.Background(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Empty(t, plan.Ops)
}

func TestPlanner_Expand_NoDirectives(t *testing.T) {
	p := NewPlanner(nil, nil, zap.NewNop())
	entry := newEntry("app", "svc", "service")
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	plan, err := p.Expand(context.Background(), changes, nil)
	require.NoError(t, err)
	require.Len(t, plan.Ops, 1)
	assert.Equal(t, entry.ID, plan.Ops[0].Operation.Entry.ID)
	assert.Equal(t, registry.ScopeHistory, plan.Ops[0].Scope)
	assert.False(t, plan.Expanded)
}

func TestPlanner_Expand_DirectiveNotApplied(t *testing.T) {
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{Applied: false}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"svc": {dir}}, nil, zap.NewNop())
	entry := newEntry("app", "item", "svc")
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	plan, err := p.Expand(context.Background(), changes, nil)
	require.NoError(t, err)
	assert.Len(t, plan.Ops, 1)
	assert.False(t, plan.Expanded)
}

func TestPlanner_Expand_DirectiveApplied_AddsOps(t *testing.T) {
	extra := newEntry("mod", "extra", "service")
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{
				{Operation: newOp(registry.EntryCreate, extra), Scope: registry.ScopeBaseline},
			},
		}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir}}, nil, zap.NewNop())
	entry := newEntry("app", "dep", "dep")
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	plan, err := p.Expand(context.Background(), changes, nil)
	require.NoError(t, err)
	assert.Len(t, plan.Ops, 2)
	assert.True(t, plan.Expanded)
	assert.Equal(t, registry.ScopeBaseline, plan.Ops[1].Scope)
}

func TestPlanner_Expand_DirectiveError(t *testing.T) {
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{}, errors.New("expand failed")
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir}}, nil, zap.NewNop())
	entry := newEntry("app", "dep", "dep")
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	_, err := p.Expand(context.Background(), changes, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expand failed")
}

func TestPlanner_Expand_SkipDuplicateFromOriginalChangeset(t *testing.T) {
	entry := newEntry("app", "dep", "dep")
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{
				{Operation: newOp(registry.EntryUpdate, entry), Scope: registry.ScopeBaseline},
			},
		}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir}}, nil, zap.NewNop())
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	plan, err := p.Expand(context.Background(), changes, nil)
	require.NoError(t, err)
	assert.Len(t, plan.Ops, 1)
	assert.True(t, plan.Expanded)
}

func TestPlanner_Expand_ConflictBetweenDirectiveExpansions(t *testing.T) {
	shared := newEntry("mod", "svc", "service")
	dir1 := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{
				{Operation: newOp(registry.EntryCreate, shared), Scope: registry.ScopeBaseline},
			},
		}, nil
	}}
	dir2 := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{
				{Operation: newOp(registry.EntryUpdate, shared), Scope: registry.ScopeBaseline},
			},
		}, nil
	}}

	entry := newEntry("app", "dep", "dep")
	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir1, dir2}}, nil, zap.NewNop())
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	_, err := p.Expand(context.Background(), changes, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expansion produced entry")
}

func TestPlanner_Expand_InvalidResult_NotAppliedButHasData(t *testing.T) {
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied: false,
			Additional: []registry.ScopedOperation{
				{Operation: newOp(registry.EntryCreate, newEntry("x", "y", "z")), Scope: registry.ScopeBaseline},
			},
		}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir}}, nil, zap.NewNop())
	entry := newEntry("app", "dep", "dep")
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	_, err := p.Expand(context.Background(), changes, nil)
	require.Error(t, err)
}

func TestPlanner_Expand_ScopeOverride(t *testing.T) {
	scope := registry.ScopeBaseline
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied:       true,
			OriginalScope: &scope,
		}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir}}, nil, zap.NewNop())
	entry := newEntry("app", "dep", "dep")
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	plan, err := p.Expand(context.Background(), changes, nil)
	require.NoError(t, err)
	assert.Equal(t, registry.ScopeBaseline, plan.Ops[0].Scope)
}

func TestPlanner_Expand_KindFromSnapshot(t *testing.T) {
	extra := newEntry("mod", "extra", "service")
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{
				{Operation: newOp(registry.EntryCreate, extra), Scope: registry.ScopeBaseline},
			},
		}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir}}, nil, zap.NewNop())

	// Operation has no kind, but snapshot has the entry with kind "dep"
	op := registry.Operation{
		Kind:  registry.EntryUpdate,
		Entry: registry.Entry{ID: registry.NewID("app", "dep")},
	}
	snapshot := registry.State{
		{ID: registry.NewID("app", "dep"), Kind: "dep"},
	}

	plan, err := p.Expand(context.Background(), registry.ChangeSet{op}, snapshot)
	require.NoError(t, err)
	assert.Len(t, plan.Ops, 2)
	assert.True(t, plan.Expanded)
}

func TestPlanner_Expand_Effects(t *testing.T) {
	eff := &testEffect{}
	dir := &stubDirective{expandFunc: func(_ context.Context, _ registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		return registry.DirectiveResult{
			Applied: true,
			Effects: []registry.Effect{eff},
		}, nil
	}}

	p := NewPlanner(map[registry.Kind][]registry.Directive{"dep": {dir}}, nil, zap.NewNop())
	entry := newEntry("app", "dep", "dep")
	changes := registry.ChangeSet{newOp(registry.EntryCreate, entry)}

	plan, err := p.Expand(context.Background(), changes, nil)
	require.NoError(t, err)
	assert.Len(t, plan.Effects, 1)
}

// --- SortOps ---

func TestPlanner_SortOps_Empty(t *testing.T) {
	p := NewPlanner(nil, nil, zap.NewNop())
	result, err := p.SortOps(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestPlanner_SortOps_UsesCanonicalRewireOrder(t *testing.T) {
	oldHelper := newEntry("old", "helper", "svc")
	newHelper := newEntry("new", "helper", "svc")
	oldConsumer := newEntry("app", "consumer", "svc")
	oldConsumer.Meta = map[string]any{registry.TagDependsOn: []string{oldHelper.ID.String()}}
	newConsumer := newEntry("app", "consumer", "svc")
	newConsumer.Meta = map[string]any{registry.TagDependsOn: []string{newHelper.ID.String()}}

	ops := []ScopedOp{
		{Operation: newOp(registry.EntryDelete, oldHelper), Scope: registry.ScopeHistory},
		{Operation: newOp(registry.EntryUpdate, newConsumer), Scope: registry.ScopeHistory},
		{Operation: newOp(registry.EntryCreate, newHelper), Scope: registry.ScopeBaseline},
	}

	p := NewPlanner(nil, nil, zap.NewNop())
	result, err := p.SortOps(registry.State{oldHelper, oldConsumer}, ops)
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, registry.EntryCreate, result[0].Operation.Kind)
	assert.Equal(t, newHelper.ID, result[0].Operation.Entry.ID)
	assert.Equal(t, registry.ScopeBaseline, result[0].Scope)
	assert.Equal(t, registry.EntryUpdate, result[1].Operation.Kind)
	assert.Equal(t, newConsumer.ID, result[1].Operation.Entry.ID)
	assert.Equal(t, registry.ScopeHistory, result[1].Scope)
	assert.Equal(t, registry.EntryDelete, result[2].Operation.Kind)
	assert.Equal(t, oldHelper.ID, result[2].Operation.Entry.ID)
	assert.Equal(t, registry.ScopeHistory, result[2].Scope)
}

func TestPlanner_SortOps_UnconstrainedSortsLexicographically(t *testing.T) {
	// Operations with no dependency edges between them are ordered by
	// (NS, Name, Kind) so the planner is input-order-invariant. Upstream
	// callers iterating Go maps no longer leak hash-seed randomness into
	// the registry transition stream.
	e1 := newEntry("a", "zzz", "svc")
	e2 := newEntry("a", "aaa", "svc")

	ops := []ScopedOp{
		{Operation: newOp(registry.EntryCreate, e1), Scope: registry.ScopeHistory},
		{Operation: newOp(registry.EntryCreate, e2), Scope: registry.ScopeHistory},
	}

	p := NewPlanner(nil, nil, zap.NewNop())
	result, err := p.SortOps(nil, ops)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, e2.ID, result[0].Operation.Entry.ID)
	assert.Equal(t, e1.ID, result[1].Operation.Entry.ID)
}

// --- PrepareEffects ---

func TestPlanner_PrepareEffects_Empty(t *testing.T) {
	p := NewPlanner(nil, nil, zap.NewNop())
	prepared, err := p.PrepareEffects(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, prepared)
}

func TestPlanner_PrepareEffects_Success(t *testing.T) {
	eff1 := &testEffect{}
	eff2 := &testEffect{}
	p := NewPlanner(nil, nil, zap.NewNop())

	prepared, err := p.PrepareEffects(context.Background(), []registry.Effect{eff1, eff2})
	require.NoError(t, err)
	assert.Len(t, prepared, 2)
	assert.Equal(t, 1, eff1.prepareCall)
	assert.Equal(t, 1, eff2.prepareCall)
}

func TestPlanner_PrepareEffects_PartialFailure(t *testing.T) {
	eff1 := &testEffect{}
	eff2 := &testEffect{prepareErr: errors.New("fail")}
	eff3 := &testEffect{}
	p := NewPlanner(nil, nil, zap.NewNop())

	prepared, err := p.PrepareEffects(context.Background(), []registry.Effect{eff1, eff2, eff3})
	require.Error(t, err)
	assert.Len(t, prepared, 1)
	assert.Equal(t, 1, eff1.prepareCall)
	assert.Equal(t, 1, eff2.prepareCall)
	assert.Equal(t, 0, eff3.prepareCall)
}

// --- CommitEffects ---

func TestPlanner_CommitEffects_Empty(t *testing.T) {
	p := NewPlanner(nil, nil, zap.NewNop())
	err := p.CommitEffects(context.Background(), nil)
	require.NoError(t, err)
}

func TestPlanner_CommitEffects_Success(t *testing.T) {
	eff1 := &testEffect{}
	eff2 := &testEffect{}
	p := NewPlanner(nil, nil, zap.NewNop())

	err := p.CommitEffects(context.Background(), []registry.Effect{eff1, eff2})
	require.NoError(t, err)
	assert.Equal(t, 1, eff1.commitCall)
	assert.Equal(t, 1, eff2.commitCall)
}

func TestPlanner_CommitEffects_StopsOnError(t *testing.T) {
	eff1 := &testEffect{commitErr: errors.New("fail")}
	eff2 := &testEffect{}
	p := NewPlanner(nil, nil, zap.NewNop())

	err := p.CommitEffects(context.Background(), []registry.Effect{eff1, eff2})
	require.Error(t, err)
	assert.Equal(t, 1, eff1.commitCall)
	assert.Equal(t, 0, eff2.commitCall)
}

// --- RollbackEffects ---

func TestPlanner_RollbackEffects_ReverseOrder(t *testing.T) {
	order := make([]int, 0, 3)
	eff1 := &orderEffect{order: &order, id: 1}
	eff2 := &orderEffect{order: &order, id: 2}
	eff3 := &orderEffect{order: &order, id: 3}
	p := NewPlanner(nil, nil, zap.NewNop())

	p.RollbackEffects(context.Background(), []registry.Effect{eff1, eff2, eff3})
	assert.Equal(t, []int{3, 2, 1}, order)
}

func TestPlanner_RollbackEffects_ContinuesOnError(t *testing.T) {
	eff1 := &testEffect{}
	eff2 := &failingRollbackEffect{}
	eff3 := &testEffect{}
	p := NewPlanner(nil, nil, zap.NewNop())

	p.RollbackEffects(context.Background(), []registry.Effect{eff1, eff2, eff3})
	assert.Equal(t, 1, eff1.rollbackCall)
	assert.Equal(t, 1, eff3.rollbackCall)
}

func TestPlanner_RollbackEffects_Empty(t *testing.T) {
	p := NewPlanner(nil, nil, zap.NewNop())
	p.RollbackEffects(context.Background(), nil)
}

// --- helpers ---

type testEffect struct {
	prepareErr   error
	commitErr    error
	prepareCall  int
	commitCall   int
	rollbackCall int
}

func (e *testEffect) Prepare(context.Context) error {
	e.prepareCall++
	return e.prepareErr
}

func (e *testEffect) Commit(context.Context) error {
	e.commitCall++
	return e.commitErr
}

func (e *testEffect) Rollback(context.Context) error {
	e.rollbackCall++
	return nil
}

type orderEffect struct {
	order *[]int
	id    int
}

func (e *orderEffect) Prepare(context.Context) error { return nil }
func (e *orderEffect) Commit(context.Context) error  { return nil }
func (e *orderEffect) Rollback(context.Context) error {
	*e.order = append(*e.order, e.id)
	return nil
}

type failingRollbackEffect struct{}

func (e *failingRollbackEffect) Prepare(context.Context) error { return nil }
func (e *failingRollbackEffect) Commit(context.Context) error  { return nil }
func (e *failingRollbackEffect) Rollback(context.Context) error {
	return errors.New("rollback failed")
}
