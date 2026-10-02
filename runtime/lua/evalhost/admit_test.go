// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	sysprocess "github.com/wippyai/runtime/system/process"
	"go.uber.org/zap"
)

type recordingStarter struct {
	start *process.Start
	err   error
}

func (s *recordingStarter) Start(_ context.Context, start *process.Start) (pid.PID, error) {
	s.start = start
	if s.err != nil {
		return pid.PID{}, s.err
	}
	return pid.PID{Host: start.HostID, UniqID: "eval-1"}, nil
}

var admitParent = pid.PID{Host: "app:processes", UniqID: "parent"}

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
		"jit":                       {CompileMode: apihost.EvalCompileJIT},
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	require.NotEqual(t, secapi.Allow, scope.Evaluate(actor, "process.terminate", admitParent.String(), nil))
	require.NotEqual(t, secapi.Allow, scope.Evaluate(actor, "fs.read", "/", nil), "no ambient authority")
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
