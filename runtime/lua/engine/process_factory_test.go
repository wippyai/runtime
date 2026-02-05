package engine

import (
	"testing"

	lua "github.com/wippyai/go-lua"
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

// --- helpers ---

func compileTestProto(t *testing.T, source string) *lua.FunctionProto {
	t.Helper()
	l := lua.NewState()
	defer l.Close()

	fn, err := l.LoadString(source)
	require.NoError(t, err)
	return fn.Proto
}
