// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	sysprocess "github.com/wippyai/runtime/system/process"
	"go.uber.org/zap"
)

// recordingStarter is a process.Manager that records starts and
// terminations.
type recordingStarter struct {
	start      *process.Start
	err        error
	terminated []pid.PID
}

func (s *recordingStarter) Start(_ context.Context, start *process.Start) (pid.PID, error) {
	s.start = start
	if s.err != nil {
		return pid.PID{}, s.err
	}
	return pid.PID{Host: start.HostID, UniqID: "eval-1"}, nil
}

func (s *recordingStarter) Terminate(_ context.Context, p pid.PID) error {
	s.terminated = append(s.terminated, p)
	return nil
}

func (s *recordingStarter) Cancel(context.Context, pid.PID, pid.PID, string) error { return nil }

var admitParent = pid.PID{Host: "app:processes", UniqID: "parent"}

// ownerContext is the execution context of an eval's caller: a frame with
// an execution scope of the given kind.
func ownerContext(t *testing.T, kind ...process.ExecutionKind) (context.Context, *process.ExecutionScope) {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	k := process.ExecutionProcess
	if len(kind) > 0 {
		k = kind[0]
	}
	scope := process.NewExecutionScope(ctx, k, &recordingStarter{})
	require.NoError(t, fc.SetMultiple(process.ExecutionScopePair(scope)))
	return ctx, scope
}

const admitSource = `return { main = function(x) return x end }`

func newTestAdmitter(t *testing.T, opts ...AdmitterOption) (*Admitter, *recordingStarter) {
	t.Helper()
	host := NewHost(zap.NewNop(), func() []*luaapi.ModuleDef {
		return []*luaapi.ModuleDef{
			{Name: "json", Class: []string{luaapi.ClassEncoding}},
			{Name: "sql", Class: []string{luaapi.ClassStorage}},
			{Name: "process", Class: []string{luaapi.ClassProcess}},
			{Name: "unclassed", Class: []string{"other"}},
		}
	})
	starter := &recordingStarter{}
	return NewAdmitter(host, starter, opts...), starter
}

func TestAdmitterPolicyValidation(t *testing.T) {
	cases := map[string]apihost.EvalPolicy{
		"unknown compile mode":      {CompileMode: apihost.EvalCompileTyped + 1},
		"denied class":              {AllowClasses: []string{luaapi.ClassStorage}},
		"module path":               {Modules: []string{"app:lib"}},
		"tick budget too large":     {TickBudget: maxEvalTickBudget + 1},
		"max steps too large":       {MaxSteps: maxEvalMaxSteps + 1},
		"spawn without children":    {AllowCommands: []apihost.EvalCommand{apihost.EvalCommandSpawn}},
		"children without spawn":    {MaxChildren: 1},
		"too many children":         {AllowCommands: []apihost.EvalCommand{apihost.EvalCommandSpawn}, MaxChildren: maxEvalMaxChildren + 1},
		"upgrade":                   {AllowCommands: []apihost.EvalCommand{apihost.EvalCommandUpgrade}},
		"send policy":               {SendMode: apihost.EvalSendPolicy},
		"targets without explicit":  {SendTargets: []pid.PID{admitParent}},
		"import without loader":     {Imports: []apihost.EvalImport{{Alias: "lib", Source: registry.NewID("app", "lib")}}},
		"module without class":      {Modules: []string{"unclassed"}},
		"too many modules":          {Modules: make([]string, maxEvalModules+1)},
		"binding shadows module":    {Modules: []string{"json"}, Bindings: []apihost.EvalBinding{{Name: "json", Value: 1}}},
		"binding with invalid name": {Bindings: []apihost.EvalBinding{{Name: "1x", Value: 1}}},
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := newTestAdmitter(t)
			_, err := a.Compile(context.Background(), apihost.EvalCompileSpec{SourceCode: admitSource, Policy: policy})
			require.Error(t, err)
			require.True(t,
				errors.Is(err, apihost.ErrEvalPolicyUnsupported) || errors.Is(err, apihost.ErrEvalBindingInvalid),
				"unexpected error %v", err)
		})
	}

	a, _ := newTestAdmitter(t)
	_, err := a.Compile(context.Background(), apihost.EvalCompileSpec{SourceCode: admitSource, Policy: apihost.EvalPolicy{Modules: []string{"sql"}}})
	require.Error(t, err, "storage modules are forbidden")

	_, err = a.Compile(context.Background(), apihost.EvalCompileSpec{})
	require.ErrorIs(t, err, apihost.ErrEvalSourceRequired)
	_, err = a.Compile(context.Background(), apihost.EvalCompileSpec{SourceCode: string(make([]byte, maxEvalSourceBytes+1))})
	require.ErrorIs(t, err, apihost.ErrEvalSourceTooLarge)
}

func TestAdmitterCachesByCanonicalPolicy(t *testing.T) {
	a, _ := newTestAdmitter(t)
	ctx := context.Background()

	first, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource, Policy: apihost.EvalPolicy{Modules: []string{"json", "process"}}})
	require.NoError(t, err)
	same, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource, Policy: apihost.EvalPolicy{
		Modules:    []string{"process", "json", "json"},
		TickBudget: defaultEvalTickBudget,
	}})
	require.NoError(t, err)
	require.Equal(t, first, same, "equivalent policies share a program")

	other, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource, Policy: apihost.EvalPolicy{Modules: []string{"json"}}})
	require.NoError(t, err)
	require.NotEqual(t, first.Key, other.Key)
	require.Equal(t, 2, len(a.entries))

	evicted, err := a.Evict(ctx, first)
	require.NoError(t, err)
	require.True(t, evicted)
	evicted, err = a.Evict(ctx, first)
	require.NoError(t, err)
	require.False(t, evicted)
}

func TestAdmitterEvictsLeastRecentlyUsed(t *testing.T) {
	a, _ := newTestAdmitter(t, WithProgramCacheSize(2))
	ctx, _ := ownerContext(t)
	compile := func(src string) apihost.EvalProgram {
		p, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: src})
		require.NoError(t, err)
		return p
	}
	first := compile(`return { main = function() return 1 end }`)
	second := compile(`return { main = function() return 2 end }`)
	compile(`return { main = function() return 1 end }`) // refresh first
	compile(`return { main = function() return 3 end }`)

	_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{Program: first, Parent: admitParent})
	require.NoError(t, err, "recently used program stays cached")
	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{Program: second, Parent: admitParent})
	require.ErrorIs(t, err, apihost.ErrEvalProgramNotFound)
}

func TestAdmitterSupersedesProgramWhenImportChanges(t *testing.T) {
	a, _ := newTestAdmitter(t)
	library := `return { value = 1 }`
	a.host.WithImportLoader(func(registry.ID) (string, error) { return library, nil })
	ctx := context.Background()
	policy := apihost.EvalPolicy{Imports: []apihost.EvalImport{{Alias: "lib", Source: registry.NewID("app", "lib")}}}

	old, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource, Policy: policy})
	require.NoError(t, err)
	library = `return { value = 2 }`
	updated, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource, Policy: policy})
	require.NoError(t, err)
	require.NotEqual(t, old.Key, updated.Key, "library change yields a new program")
	require.Equal(t, 1, len(a.entries), "the old generation is superseded")
}

func TestAdmitterSpawnBuildsConfinedProcess(t *testing.T) {
	a, starter := newTestAdmitter(t)
	ctx, _ := ownerContext(t)
	child, err := a.Spawn(ctx, apihost.EvalSpawnSpec{
		SourceCode: admitSource,
		Name:       "worker",
		Parent:     admitParent,
		Policy: apihost.EvalPolicy{
			Modules:       []string{"json"},
			AllowCommands: []apihost.EvalCommand{apihost.EvalCommandSpawn},
			MaxChildren:   2,
			TickBudget:    1000,
			MaxSteps:      50,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "eval-1", child.UniqID)

	start := starter.start
	require.Equal(t, admitParent.Host, start.HostID, "runs on the caller's host by default")
	require.Equal(t, EvalProgramNamespace, start.Source.NS)
	require.NotNil(t, start.Admission)
	require.Equal(t, "main", start.Admission.Meta.Method)
	parent, _ := start.Options.Get(process.ProcessParentKey)
	require.Equal(t, admitParent, parent)
	require.True(t, start.Options.GetBool(process.ProcessLinkKey, false), "linked by default")
	require.True(t, start.Options.GetBool(process.ProcessMonitorKey, false), "monitored by default")
	require.Equal(t, "worker", start.Options.GetString(process.ProcessNameKey, ""))

	frameCtx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, fc.SetMultiple(start.Context...))
	actor, ok := secapi.GetActor(frameCtx)
	require.True(t, ok)
	scope, ok := secapi.GetScope(frameCtx)
	require.True(t, ok)
	require.Equal(t, secapi.Allow, scope.Evaluate(actor, "process.send", "{x|y}", nil), "cap mode leaves sends to the grants")
	require.Equal(t, secapi.Allow, scope.Evaluate(actor, "process.spawn", "app:worker", nil))
	require.Equal(t, secapi.Deny, scope.Evaluate(actor, "process.terminate", admitParent.String(), nil))
	require.Equal(t, secapi.Deny, scope.Evaluate(actor, "process.upgrade", "app:worker", nil), "an eval is never replaced by a registry process")
	require.Equal(t, secapi.Deny, scope.Evaluate(actor, "fs.read", "/", nil), "no ambient authority")
	grants := secapi.GetProcessSendGrants(frameCtx)
	require.NotNil(t, grants)
	require.True(t, grants.Holds(admitParent), "the parent is granted")
	slots, err := process.ChildSlotResolver(frameCtx, attrs.Bag{process.ProcessParentKey: child})
	require.NoError(t, err)
	require.Len(t, slots, 1, "spawns reserve child slots")

	proc, err := start.Admission.Factory()
	require.NoError(t, err)
	proc.Close()
}

func TestAdmitterSpawnLinkModes(t *testing.T) {
	ctx, _ := ownerContext(t)
	a, starter := newTestAdmitter(t)

	_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent, LinkMode: apihost.EvalLinkMonitorOnly})
	require.NoError(t, err)
	require.False(t, starter.start.Options.GetBool(process.ProcessLinkKey, false))
	require.True(t, starter.start.Options.GetBool(process.ProcessMonitorKey, false))

	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent, LinkMode: apihost.EvalLinkDetached})
	require.ErrorIs(t, err, apihost.ErrEvalDetachedDenied)

	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{
		SourceCode: admitSource, Parent: admitParent, LinkMode: apihost.EvalLinkDetached,
		Policy: apihost.EvalPolicy{AllowDetached: true},
	})
	require.NoError(t, err)
	_, hasParent := starter.start.Options.Get(process.ProcessParentKey)
	require.False(t, hasParent, "detached evals have no parent relationship")

	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource})
	require.ErrorIs(t, err, apihost.ErrEvalParentRequired)
}

func TestAdmitterSpawnProgramPolicyMustMatch(t *testing.T) {
	ctx, _ := ownerContext(t)
	a, _ := newTestAdmitter(t)
	program, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource, Policy: apihost.EvalPolicy{Modules: []string{"json"}}})
	require.NoError(t, err)

	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{Program: program, Parent: admitParent})
	require.NoError(t, err, "an empty policy uses the cached one")
	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{Program: program, Parent: admitParent, Policy: apihost.EvalPolicy{Modules: []string{"json"}}})
	require.NoError(t, err, "a matching policy is accepted")
	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{Program: program, Parent: admitParent, Policy: apihost.EvalPolicy{Modules: []string{"process"}}})
	require.ErrorIs(t, err, apihost.ErrEvalPolicyMismatch)
}

func TestAdmitterSpawnHost(t *testing.T) {
	ctx, _ := ownerContext(t)
	a, starter := newTestAdmitter(t, WithSpawnHost("app:evals"))
	_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent})
	require.NoError(t, err)
	require.Equal(t, "app:evals", starter.start.HostID)

	b, failing := newTestAdmitter(t)
	failing.err = sysprocess.NewInvalidHostError(admitParent.Host)
	_, err = b.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent})
	require.ErrorIs(t, err, sysprocess.ErrInvalidHost)
	require.ErrorContains(t, err, "lua.eval.spawn_host", "the error names the setting that fixes it")
}

func TestAdmitterMarksEvalsOwnedUnlessDetached(t *testing.T) {
	a, starter := newTestAdmitter(t)
	ctx, _ := ownerContext(t)
	for _, mode := range []apihost.EvalLinkMode{apihost.EvalLinkRequired, apihost.EvalLinkMonitorOnly} {
		_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent, LinkMode: mode})
		require.NoError(t, err)
		require.True(t, starter.start.Options.GetBool(process.ProcessOwnedKey, false), "mode %d is owned by the caller", mode)
	}

	_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{
		SourceCode: admitSource, Parent: admitParent, LinkMode: apihost.EvalLinkDetached,
		Policy: apihost.EvalPolicy{AllowDetached: true},
	})
	require.NoError(t, err, "a process that is not owned may detach an eval")
	require.False(t, starter.start.Options.GetBool(process.ProcessOwnedKey, false))
}

func TestAdmitterDetachedRequiresUnownedProcessCaller(t *testing.T) {
	a, _ := newTestAdmitter(t)
	detached := apihost.EvalSpawnSpec{
		SourceCode: admitSource, Parent: admitParent, LinkMode: apihost.EvalLinkDetached,
		Policy: apihost.EvalPolicy{AllowDetached: true},
	}

	fn, _ := ownerContext(t, process.ExecutionFunction)
	_, err := a.Spawn(fn, detached)
	require.ErrorIs(t, err, apihost.ErrEvalDetachedDenied, "a function call cannot outlive its evals")

	_, err = a.Spawn(context.Background(), detached)
	require.ErrorIs(t, err, apihost.ErrEvalDetachedDenied, "detaching needs a caller execution")

	owner, _ := ownerContext(t)
	pairs, child, err := process.GetExecutionScope(owner).Reserve()
	require.NoError(t, err)
	require.NoError(t, child.Bind(admitParent))
	owned, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, fc.SetMultiple(append(pairs, process.ExecutionScopePair(process.NewExecutionScope(owned, process.ExecutionProcess, &recordingStarter{})))...))
	_, err = a.Spawn(owned, detached)
	require.ErrorIs(t, err, apihost.ErrEvalDetachedDenied, "an owned process stays contained")
}

func TestAdmitterSpawnRunsAdmittedImportRevision(t *testing.T) {
	a, starter := newTestAdmitter(t)
	library := `return { value = 1 }`
	a.host.WithImportLoader(func(registry.ID) (string, error) { return library, nil })
	ctx, _ := ownerContext(t)
	policy := apihost.EvalPolicy{Imports: []apihost.EvalImport{{Alias: "lib", Source: registry.NewID("app", "lib")}}}

	program, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource, Policy: policy})
	require.NoError(t, err)
	library = `return { value = 2 }`

	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{Program: program, Parent: admitParent})
	require.NoError(t, err)
	proc, err := starter.start.Admission.Factory()
	require.NoError(t, err)
	defer proc.Close()
	frame, _ := ctxapi.OpenFrameContext(context.Background())
	require.NoError(t, proc.Init(frame, "main", nil))
	var output process.StepOutput
	require.NoError(t, proc.Step(nil, &output), "imports run before the program")
	lib, ok := proc.(*engine.Process).State().GetGlobal("lib").(*lua.LTable)
	require.True(t, ok, "the import is bound")
	require.Equal(t, "1", lib.RawGetString("value").String(), "a program runs the library revision it was admitted with")
}

func TestAdmitterImportRunsUnderTheEvalBudget(t *testing.T) {
	a, starter := newTestAdmitter(t)
	a.host.WithImportLoader(func(registry.ID) (string, error) { return `while true do end`, nil })
	ctx, _ := ownerContext(t)
	policy := apihost.EvalPolicy{
		Imports:    []apihost.EvalImport{{Alias: "lib", Source: registry.NewID("app", "lib")}},
		TickBudget: 100,
	}

	_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent, Policy: policy})
	require.NoError(t, err)
	created := make(chan process.Process, 1)
	go func() {
		proc, createErr := starter.start.Admission.Factory()
		require.NoError(t, createErr)
		created <- proc
	}()
	var proc process.Process
	select {
	case proc = <-created:
	case <-time.After(5 * time.Second):
		t.Fatal("creating the eval process ran its import")
	}
	defer proc.Close()

	proc.(process.Preemptible).EnablePreemption()
	frame, _ := ctxapi.OpenFrameContext(context.Background())
	require.NoError(t, proc.Init(frame, "main", nil))
	var output process.StepOutput
	require.NoError(t, proc.Step(nil, &output))
	require.Equal(t, process.StepPreempted, output.Status(), "the import is preempted like the program")
}

func TestAdmitterTypedCompileKnowsBindingsAndImports(t *testing.T) {
	a, _ := newTestAdmitter(t)
	a.host.WithImportLoader(func(registry.ID) (string, error) {
		return `return { greet = function(n: string): string return "hi " .. n end }`, nil
	})
	ctx := context.Background()
	policy := apihost.EvalPolicy{
		CompileMode: apihost.EvalCompileTyped,
		Bindings:    []apihost.EvalBinding{{Name: "limit", Value: int64(3)}},
		Imports:     []apihost.EvalImport{{Alias: "lib", Source: registry.NewID("app", "lib")}},
	}
	_, err := a.Compile(ctx, apihost.EvalCompileSpec{Policy: policy, SourceCode: `
		return { main = function() return lib.greet("x"), limit end }
	`})
	require.NoError(t, err, "typed source may use its bindings and imports")

	_, err = a.Compile(ctx, apihost.EvalCompileSpec{Policy: policy, SourceCode: `
		return { main = function() return lib.greet(42) end }
	`})
	require.Error(t, err, "typed compilation still rejects type errors")
}

func TestAdmitterBoundsDetachedEvalLifetime(t *testing.T) {
	a, starter := newTestAdmitter(t, WithDetachedLifetime(5*time.Minute))
	ctx, _ := ownerContext(t)
	lifetimeOf := func() time.Duration {
		proc, err := starter.start.Admission.Factory()
		require.NoError(t, err)
		defer proc.Close()
		return proc.(process.ExecutionTimeoutProvider).ExecutionTimeout()
	}

	_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent})
	require.NoError(t, err)
	require.Zero(t, lifetimeOf(), "an owned eval lives as long as its owner")

	_, err = a.Spawn(ctx, apihost.EvalSpawnSpec{
		SourceCode: admitSource, Parent: admitParent, LinkMode: apihost.EvalLinkDetached,
		Policy: apihost.EvalPolicy{AllowDetached: true},
	})
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, lifetimeOf(), "a detached eval has a bounded lifetime")
}

func TestAdmitterSpawnHonorsCancellation(t *testing.T) {
	a, starter := newTestAdmitter(t)
	ctx, _ := ownerContext(t)
	program, err := a.Compile(ctx, apihost.EvalCompileSpec{SourceCode: admitSource})
	require.NoError(t, err)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, spec := range map[string]apihost.EvalSpawnSpec{
		"cached program": {Program: program, Parent: admitParent},
		"source":         {SourceCode: admitSource, Parent: admitParent},
	} {
		_, err := a.Spawn(cancelled, spec)
		require.ErrorIs(t, err, context.Canceled, name)
	}
	require.Nil(t, starter.start, "no process starts for a cancelled caller")
}

type tracePropagator struct{}

func (tracePropagator) PropagateValue() any { return "span-context" }

func TestAdmitterSpawnCarriesOnlyCrossProcessCallerValues(t *testing.T) {
	a, starter := newTestAdmitter(t)
	ctx, _ := ownerContext(t)
	fc := ctxapi.FrameFromContext(ctx)
	traceKey := &ctxapi.Key{Name: "test.trace", Inherit: true}
	deliveryKey := &ctxapi.Key{Name: "test.delivery", Inherit: true}
	require.NoError(t, fc.SetMultiple(
		ctxapi.Pair{Key: traceKey, Value: tracePropagator{}},
		ctxapi.Pair{Key: deliveryKey, Value: "caller delivery"},
	))

	_, err := a.Spawn(ctx, apihost.EvalSpawnSpec{SourceCode: admitSource, Parent: admitParent})
	require.NoError(t, err)
	var trace any
	for _, pair := range starter.start.Context {
		require.NotSame(t, deliveryKey, pair.Key, "caller authority stays with the caller")
		if pair.Key == traceKey {
			trace = pair.Value
		}
	}
	require.Equal(t, "span-context", trace, "the eval continues the caller's trace")
}
