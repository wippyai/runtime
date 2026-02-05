package expansion_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	sysreg "github.com/wippyai/runtime/system/registry"
	sysregexp "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

type expanderFunc func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error)

func (f expanderFunc) Expand(ctx context.Context, op regapi.Operation, snap regapi.State) (regapi.DirectiveResult, error) {
	return f(ctx, op, snap)
}

type testEffect struct {
	prepareErr   error
	commitErr    error
	prepareCall  int
	commitCall   int
	rollbackCall int
}

func (t *testEffect) Prepare(context.Context) error {
	t.prepareCall++
	return t.prepareErr
}

func (t *testEffect) Commit(context.Context) error {
	t.commitCall++
	return t.commitErr
}

func (t *testEffect) Rollback(context.Context) error {
	t.rollbackCall++
	return nil
}

type blockingEffect struct {
	testEffect
	start   chan struct{}
	proceed chan struct{}
}

func (b *blockingEffect) Prepare(context.Context) error {
	b.prepareCall++
	close(b.start)
	<-b.proceed
	return b.prepareErr
}

type applyRunner struct {
	builder         *topology.StateBuilder
	err             error
	lastChangeSet   regapi.ChangeSet
	transitionCalls int
}

func (r *applyRunner) Transition(_ context.Context, state regapi.State, changes regapi.ChangeSet) (regapi.State, error) {
	r.transitionCalls++
	r.lastChangeSet = changes
	if r.err != nil {
		return state, r.err
	}

	stateMap := topology.NewStateMap(state)
	for _, op := range changes {
		var err error
		stateMap, err = r.builder.ApplyOperation(stateMap, op)
		if err != nil {
			return state, err
		}
	}

	return topology.StateMapToSlice(stateMap), nil
}

type failingHistory struct {
	*historymem.Storage
	saveErr   error
	saveCalls int
}

func (f *failingHistory) Save(v regapi.Version, cs regapi.ChangeSet, head bool) error {
	f.saveCalls++
	if f.saveErr != nil {
		return f.saveErr
	}
	return f.Storage.Save(v, cs, head)
}

func TestApplyExpansion_ScopedHistoryAndBaseline(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app", "dep"),
		Kind: "ns.dependency",
		Data: payload.NewString("dep"),
	}
	modEntry := regapi.Entry{
		ID:   regapi.NewID("mod", "svc"),
		Kind: "service",
		Data: payload.NewString("svc"),
	}

	exp := expanderFunc(func(_ context.Context, op regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		if op.Entry.Kind != "ns.dependency" {
			return regapi.DirectiveResult{}, nil
		}
		return regapi.DirectiveResult{
			Applied: true,
			Additional: []regapi.ScopedOperation{
				{
					Operation: regapi.Operation{Kind: regapi.EntryCreate, Entry: modEntry},
					Scope:     regapi.ScopeBaseline,
				},
			},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	version, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: depEntry},
	})
	require.NoError(t, err)
	require.NotNil(t, version)
	assert.Equal(t, uint(1), version.ID())

	assert.Len(t, runner.lastChangeSet, 2)

	_, err = reg.GetEntry(depEntry.ID)
	require.NoError(t, err)
	_, err = reg.GetEntry(modEntry.ID)
	require.NoError(t, err)

	cs, err := hist.Get(version)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "ns.dependency", cs[0].Entry.Kind)
}

func TestApplyExpansion_BaselineOnly_NoHistory(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	entry := regapi.Entry{
		ID:   regapi.NewID("app", "baseline"),
		Kind: "baseline.only",
		Data: payload.NewString("x"),
	}

	scope := regapi.ScopeBaseline
	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied:       true,
			OriginalScope: &scope,
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("baseline.only", exp),
	)

	version, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: entry},
	})
	require.NoError(t, err)
	assert.Equal(t, uint(0), version.ID())

	_, err = reg.GetEntry(entry.ID)
	require.NoError(t, err)

	_, headErr := hist.Head()
	assert.Error(t, headErr)
}

func TestApplyExpansion_PrepareFailure_RollsBackPreparedEffects(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	eff1 := &testEffect{}
	eff2 := &testEffect{prepareErr: errors.New("prepare failed")}

	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Effects: []regapi.Effect{eff1, eff2},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}},
	})
	require.Error(t, err)
	assert.Equal(t, 0, runner.transitionCalls)
	assert.Equal(t, 1, eff1.rollbackCall)
	assert.Equal(t, 0, eff2.rollbackCall)
}

func TestApplyExpansion_ConcurrentApply_Serializes(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	block := &blockingEffect{start: make(chan struct{}), proceed: make(chan struct{})}
	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Effects: []regapi.Effect{block},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	errCh := make(chan error, 1)
	go func() {
		_, err := reg.Apply(context.Background(), regapi.ChangeSet{
			{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}},
		})
		errCh <- err
	}()

	<-block.start

	secondErr := make(chan error, 1)
	entered := make(chan struct{})
	go func() {
		close(entered)
		_, err := reg.Apply(context.Background(), regapi.ChangeSet{
			{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "other"), Kind: "service"}},
		})
		secondErr <- err
	}()

	<-entered

	select {
	case err := <-secondErr:
		t.Fatalf("expected second apply to wait, got err=%v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(block.proceed)

	err := <-errCh
	require.NoError(t, err)

	err = <-secondErr
	require.NoError(t, err)

	assert.Equal(t, 1, block.commitCall)
	assert.Equal(t, 0, block.rollbackCall)
}

func TestApplyExpansion_TransitionFailure_RollsBackEffects(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder, err: errors.New("transition failed")}
	hist := historymem.New()

	eff := &testEffect{}
	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Effects: []regapi.Effect{eff},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}},
	})
	require.Error(t, err)
	assert.Equal(t, 1, eff.rollbackCall)
}

func TestApplyExpansion_DirectiveError_StopsApply(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{}, errors.New("directive failed")
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}},
	})
	require.Error(t, err)
	assert.Equal(t, 0, runner.transitionCalls)
}

func TestApplyExpansion_HistorySaveFailure_RollsBackStateAndEffects(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := &failingHistory{Storage: historymem.New(), saveErr: errors.New("save failed")}

	eff := &testEffect{}
	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Effects: []regapi.Effect{eff},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	entry := regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: entry},
	})
	require.Error(t, err)
	assert.Equal(t, 1, eff.rollbackCall)

	_, getErr := reg.GetEntry(entry.ID)
	assert.Error(t, getErr)
}

func TestApplyExpansion_CommitFailure_RollsBackStateAndEffects_NoHistorySave(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := &failingHistory{Storage: historymem.New()}

	eff := &testEffect{commitErr: errors.New("commit failed")}
	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Effects: []regapi.Effect{eff},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	entry := regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: entry},
	})
	require.Error(t, err)

	assert.Equal(t, 1, eff.rollbackCall)
	assert.Equal(t, 2, runner.transitionCalls)
	assert.Equal(t, 0, hist.saveCalls)

	_, getErr := reg.GetEntry(entry.ID)
	assert.Error(t, getErr)
}

func TestApplyExpansion_DisallowAdditionalOpsOnOriginalEntry(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	entry := regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}

	exp := expanderFunc(func(_ context.Context, op regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Additional: []regapi.ScopedOperation{
				{
					Operation: regapi.Operation{Kind: regapi.EntryUpdate, Entry: op.Entry},
					Scope:     regapi.ScopeBaseline,
				},
			},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: entry},
	})
	require.Error(t, err)
	assert.Equal(t, 0, runner.transitionCalls)
}

func TestApplyExpansion_DisallowDuplicateAdditionalIDsAcrossDirectives(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	shared := regapi.Entry{ID: regapi.NewID("mod", "svc"), Kind: "service"}
	entry := regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}

	exp1 := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Additional: []regapi.ScopedOperation{
				{Operation: regapi.Operation{Kind: regapi.EntryCreate, Entry: shared}, Scope: regapi.ScopeBaseline},
			},
		}, nil
	})
	exp2 := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Additional: []regapi.ScopedOperation{
				{Operation: regapi.Operation{Kind: regapi.EntryUpdate, Entry: shared}, Scope: regapi.ScopeBaseline},
			},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp1),
		sysreg.WithKindDirective("ns.dependency", exp2),
	)

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: entry},
	})
	require.Error(t, err)
	assert.Equal(t, 0, runner.transitionCalls)
}

func TestApplyExpansion_DisallowDuplicateAdditionalIDsWithinDirective(t *testing.T) {
	builder := topology.NewStateBuilder(zap.NewNop(), topology.NewResolver())
	runner := &applyRunner{builder: builder}
	hist := historymem.New()

	shared := regapi.Entry{ID: regapi.NewID("mod", "svc"), Kind: "service"}
	entry := regapi.Entry{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"}

	exp := expanderFunc(func(_ context.Context, _ regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied: true,
			Additional: []regapi.ScopedOperation{
				{Operation: regapi.Operation{Kind: regapi.EntryCreate, Entry: shared}, Scope: regapi.ScopeBaseline},
				{Operation: regapi.Operation{Kind: regapi.EntryUpdate, Entry: shared}, Scope: regapi.ScopeBaseline},
			},
		}, nil
	})

	reg := sysreg.NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		sysreg.WithKindDirective("ns.dependency", exp),
	)

	_, err := reg.Apply(context.Background(), regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: entry},
	})
	require.Error(t, err)
	assert.Equal(t, 0, runner.transitionCalls)
}

func TestDependencyDirective_NoHandler(t *testing.T) {
	dir := &sysregexp.DependencyDirective{}
	res, err := dir.Expand(context.Background(), regapi.Operation{}, nil)
	require.NoError(t, err)
	assert.False(t, res.Applied)
}

func TestDependencyDirective_NotApplied(t *testing.T) {
	dir := sysregexp.NewDependencyDirective(func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{Applied: false}, nil
	})

	res, err := dir.Expand(context.Background(), regapi.Operation{}, nil)
	require.NoError(t, err)
	assert.False(t, res.Applied)
}

func TestDependencyDirective_Error(t *testing.T) {
	dir := sysregexp.NewDependencyDirective(func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{}, errors.New("expand failed")
	})

	_, err := dir.Expand(context.Background(), regapi.Operation{}, nil)
	require.Error(t, err)
}

func TestDependencyDirective_Applied(t *testing.T) {
	scope := regapi.ScopeBaseline
	eff := &testEffect{}
	additional := []regapi.ScopedOperation{
		{
			Operation: regapi.Operation{
				Kind:  regapi.EntryCreate,
				Entry: regapi.Entry{ID: regapi.NewID("mod", "svc"), Kind: "service"},
			},
			Scope: regapi.ScopeBaseline,
		},
	}

	dir := sysregexp.NewDependencyDirective(func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
		return regapi.DirectiveResult{
			Applied:       true,
			OriginalScope: &scope,
			Additional:    additional,
			Effects:       []regapi.Effect{eff},
		}, nil
	})

	res, err := dir.Expand(context.Background(), regapi.Operation{}, nil)
	require.NoError(t, err)
	require.True(t, res.Applied)
	require.NotNil(t, res.OriginalScope)
	assert.Equal(t, regapi.ScopeBaseline, *res.OriginalScope)
	assert.Len(t, res.Additional, 1)
	assert.Len(t, res.Effects, 1)
}
