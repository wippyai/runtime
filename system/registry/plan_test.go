// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// planTestEffect records which phases ran and reports a target the test may
// change between plan and apply to model external drift.
type planTestEffect struct {
	target    atomic.Pointer[string]
	prepared  atomic.Int32
	committed atomic.Int32
	rolled    atomic.Int32
}

func newPlanTestEffect(target string) *planTestEffect {
	e := &planTestEffect{}
	e.target.Store(&target)
	return e
}

func (e *planTestEffect) Prepare(context.Context) error  { e.prepared.Add(1); return nil }
func (e *planTestEffect) Commit(context.Context) error   { e.committed.Add(1); return nil }
func (e *planTestEffect) Rollback(context.Context) error { e.rolled.Add(1); return nil }
func (e *planTestEffect) Target() (regapi.EffectTarget, error) {
	return regapi.EffectTarget{Kind: "test.effect", Digest: *e.target.Load()}, nil
}

// planTestDirective expands one declaration into a derived entry plus an effect,
// the shape a Hub dependency takes.
type planTestDirective struct {
	effect *planTestEffect
}

func (d planTestDirective) Expand(_ context.Context, op regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
	if op.Kind != regapi.EntryCreate {
		return regapi.DirectiveResult{}, nil
	}
	derived := regapi.Entry{
		ID:   regapi.NewID(op.Entry.ID.NS, op.Entry.ID.Name+".derived"),
		Kind: "derived",
		Data: payload.New("expanded from " + op.Entry.ID.String()),
	}
	return regapi.DirectiveResult{
		Additional: []regapi.ScopedOperation{{
			Operation: regapi.Operation{Kind: regapi.EntryCreate, Entry: derived},
			Scope:     regapi.ScopeHistory,
		}},
		Effects: []regapi.Effect{d.effect},
		Applied: true,
	}, nil
}

func newPlanTestRegistry(t *testing.T, effect *planTestEffect) (*Reg, *TestRunner) {
	t.Helper()
	history := historymem.New()
	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(zap.NewNop(), resolver)
	runner := NewTestRunner()
	reg := NewRegistry(history, runner, builder, resolver, zap.NewNop(),
		WithKindDirective("declaration", planTestDirective{effect: effect}))
	require.NoError(t, reg.LoadState(context.Background(), nil, version.FromParent(nil, regapi.RootVersion)))
	return reg, runner
}

func declarationCreate(name string) regapi.ChangeSet {
	return regapi.ChangeSet{{
		Kind:  regapi.EntryCreate,
		Entry: regapi.Entry{ID: regapi.NewID("app", name), Kind: "declaration", Data: payload.New("declared")},
	}}
}

func TestPlanExpandsWithoutApplying(t *testing.T) {
	effect := newPlanTestEffect("artifact-v1")
	reg, runner := newPlanTestRegistry(t, effect)
	base, err := reg.Current()
	require.NoError(t, err)

	plan, err := reg.Plan(context.Background(), base, declarationCreate("crm"))
	require.NoError(t, err)

	require.Equal(t, base.ID(), plan.Base.ID())
	require.Len(t, plan.Requested, 1)
	require.Len(t, plan.Changes, 2, "the plan carries the declaration and the derived entry")
	require.Len(t, plan.History, 2)
	require.Equal(t, []regapi.EffectTarget{{Kind: "test.effect", Digest: "artifact-v1"}}, plan.Effects)
	require.NotEmpty(t, plan.Digest)

	assert.Empty(t, runner.transitions, "planning must not transition the registry")
	_, getErr := reg.GetEntry(regapi.NewID("app", "crm"))
	require.Error(t, getErr, "planning must not create entries")
	current, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, base.ID(), current.ID(), "planning must not advance the version")

	assert.Equal(t, int32(0), effect.prepared.Load(), "planning must not prepare effects")
	assert.Equal(t, int32(0), effect.committed.Load())
	assert.Equal(t, int32(1), effect.rolled.Load(), "planning must release staged resources")
}

func TestPlanIsDeterministic(t *testing.T) {
	effect := newPlanTestEffect("artifact-v1")
	reg, _ := newPlanTestRegistry(t, effect)
	base, err := reg.Current()
	require.NoError(t, err)

	first, err := reg.Plan(context.Background(), base, declarationCreate("crm"))
	require.NoError(t, err)
	second, err := reg.Plan(context.Background(), base, declarationCreate("crm"))
	require.NoError(t, err)
	require.Equal(t, first.Digest, second.Digest)

	other, err := reg.Plan(context.Background(), base, declarationCreate("billing"))
	require.NoError(t, err)
	require.NotEqual(t, first.Digest, other.Digest)
}

func TestPlanRefusesStaleBase(t *testing.T) {
	reg, _ := newPlanTestRegistry(t, newPlanTestEffect("artifact-v1"))
	stale, err := reg.Current()
	require.NoError(t, err)
	_, err = reg.Apply(context.Background(), declarationCreate("first"))
	require.NoError(t, err)

	_, err = reg.Plan(context.Background(), stale, declarationCreate("second"))
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
}

func TestApplyPlanExecutesTheBoundPlan(t *testing.T) {
	effect := newPlanTestEffect("artifact-v1")
	reg, _ := newPlanTestRegistry(t, effect)
	base, err := reg.Current()
	require.NoError(t, err)
	plan, err := reg.Plan(context.Background(), base, declarationCreate("crm"))
	require.NoError(t, err)

	applied, err := reg.ApplyPlan(context.Background(), plan)
	require.NoError(t, err)
	require.NotEqual(t, base.ID(), applied.ID())

	_, err = reg.GetEntry(regapi.NewID("app", "crm"))
	require.NoError(t, err)
	_, err = reg.GetEntry(regapi.NewID("app", "crm.derived"))
	require.NoError(t, err, "the derived entry from expansion is applied")
	assert.Equal(t, int32(1), effect.prepared.Load())
	assert.Equal(t, int32(1), effect.committed.Load())
}

func TestApplyPlanRefusesRegistryDrift(t *testing.T) {
	reg, _ := newPlanTestRegistry(t, newPlanTestEffect("artifact-v1"))
	base, err := reg.Current()
	require.NoError(t, err)
	plan, err := reg.Plan(context.Background(), base, declarationCreate("crm"))
	require.NoError(t, err)

	_, err = reg.Apply(context.Background(), declarationCreate("unrelated"))
	require.NoError(t, err)

	_, err = reg.ApplyPlan(context.Background(), plan)
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
	_, getErr := reg.GetEntry(regapi.NewID("app", "crm"))
	require.Error(t, getErr, "a refused plan applies nothing")
}

func TestApplyPlanRefusesExternalDrift(t *testing.T) {
	effect := newPlanTestEffect("artifact-v1")
	reg, _ := newPlanTestRegistry(t, effect)
	base, err := reg.Current()
	require.NoError(t, err)
	plan, err := reg.Plan(context.Background(), base, declarationCreate("crm"))
	require.NoError(t, err)

	changed := "artifact-v2"
	effect.target.Store(&changed)

	_, err = reg.ApplyPlan(context.Background(), plan)
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
	assert.Equal(t, int32(0), effect.committed.Load(), "drift is detected before any effect commits")
	_, getErr := reg.GetEntry(regapi.NewID("app", "crm"))
	require.Error(t, getErr)
}

func TestApplyPlanRejectsPlanWithoutBase(t *testing.T) {
	reg, _ := newPlanTestRegistry(t, newPlanTestEffect("artifact-v1"))
	_, err := reg.ApplyPlan(context.Background(), &regapi.Plan{Requested: declarationCreate("crm")})
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Invalid, structured.Kind())
}

func TestApplyAtAppliesOnTheExpectedBase(t *testing.T) {
	effect := newPlanTestEffect("artifact-v1")
	reg, _ := newPlanTestRegistry(t, effect)
	base, err := reg.Current()
	require.NoError(t, err)

	applied, err := reg.ApplyAt(context.Background(), base, declarationCreate("crm"))
	require.NoError(t, err)
	require.NotEqual(t, base.ID(), applied.ID())
	_, err = reg.GetEntry(regapi.NewID("app", "crm.derived"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), effect.committed.Load())
}

func TestApplyAtRefusesStaleBase(t *testing.T) {
	effect := newPlanTestEffect("artifact-v1")
	reg, _ := newPlanTestRegistry(t, effect)
	stale, err := reg.Current()
	require.NoError(t, err)
	_, err = reg.Apply(context.Background(), declarationCreate("first"))
	require.NoError(t, err)

	_, err = reg.ApplyAt(context.Background(), stale, declarationCreate("second"))
	require.Error(t, err)
	var structured apierror.Error
	require.ErrorAs(t, err, &structured)
	assert.Equal(t, apierror.Conflict, structured.Kind())
	_, getErr := reg.GetEntry(regapi.NewID("app", "second"))
	require.Error(t, getErr, "a refused apply changes nothing")
	assert.Equal(t, int32(1), effect.committed.Load(), "only the first apply committed its effect")
}

func TestApplyAtRequiresBase(t *testing.T) {
	reg, _ := newPlanTestRegistry(t, newPlanTestEffect("artifact-v1"))
	_, err := reg.ApplyAt(context.Background(), nil, declarationCreate("crm"))
	require.ErrorIs(t, err, ErrPlanBaseRequired)
}
