// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- toSet ---

func TestToSet_Empty(t *testing.T) {
	s := toSet(nil)
	assert.Empty(t, s)
}

func TestToSet_Items(t *testing.T) {
	s := toSet([]string{"a", "b", "c"})
	assert.Len(t, s, 3)
	_, ok := s["a"]
	assert.True(t, ok)
	_, ok = s["d"]
	assert.False(t, ok)
}

func TestToSet_Duplicates(t *testing.T) {
	s := toSet([]string{"a", "a", "b"})
	assert.Len(t, s, 2)
}

// --- hasAnyClass ---

func TestHasAnyClass_NoClasses(t *testing.T) {
	set := toSet([]string{"io", "network"})
	assert.False(t, hasAnyClass(nil, set))
}

func TestHasAnyClass_EmptySet(t *testing.T) {
	assert.False(t, hasAnyClass([]string{"io"}, nil))
}

func TestHasAnyClass_Match(t *testing.T) {
	set := toSet([]string{"io", "network"})
	assert.True(t, hasAnyClass([]string{"compute", "io"}, set))
}

func TestHasAnyClass_NoMatch(t *testing.T) {
	set := toSet([]string{"io", "network"})
	assert.False(t, hasAnyClass([]string{"compute", "storage"}, set))
}

// --- newProcessConfig ---

func TestNewProcessConfig_Defaults(t *testing.T) {
	cfg := newProcessConfig()
	assert.Equal(t, code.AllowAll, cfg.buildMode)
	assert.Nil(t, cfg.filter)
	assert.Nil(t, cfg.allowedIDs)
	assert.Nil(t, cfg.deniedIDs)
	assert.Nil(t, cfg.requiredIDs)
	assert.Nil(t, cfg.extraModules)
}

// --- Factory options ---

func TestWithMode(t *testing.T) {
	cfg := newProcessConfig()
	WithMode(code.DenyAll)(cfg)
	assert.Equal(t, code.DenyAll, cfg.buildMode)
}

func TestWithAllowed(t *testing.T) {
	cfg := newProcessConfig()
	id1 := registry.NewID("ns", "mod1")
	id2 := registry.NewID("ns", "mod2")
	WithAllowed(id1, id2)(cfg)
	assert.Len(t, cfg.allowedIDs, 2)
}

func TestWithAllowedClasses(t *testing.T) {
	cfg := newProcessConfig()
	WithAllowedClasses("io", "network")(cfg)
	assert.Equal(t, []string{"io", "network"}, cfg.allowedClasses)
}

func TestExcludeClasses(t *testing.T) {
	cfg := newProcessConfig()
	ExcludeClasses("debug", "test")(cfg)
	assert.Equal(t, []string{"debug", "test"}, cfg.excludeClasses)
}

func TestExcludeModules(t *testing.T) {
	cfg := newProcessConfig()
	ExcludeModules("profiler", "debugger")(cfg)
	assert.Equal(t, []string{"profiler", "debugger"}, cfg.excludeModules)
}

func TestWithModule(t *testing.T) {
	cfg := newProcessConfig()
	mod := &luaapi.ModuleDef{Name: "testmod"}
	WithModule(mod)(cfg)
	require.Len(t, cfg.extraModules, 1)
	assert.Equal(t, "testmod", cfg.extraModules[0].Name)
}

func TestWithFilter(t *testing.T) {
	cfg := newProcessConfig()
	called := false
	WithFilter(func(name string, classes []string) (bool, error) {
		called = true
		return true, nil
	})(cfg)
	require.NotNil(t, cfg.filter)
	ok, err := cfg.filter("mod", nil)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, called)
}

func TestWithAllowed_Appends(t *testing.T) {
	cfg := newProcessConfig()
	id1 := registry.NewID("ns", "mod1")
	id2 := registry.NewID("ns", "mod2")
	WithAllowed(id1)(cfg)
	WithAllowed(id2)(cfg)
	assert.Len(t, cfg.allowedIDs, 2)
}

// --- Factory.CreateState ---

func TestFactory_CreateState_Default(t *testing.T) {
	f := &Factory{}
	state, err := f.CreateState()
	require.NoError(t, err)
	require.NotNil(t, state)
	defer state.Close()

	// base functions available
	assert.NotNil(t, state.GetGlobal("type"))
	assert.NotNil(t, state.GetGlobal("tostring"))
}

func TestFactory_CreateState_CustomOptions(t *testing.T) {
	opts := &lua.Options{
		RegistrySize:        64,
		RegistryMaxSize:     1024,
		RegistryGrowStep:    8,
		SkipOpenLibs:        true,
		CallStackSize:       64,
		MinimizeStackMemory: true,
	}
	f := &Factory{stateOpts: opts}
	state, err := f.CreateState()
	require.NoError(t, err)
	require.NotNil(t, state)
	state.Close()
}

func TestFactory_CreateState_BinderError(t *testing.T) {
	f := &Factory{
		moduleBinders: []ModuleBinder{
			func(l *lua.LState) error {
				return assert.AnError
			},
		},
	}
	_, err := f.CreateState()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module binder failed")
}

// --- Factory.Create ---

func TestFactory_Create_WithScript(t *testing.T) {
	f := &Factory{
		script:     "return 42",
		scriptName: "test.lua",
	}

	proc, err := f.Create()
	require.NoError(t, err)
	require.NotNil(t, proc)

	p := proc.(*Process)
	assert.Equal(t, "return 42", p.script)
	assert.Equal(t, "test.lua", p.scriptName)
}

func TestFactory_Create_WithProto(t *testing.T) {
	proto := compileTestProto(t, "return 1 + 2")

	f := &Factory{
		proto: proto,
	}

	proc, err := f.Create()
	require.NoError(t, err)
	require.NotNil(t, proc)

	p := proc.(*Process)
	assert.NotNil(t, p.proto)
}

// --- NewFactory ---

func TestNewFactory_ReturnsFunc(t *testing.T) {
	fn := NewFactory(FactoryConfig{
		Script: "return 1",
	})
	require.NotNil(t, fn)

	proc, err := fn()
	require.NoError(t, err)
	assert.NotNil(t, proc)
}

// --- NewFactoryFromProto ---

func TestNewFactoryFromProto(t *testing.T) {
	proto := compileTestProto(t, "return true")

	fn := NewFactoryFromProto(proto)
	require.NotNil(t, fn)

	proc, err := fn()
	require.NoError(t, err)
	assert.NotNil(t, proc)
}

func TestNewFactoryFromProto_WithBinder(t *testing.T) {
	proto := compileTestProto(t, "return INJECTED")
	binderCalled := false

	fn := NewFactoryFromProto(proto, func(l *lua.LState) error {
		binderCalled = true
		l.SetGlobal("INJECTED", lua.LNumber(99))
		return nil
	})

	proc, err := fn()
	require.NoError(t, err)
	assert.NotNil(t, proc)
	assert.True(t, binderCalled)
}

// --- NewProcessFactory ---

func TestNewProcessFactory(t *testing.T) {
	pf := NewProcessFactory(nil)
	require.NotNil(t, pf)
}

func TestDependencyRaisePreservesKind(t *testing.T) {
	id := registry.NewID("test", "dependency")
	compiled := &code.CompiledMain{
		Dependencies: []code.CompiledProto{{
			Name:  "dependency",
			Node:  &code.Node{ID: id},
			Proto: compileTestProto(t, `error(errors.new({message="bad dependency", kind=errors.INVALID, retryable=false}))`),
		}},
	}
	proc := newDependencyProcess(t, compiled, `return { main = function() end }`, 0)
	if err := proc.Init(frameContext(), "main", nil); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	err := proc.Step(nil, &output)
	if err == nil || apierror.BuildChain(err).Root().Kind != string(apierror.Invalid) {
		t.Fatalf("dependency error = %v", err)
	}
}

// Library chunks run in dependency order under per-chunk environments, inside
// the first step.
func TestDependenciesRunInFirstStepWithScopedEnvironments(t *testing.T) {
	baseID := registry.NewID("test", "base")
	midID := registry.NewID("test", "mid")
	mainID := registry.NewID("test", "main")
	compiled := &code.CompiledMain{
		MainID: mainID,
		Imports: map[registry.ID][]code.Import{
			midID:  {{ID: baseID, Alias: "b"}},
			mainID: {{ID: midID, Alias: "m"}},
		},
		Dependencies: []code.CompiledProto{
			{Name: "base", Node: &code.Node{ID: baseID}, Proto: compileTestProto(t, `ran = (ran or 0) + 1 return { v = 1 }`)},
			{Name: "mid", Node: &code.Node{ID: midID}, Proto: compileTestProto(t, `return { v = b.v + 1, via_require = require("b").v }`)},
		},
	}
	proc := newDependencyProcess(t, compiled, `
		return { main = function()
			return m.v * 100 + require("m").via_require * 10 + (b == nil and 1 or 0)
		end }
	`, 0)
	state := proc.State()
	if state.GetGlobal("ran") != lua.LNil {
		t.Fatal("library code must not run before the first step")
	}
	if err := proc.Init(frameContext(), "main", nil); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	// m.v = 2, require("m").via_require = 1, b hidden from main = 1.
	if v, _ := output.Result().Data().(lua.LValue); lua.LVAsNumber(v) != 211 {
		t.Fatalf("expected 211, got %v", output.Result().Data())
	}
}

// A library that never returns is preempted instead of blocking Init, and a
// later execution of a process with finished libraries does not run them again.
func TestDependencyLoopIsPreempted(t *testing.T) {
	id := registry.NewID("test", "spin")
	compiled := &code.CompiledMain{
		Dependencies: []code.CompiledProto{{Name: "spin", Node: &code.Node{ID: id}, Proto: compileTestProto(t, `while true do end`)}},
	}
	proc := newDependencyProcess(t, compiled, `return { main = function() end }`, 100)
	proc.EnablePreemption()

	inited := make(chan error, 1)
	go func() { inited <- proc.Init(frameContext(), "main", nil) }()
	select {
	case err := <-inited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Init blocked on library code")
	}
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if output.Status() != process.StepPreempted {
		t.Fatalf("expected the library loop to be preempted, got %v", output.Status())
	}
}

func TestDependenciesRunOncePerProcess(t *testing.T) {
	id := registry.NewID("test", "counter")
	mainID := registry.NewID("test", "main")
	compiled := &code.CompiledMain{
		MainID:  mainID,
		Imports: map[registry.ID][]code.Import{mainID: {{ID: id, Alias: "c"}}},
		Dependencies: []code.CompiledProto{{Name: "counter", Node: &code.Node{ID: id}, Proto: compileTestProto(t,
			`runs = (runs or 0) + 1 return { runs = runs }`)}},
	}
	proc := newDependencyProcess(t, compiled, `return { main = function() return c.runs end }`, 0)
	for i := 0; i < 3; i++ {
		if err := proc.Init(frameContext(), "main", nil); err != nil {
			t.Fatal(err)
		}
		var output process.StepOutput
		if err := proc.Step(nil, &output); err != nil {
			t.Fatal(err)
		}
		if v, _ := output.Result().Data().(lua.LValue); lua.LVAsNumber(v) != 1 {
			t.Fatalf("execution %d: expected the library to have run once, got %v", i, output.Result().Data())
		}
	}
}

func frameContext() context.Context {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	return ctx
}

// newDependencyProcess builds a process over script whose state is bound
// through the factory's isolation binder for compiled.
func newDependencyProcess(t *testing.T, compiled *code.CompiledMain, script string, tickBudget int64) *Process {
	t.Helper()
	binder := NewProcessFactory(nil).isolationBinder(compiled, newProcessConfig(), nil, nil, nil, nil)
	budgets := luaapi.ExecutionBudgets{}
	if tickBudget != 0 {
		budgets = luaapi.ExecutionBudgets{TickBudget: tickBudget, TickBudgetSet: true}
	}
	proc := mustNewProcess(t,
		WithModuleBinder(wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })),
		WithModuleBinder(binder),
		WithScript(script, "main.lua"),
		WithProcessExecutionBudgets(budgets),
	)
	t.Cleanup(proc.Close)
	return proc
}

// --- helpers ---

func compileTestProto(t *testing.T, source string) *lua.FunctionProto {
	t.Helper()
	l := lua.NewState()
	defer l.Close()

	fn, err := l.LoadString(source)
	require.NoError(t, err)
	return fn.Proto
}

// A library that assigns _G under the alias it is imported as does not
// override the declared import: the alias resolves the same through the
// environment and through require.
func TestLibraryGlobalExportDoesNotOverrideImportAlias(t *testing.T) {
	id := registry.NewID("test", "c")
	mainID := registry.NewID("test", "main")
	compiled := &code.CompiledMain{
		MainID:  mainID,
		Imports: map[registry.ID][]code.Import{mainID: {{ID: id, Alias: "c"}}},
		Dependencies: []code.CompiledProto{{Name: "c", Node: &code.Node{ID: id}, Proto: compileTestProto(t,
			`_G.c = { v = 99 } _G.other = 7 return { v = 42 }`)}},
	}
	proc := newDependencyProcess(t, compiled, `
		return { main = function() return c.v * 1000 + require("c").v + other end }
	`, 0)
	runProcess(t, proc, 42049)
}

// A global installed after the environment is in use stays visible even when
// it shares a name with an import alias.
func TestDynamicGlobalOverridesImportAliasAfterInitialization(t *testing.T) {
	id := registry.NewID("test", "dsl")
	mainID := registry.NewID("test", "main")
	compiled := &code.CompiledMain{
		MainID:  mainID,
		Imports: map[registry.ID][]code.Import{mainID: {{ID: id, Alias: "dsl"}}},
		Dependencies: []code.CompiledProto{{Name: "dsl", Node: &code.Node{ID: id}, Proto: compileTestProto(t,
			`return { with = function(fn) _G.dsl = function() return 5 end local r = fn() _G.dsl = nil return r end }`)}},
	}
	proc := newDependencyProcess(t, compiled, `
		return { main = function()
			return require("dsl").with(function() return dsl() end)
		end }
	`, 0)
	runProcess(t, proc, 5)
}

// SyncExecute initializes the process's libraries before it runs the chunk.
func TestSyncExecuteRunsPendingInitializers(t *testing.T) {
	id := registry.NewID("test", "c")
	mainID := registry.NewID("test", "main")
	compiled := &code.CompiledMain{
		MainID:  mainID,
		Imports: map[registry.ID][]code.Import{mainID: {{ID: id, Alias: "c"}}},
		Dependencies: []code.CompiledProto{{Name: "c", Node: &code.Node{ID: id}, Proto: compileTestProto(t,
			`_G.runs = (_G.runs or 0) + 1 return { v = 42 }`)}},
	}
	binder := NewProcessFactory(nil).isolationBinder(compiled, newProcessConfig(), nil, nil, nil, nil)
	proc := mustNewProcess(t, WithModuleBinder(binder), WithProto(compileTestProto(t, `return c.v + runs`)))
	t.Cleanup(proc.Close)
	for i := 0; i < 2; i++ {
		got, err := proc.SyncExecute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if lua.LVAsNumber(got) != 43 {
			t.Fatalf("run %d: expected 43, got %v", i, got)
		}
	}
}

func TestSyncExecuteInitializerErrorKeepsKind(t *testing.T) {
	id := registry.NewID("test", "c")
	compiled := &code.CompiledMain{
		Dependencies: []code.CompiledProto{{Name: "c", Node: &code.Node{ID: id}, Proto: compileTestProto(t,
			`error(errors.new({message="bad dependency", kind=errors.INVALID, retryable=false}))`)}},
	}
	binder := NewProcessFactory(nil).isolationBinder(compiled, newProcessConfig(), nil, nil, nil, nil)
	proc := mustNewProcess(t,
		WithModuleBinder(wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })),
		WithModuleBinder(binder), WithProto(compileTestProto(t, `return 1`)))
	t.Cleanup(proc.Close)
	_, err := proc.SyncExecute(context.Background())
	if err == nil || apierror.BuildChain(err).Root().Kind != string(apierror.Invalid) {
		t.Fatalf("expected an Invalid error from the library, got %v", err)
	}
}

func runProcess(t *testing.T, proc *Process, want float64) {
	t.Helper()
	if err := proc.Init(frameContext(), "main", nil); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if v, _ := output.Result().Data().(lua.LValue); float64(lua.LVAsNumber(v)) != want {
		t.Fatalf("expected %v, got %v", want, output.Result().Data())
	}
}

// Every chunk environment decides its import aliases when library
// initialization completes: a sibling library's _G export under an alias name
// does not override the import in an earlier library, whether or not that
// library looked up a global while it initialized.
func TestSiblingGlobalExportDoesNotOverrideEarlierLibraryImport(t *testing.T) {
	for name, aSource := range map[string]string{
		"no top-level lookup":   `return { get = function() return c.v end }`,
		"top-level global read": `local _ = tostring return { get = function() return c.v end }`,
	} {
		t.Run(name, func(t *testing.T) {
			aID, bID, cID, mainID := registry.NewID("t", "a"), registry.NewID("t", "b"), registry.NewID("t", "c"), registry.NewID("t", "main")
			compiled := &code.CompiledMain{
				MainID: mainID,
				Imports: map[registry.ID][]code.Import{
					aID:    {{ID: cID, Alias: "c"}},
					mainID: {{ID: aID, Alias: "a"}, {ID: bID, Alias: "b"}},
				},
				Dependencies: []code.CompiledProto{
					{Name: "c", Node: &code.Node{ID: cID}, Proto: compileTestProto(t, `return { v = 42 }`)},
					{Name: "a", Node: &code.Node{ID: aID}, Proto: compileTestProto(t, aSource)},
					{Name: "b", Node: &code.Node{ID: bID}, Proto: compileTestProto(t, `_G.c = { v = 99 } return {}`)},
				},
			}
			proc := newDependencyProcess(t, compiled, `return { main = function() return a.get() end }`, 0)
			runProcess(t, proc, 42)
		})
	}
}
