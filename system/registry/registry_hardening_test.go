// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

type hardeningHistory struct {
	*memory.Storage
	failSetHeadFor uint
	failCheckpoint bool
}

// historyWithoutResolution deliberately exposes only the base History method
// set even when the wrapped backend supports richer optional capabilities.
type historyWithoutResolution struct {
	regapi.History
}

type lookupHistory struct {
	*memory.Storage
	versionsCalled bool
}

func (h *lookupHistory) Versions() ([]regapi.Version, error) {
	h.versionsCalled = true
	return nil, errors.New("full history enumeration must not be used")
}

func (h *hardeningHistory) SetHead(v regapi.Version) error {
	if h.failSetHeadFor != 0 && h.failSetHeadFor == v.ID() {
		h.failSetHeadFor = 0
		return errors.New("injected set-head failure")
	}
	return h.Storage.SetHead(v)
}

func (h *hardeningHistory) CompareAndSetHead(expected, target regapi.Version) error {
	if h.failSetHeadFor != 0 && h.failSetHeadFor == target.ID() {
		h.failSetHeadFor = 0
		return errors.New("injected set-head failure")
	}
	return h.Storage.CompareAndSetHead(expected, target)
}

func (h *hardeningHistory) CheckpointDependencyResolution(v regapi.Version, resolution *regapi.DependencyResolution) error {
	if h.failCheckpoint {
		return errors.New("injected checkpoint failure")
	}
	return h.Storage.CheckpointDependencyResolution(v, resolution)
}

func (h *hardeningHistory) CompareAndSetHeadWithDependencyResolution(expected, target regapi.Version, resolution *regapi.DependencyResolution) error {
	if h.failCheckpoint {
		return errors.New("injected checkpoint failure")
	}
	if h.failSetHeadFor != 0 && h.failSetHeadFor == target.ID() {
		h.failSetHeadFor = 0
		return errors.New("injected set-head failure")
	}
	return h.Storage.CompareAndSetHeadWithDependencyResolution(expected, target, resolution)
}

type hardeningRunner struct {
	transitions      int
	failTransitionAt int
}

func (r *hardeningRunner) Transition(_ context.Context, state regapi.State, changes regapi.ChangeSet) (regapi.State, error) {
	r.transitions++
	if r.failTransitionAt == r.transitions {
		return state, errors.New("injected transition failure")
	}
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

type hardeningEffect struct {
	prepared   int
	committed  int
	rolledBack int
}

func (e *hardeningEffect) Prepare(context.Context) error {
	e.prepared++
	return nil
}

func (e *hardeningEffect) Commit(context.Context) error {
	e.committed++
	return nil
}

func (e *hardeningEffect) Rollback(context.Context) error {
	e.rolledBack++
	return nil
}

type hardeningDirective struct {
	expand    func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error)
	reconcile func(context.Context, regapi.State, regapi.State, *regapi.DependencyResolution) (regapi.DirectiveResult, error)
}

func (d hardeningDirective) Expand(ctx context.Context, op regapi.Operation, state regapi.State) (regapi.DirectiveResult, error) {
	if d.expand == nil {
		return regapi.DirectiveResult{}, nil
	}
	return d.expand(ctx, op, state)
}

func (d hardeningDirective) ReconcileResolution(ctx context.Context, state regapi.State, resolution *regapi.DependencyResolution) (regapi.DirectiveResult, error) {
	if d.reconcile == nil {
		return regapi.DirectiveResult{}, nil
	}
	return d.reconcile(ctx, state, state, resolution)
}

func (d hardeningDirective) ReconcileResolutionTransition(ctx context.Context, current regapi.State, target regapi.State, resolution *regapi.DependencyResolution) (regapi.DirectiveResult, error) {
	if d.reconcile == nil {
		return regapi.DirectiveResult{}, nil
	}
	return d.reconcile(ctx, current, target, resolution)
}

func hardeningResolution() *regapi.DependencyResolution {
	return (&regapi.DependencyResolution{
		InputDigest: "legacy-roots",
		Modules:     []regapi.ResolvedModule{{Name: "acme/module", Version: "v1.0.0", Digest: "sha256:test"}},
	}).Canonical()
}

func TestLoadStateDefaultsDependencyAccessToVerifiedOffline(t *testing.T) {
	for _, test := range []struct {
		ctx  context.Context
		name string
		want regapi.DependencyAccess
	}{
		{name: "default restore", ctx: context.Background(), want: regapi.DependencyAccessVerifiedOffline},
		{
			name: "explicit online migration",
			ctx:  regapi.WithDependencyAccess(context.Background(), regapi.DependencyAccessOnline),
			want: regapi.DependencyAccessOnline,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen := regapi.DependencyAccessUnspecified
			directive := hardeningDirective{expand: func(ctx context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
				seen = regapi.DependencyAccessFromContext(ctx)
				return regapi.DirectiveResult{}, nil
			}}
			reg := NewRegistry(memory.New(), &hardeningRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
				WithKindDirective(regapi.NamespaceDependency, directive))
			dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
			require.NoError(t, reg.LoadState(test.ctx, regapi.State{dep}, version.New(0)))
			require.Equal(t, test.want, seen)
		})
	}
}

func TestApplyVersion_SetHeadFailureCompensatesRuntimeAndEffects(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	history := &hardeningHistory{Storage: memory.New(), failSetHeadFor: v1.ID()}
	require.NoError(t, history.Save(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: dep}}, false))
	require.NoError(t, history.SetHead(v0))

	effect := &hardeningEffect{}
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution(), Effects: []regapi.Effect{effect}}, nil
	}}
	runner := &hardeningRunner{}
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))
	require.NoError(t, reg.LoadState(ctx, nil, v0))

	err := reg.ApplyVersion(ctx, v1)
	require.ErrorContains(t, err, "failed to set head version")
	_, getErr := reg.GetEntry(dep.ID)
	require.Error(t, getErr)
	head, headErr := history.Head()
	require.NoError(t, headErr)
	require.Equal(t, v0.ID(), head.ID())
	_, resolutionErr := history.GetDependencyResolution(v1)
	require.ErrorIs(t, resolutionErr, regapi.ErrDependencyResolutionNotFound,
		"a failed atomic head update must not annotate the target graph")
	require.Equal(t, 2, runner.transitions, "target transition must be compensated")
	require.Equal(t, 1, effect.committed)
	require.Equal(t, 1, effect.rolledBack)
}

func TestApplyVersionNoOpRejectsChangedDurableHead(t *testing.T) {
	ctx := context.Background()
	history := memory.New()
	reg := NewRegistry(history, &hardeningRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop())
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{
		Kind:  regapi.EntryCreate,
		Entry: regapi.Entry{ID: regapi.NewID("app", "one"), Kind: "service"},
	}})
	require.NoError(t, err)
	require.NoError(t, history.SetHead(version.New(0)), "simulate a concurrent runtime moving durable head")

	err = reg.ApplyVersion(ctx, v1)
	require.ErrorContains(t, err, "history head changed")
	current, currentErr := reg.Current()
	require.NoError(t, currentErr)
	require.Equal(t, v1.ID(), current.ID(), "failed validation must not mutate local state")
}

func TestApplyVersion_LegacyResolutionCheckpointFailureCompensates(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	history := &hardeningHistory{Storage: memory.New(), failCheckpoint: true}
	require.NoError(t, history.Save(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: dep}}, false))
	require.NoError(t, history.SetHead(v0))

	effect := &hardeningEffect{}
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution(), Effects: []regapi.Effect{effect}}, nil
	}}
	runner := &hardeningRunner{}
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))
	require.NoError(t, reg.LoadState(ctx, nil, v0))

	err := reg.ApplyVersion(ctx, v1)
	require.ErrorContains(t, err, "injected checkpoint failure")
	_, getErr := reg.GetEntry(dep.ID)
	require.Error(t, getErr)
	head, headErr := history.Head()
	require.NoError(t, headErr)
	require.Equal(t, v0.ID(), head.ID())
	require.Equal(t, 2, runner.transitions)
	require.Equal(t, 1, effect.rolledBack)
}

func TestApplyVersion_LegacyResolutionIsCheckpointed(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	history := &hardeningHistory{Storage: memory.New()}
	require.NoError(t, history.Save(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: dep}}, false))
	require.NoError(t, history.SetHead(v0))
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution()}, nil
	}}
	reg := NewRegistry(history, &hardeningRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))
	require.NoError(t, reg.LoadState(ctx, nil, v0))
	require.NoError(t, reg.ApplyVersion(ctx, v1))
	stored, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, hardeningResolution().Digest, stored.Digest)
}

func TestApplyVersionRejectsExactResolutionWithoutDurableHistorySupport(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	durable := memory.New()
	require.NoError(t, durable.Save(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: dep}}, false))
	require.NoError(t, durable.SetHead(v0))
	history := &historyWithoutResolution{History: durable}
	effect := &hardeningEffect{}
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution(), Effects: []regapi.Effect{effect}}, nil
	}}
	runner := &hardeningRunner{}
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))
	require.NoError(t, reg.LoadState(ctx, nil, v0))

	err := reg.ApplyVersion(ctx, v1)
	require.ErrorContains(t, err, "history does not support durable dependency resolutions")
	head, headErr := durable.Head()
	require.NoError(t, headErr)
	require.Equal(t, v0.ID(), head.ID())
	_, getErr := reg.GetEntry(dep.ID)
	require.Error(t, getErr)
	require.Zero(t, runner.transitions, "capability failure must happen before runtime transition")
	require.Equal(t, 1, effect.prepared)
	require.Equal(t, 1, effect.rolledBack)
}

func TestApplyVersion_LegacyUnrelatedTransitionResolvesFinalRoots(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	unrelated := regapi.Entry{ID: regapi.NewID("app", "setting"), Kind: "setting"}
	history := &hardeningHistory{Storage: memory.New()}
	require.NoError(t, history.Save(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: unrelated}}, false))
	require.NoError(t, history.SetHead(v0))
	expansions := 0
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		expansions++
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution()}, nil
	}}
	reg := NewRegistry(history, &hardeningRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))
	require.NoError(t, reg.LoadState(ctx, regapi.State{dep}, v0))
	expansions = 0

	require.NoError(t, reg.ApplyVersion(ctx, v1))
	require.Equal(t, 1, expansions, "the final dependency root graph must be resolved once")
	stored, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, hardeningResolution().Digest, stored.Digest)
}

func TestLoadState_CheckpointFailureCompensatesRuntimeAndEffects(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	history := &hardeningHistory{Storage: memory.New(), failCheckpoint: true}
	effect := &hardeningEffect{}
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution(), Effects: []regapi.Effect{effect}}, nil
	}}
	runner := &hardeningRunner{}
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))

	err := reg.LoadState(ctx, regapi.State{dep}, v0)
	require.ErrorContains(t, err, "injected checkpoint failure")
	entries, getErr := reg.GetAllEntries()
	require.NoError(t, getErr)
	require.Empty(t, entries)
	require.Equal(t, 2, runner.transitions)
	require.Equal(t, 1, effect.committed)
	require.Equal(t, 1, effect.rolledBack)
}

func TestLoadStateRejectsExactResolutionWithoutDurableHistorySupport(t *testing.T) {
	ctx := context.Background()
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	durable := memory.New()
	history := &historyWithoutResolution{History: durable}
	effect := &hardeningEffect{}
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution(), Effects: []regapi.Effect{effect}}, nil
	}}
	runner := &hardeningRunner{}
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))

	err := reg.LoadState(ctx, regapi.State{dep}, version.New(0))
	require.ErrorContains(t, err, "history does not support durable dependency resolutions")
	entries, getErr := reg.GetAllEntries()
	require.NoError(t, getErr)
	require.Empty(t, entries)
	require.Zero(t, runner.transitions, "capability failure must happen before runtime transition")
	require.Equal(t, 1, effect.prepared)
	require.Equal(t, 1, effect.rolledBack)
}

func TestLoadState_TransitionFailureDoesNotCheckpointLegacyResolution(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	history := &hardeningHistory{Storage: memory.New()}
	effect := &hardeningEffect{}
	directive := hardeningDirective{expand: func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Resolution: hardeningResolution(), Effects: []regapi.Effect{effect}}, nil
	}}
	reg := NewRegistry(history, &hardeningRunner{failTransitionAt: 1}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, directive))

	err := reg.LoadState(ctx, regapi.State{dep}, v0)
	require.ErrorContains(t, err, "injected transition failure")
	_, resolutionErr := history.GetDependencyResolution(v0)
	require.ErrorIs(t, resolutionErr, regapi.ErrDependencyResolutionNotFound)
	require.Equal(t, 0, effect.committed)
	require.Equal(t, 1, effect.rolledBack)
}

func TestApplyVersion_ReconcilerErrorRollsBackPreviouslyPreparedEffects(t *testing.T) {
	ctx := context.Background()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	dep := regapi.Entry{ID: regapi.NewID("app.deps", "module"), Kind: regapi.NamespaceDependency}
	history := memory.New()
	require.NoError(t, history.SaveWithDependencyResolution(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: dep}}, hardeningResolution(), false))
	effect := &hardeningEffect{}
	first := hardeningDirective{reconcile: func(context.Context, regapi.State, regapi.State, *regapi.DependencyResolution) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: true, Effects: []regapi.Effect{effect}}, nil
	}}
	second := hardeningDirective{reconcile: func(context.Context, regapi.State, regapi.State, *regapi.DependencyResolution) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{}, errors.New("injected reconcile failure")
	}}
	reg := NewRegistry(history, &hardeningRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop(),
		WithKindDirective(regapi.NamespaceDependency, first), WithKindDirective(regapi.NamespaceDependency, second))
	require.NoError(t, reg.LoadState(ctx, nil, v0))
	err := reg.ApplyVersion(ctx, v1)
	require.ErrorContains(t, err, "injected reconcile failure")
	require.Equal(t, 1, effect.prepared)
	require.Equal(t, 1, effect.rolledBack)
}

func TestLoadState_AllocatorUsesMaximumStoredVersionAfterRewind(t *testing.T) {
	ctx := context.Background()
	history := memory.New()
	runner := &hardeningRunner{}
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop())

	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "one"), Kind: "service"}}})
	require.NoError(t, err)
	v2, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "two"), Kind: "service"}}})
	require.NoError(t, err)
	v3, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "three"), Kind: "service"}}})
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, v1))

	restored := NewRegistry(history, &hardeningRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop())
	require.NoError(t, restored.LoadState(ctx, nil, v1))
	v4, err := restored.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "branch"), Kind: "service"}}})
	require.NoError(t, err)
	require.Equal(t, uint(4), v4.ID())
	_, err = history.Get(v2)
	require.NoError(t, err)
	_, err = history.Get(v3)
	require.NoError(t, err)
}

func TestApplyVersion_CrossBranchWorksInBothIDDirections(t *testing.T) {
	ctx := context.Background()
	history := memory.New()
	reg := NewRegistry(history, &hardeningRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop())
	anchorID := regapi.NewID("app", "anchor")
	branchAID := regapi.NewID("app", "branch_a")
	branchBID := regapi.NewID("app", "branch_b")

	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: anchorID, Kind: "service", Data: payload.New("v1")}}})
	require.NoError(t, err)
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: branchAID, Kind: "service"}}})
	require.NoError(t, err)
	v3, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: regapi.Entry{ID: anchorID, Kind: "service", Data: payload.New("branch-a")}}})
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, v1))
	v4, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: branchBID, Kind: "service"}}})
	require.NoError(t, err)

	require.NoError(t, reg.ApplyVersion(ctx, v3))
	_, err = reg.GetEntry(branchAID)
	require.NoError(t, err)
	_, err = reg.GetEntry(branchBID)
	require.Error(t, err)

	require.NoError(t, reg.ApplyVersion(ctx, v4))
	_, err = reg.GetEntry(branchAID)
	require.Error(t, err)
	_, err = reg.GetEntry(branchBID)
	require.NoError(t, err)
}

func TestFindStoredVersionUsesTargetLookupAcrossManyBranches(t *testing.T) {
	storage := memory.New()
	root := version.New(regapi.RootVersion)
	const branches = 5000
	for id := uint(1); id <= branches; id++ {
		require.NoError(t, storage.Save(version.FromParent(root, id), nil, false))
	}
	target := version.FromParent(version.FromParent(root, 1), branches+1)
	require.NoError(t, storage.Save(target, nil, false))
	history := &lookupHistory{Storage: storage}
	reg := &Reg{history: history}

	stored, err := reg.findStoredVersion(version.New(branches + 1))
	require.NoError(t, err)
	require.False(t, history.versionsCalled)
	require.Equal(t, uint(branches+1), stored.ID())
	require.Equal(t, uint(1), stored.Previous().ID())
}
