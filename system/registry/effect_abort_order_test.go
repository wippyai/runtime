// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// orderLog records effect and listener steps in the order they happen.
type orderLog struct {
	steps []string
}

func (l *orderLog) add(step string) {
	l.steps = append(l.steps, step)
}

// orderRunner applies changes like a listener-backed runner and records each
// transition; failAt makes that transition fail after calling abort, as the
// Runner contract requires.
type orderRunner struct {
	log         *orderLog
	transitions int
	failAt      int
}

func (r *orderRunner) Transition(ctx context.Context, state regapi.State, changes regapi.ChangeSet, abort func(context.Context)) (regapi.State, error) {
	r.transitions++
	if r.failAt == r.transitions {
		if abort != nil {
			abort(ctx)
		}
		r.log.add("transition failed")
		return state, errors.New("injected transition failure")
	}
	r.log.add("transition")
	stateMap := topology.NewStateMap(state)
	for _, op := range changes {
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			stateMap[op.Entry.ID] = op.Entry
		case regapi.EntryDelete:
			delete(stateMap, op.Entry.ID)
		}
	}
	return topology.StateMapToSlice(stateMap), nil
}

type orderEffect struct {
	log       *orderLog
	commitErr error
}

func (e *orderEffect) Target() (regapi.EffectTarget, error) {
	return regapi.EffectTarget{Kind: "order", Digest: "static"}, nil
}

func (e *orderEffect) Prepare(context.Context) error {
	e.log.add("effect prepare")
	return nil
}

func (e *orderEffect) Commit(context.Context) error {
	e.log.add("effect commit")
	return e.commitErr
}

func (e *orderEffect) Rollback(context.Context) error {
	e.log.add("effect rollback")
	return nil
}

func newOrderRegistry(t *testing.T, history regapi.History, runner *orderRunner, effect *orderEffect) *Reg {
	t.Helper()
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution(), Effects: []regapi.Effect{effect}}, nil
	}}
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))
	require.NoError(t, reg.LoadState(context.Background(), nil, version.New(0)))
	runner.log.steps = nil
	return reg
}

func orderDependency() regapi.Entry {
	return regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
}

// A failed transition withdraws the prepared effects exactly once, through the
// runner's abort, before the runner reverses any accepted operation.
func TestApply_FailedTransitionRollsBackEffectsOnceThroughAbort(t *testing.T) {
	log := &orderLog{}
	runner := &orderRunner{log: log}
	effect := &orderEffect{log: log}
	reg := newOrderRegistry(t, memory.New(), runner, effect)
	runner.failAt = runner.transitions + 1

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: orderDependency()}})
	require.ErrorContains(t, err, "injected transition failure")
	require.Equal(t, []string{"effect prepare", "effect rollback", "transition failed"}, log.steps)
}

// When effects cannot commit after listeners accepted the transition, the
// effects roll back before listener state is reversed, so the reverse
// operations observe the external state that preceded the transition.
func TestApply_CommitEffectsFailureRollsBackEffectsBeforeListeners(t *testing.T) {
	log := &orderLog{}
	runner := &orderRunner{log: log}
	effect := &orderEffect{log: log, commitErr: errors.New("injected commit failure")}
	reg := newOrderRegistry(t, memory.New(), runner, effect)

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: orderDependency()}})
	require.ErrorContains(t, err, "injected commit failure")
	require.Equal(t, []string{"effect prepare", "transition", "effect commit", "effect rollback", "transition"}, log.steps)
	_, getErr := reg.GetEntry(orderDependency().ID)
	require.Error(t, getErr)
}

func TestApplyVersion_SetHeadFailureRollsBackEffectsBeforeListeners(t *testing.T) {
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	history := &hardeningHistory{Storage: memory.New(), failSetHeadFor: v1.ID()}
	require.NoError(t, history.Save(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: orderDependency()}}, false))
	require.NoError(t, history.SetHead(v0))

	log := &orderLog{}
	runner := &orderRunner{log: log}
	effect := &orderEffect{log: log}
	reg := newOrderRegistry(t, history, runner, effect)

	err := reg.ApplyVersion(context.Background(), v1)
	require.ErrorContains(t, err, "failed to set head version")
	require.Equal(t, []string{"effect prepare", "transition", "effect commit", "effect rollback", "transition"}, log.steps)
}

func TestLoadState_CommitEffectsFailureRollsBackEffectsBeforeListeners(t *testing.T) {
	log := &orderLog{}
	runner := &orderRunner{log: log}
	effect := &orderEffect{log: log}
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Effects: []regapi.Effect{effect}}, nil
	}}
	reg := NewRegistry(memory.New(), runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))
	require.NoError(t, reg.LoadState(context.Background(), regapi.State{{ID: regapi.NewID("app", "base"), Kind: "service"}}, version.New(0)))
	log.steps = nil

	effect.commitErr = errors.New("injected commit failure")
	err := reg.LoadState(context.Background(), regapi.State{orderDependency()}, version.New(0))
	require.ErrorContains(t, err, "injected commit failure")
	require.Equal(t, []string{"effect prepare", "transition", "effect commit", "effect rollback", "transition"}, log.steps)
}
