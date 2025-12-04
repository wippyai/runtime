package engine

import (
	"github.com/wippyai/runtime/api/process"
	lua "github.com/yuin/gopher-lua"
)

// todo: redo most of it

// FactoryConfig configures a Lua process factory.
type FactoryConfig struct {
	// Proto is a precompiled Lua function (faster than Script).
	Proto *lua.FunctionProto

	// Script and ScriptName for on-the-fly compilation.
	Script     string
	ScriptName string

	// ModuleBinders are called after state creation to bind modules.
	ModuleBinders []ModuleBinder

	// StateOptions customize Lua state (memory, stack, etc).
	StateOptions *lua.Options
}

// Factory creates Lua processes with shared configuration.
// Holds binders and options - processes only store script/proto.
type Factory struct {
	proto         *lua.FunctionProto
	script        string
	scriptName    string
	moduleBinders []ModuleBinder
	stateOpts     *lua.Options
}

// NewFactory creates a ProcessFactory for Lua processes.
// The factory returns processes that are already initialized.
func NewFactory(cfg FactoryConfig) process.NewFunc {
	f := &Factory{
		proto:         cfg.Proto,
		script:        cfg.Script,
		scriptName:    cfg.ScriptName,
		moduleBinders: cfg.ModuleBinders,
		stateOpts:     cfg.StateOptions,
	}
	return f.Create
}

// Create produces a new initialized Process.
func (f *Factory) Create() (process.Process, error) {
	proc := &Process{
		threads:  make([]*Task, 0, 4),
		queue:    NewTaskQueue(),
		yieldBuf: make([]*Task, 0, 4),
		factory:  f,
		state:    f.CreateState(),
	}

	if f.proto != nil {
		proc.proto = f.proto
	} else if f.script != "" {
		proc.script = f.script
		proc.scriptName = f.scriptName
	}

	return proc, nil
}

// NewFactoryFromProto creates a factory from a precompiled proto with default module bindings.
func NewFactoryFromProto(proto *lua.FunctionProto, binders ...ModuleBinder) process.NewFunc {
	return NewFactory(FactoryConfig{
		Proto:         proto,
		ModuleBinders: binders,
	})
}

// NewFactoryFromScript creates a factory from a script string.
func NewFactoryFromScript(script, name string, binders ...ModuleBinder) process.NewFunc {
	return NewFactory(FactoryConfig{
		Script:        script,
		ScriptName:    name,
		ModuleBinders: binders,
	})
}

// CompileFactory compiles a script and returns a factory using the compiled proto.
// Returns error if compilation fails.
func CompileFactory(script, name string, binders ...ModuleBinder) (process.NewFunc, error) {
	proto, err := lua.CompileString(script, name)
	if err != nil {
		return nil, err
	}
	return NewFactoryFromProto(proto, binders...), nil
}

// CreateState creates and initializes a new Lua state with core libs and module binders.
func (f *Factory) CreateState() *lua.LState {
	opts := lua.Options{
		RegistrySize:        128,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
	}
	if f.stateOpts != nil {
		opts = *f.stateOpts
	}

	state := lua.NewState(opts)
	global := state.G.Global

	// Use global singleton tables (zero allocation)
	global.RawSetString(lua.TabLibName, lua.GetGlobalTableMod())
	global.RawSetString(lua.StringLibName, lua.GetGlobalStringMod())
	global.RawSetString(lua.MathLibName, lua.GetGlobalMathMod())
	global.RawSetString(lua.CoroutineLibName, lua.GetGlobalCoroutineMod())

	// String metatable
	state.G.SetBuiltinMt(lua.LTString, lua.GetGlobalStringMod())

	// Base functions (must be set on global)
	state.Push(lua.LGoFunc(lua.OpenBase))
	state.Push(lua.LString(lua.BaseLibName))
	state.Call(1, 0)

	// Restricted package loader (wippy-specific)
	state.Push(lua.LGoFunc(OpenRestrictedPackage))
	state.Push(lua.LString(lua.LoadLibName))
	state.Call(1, 0)

	// Apply module binders
	for _, binder := range f.moduleBinders {
		binder(state)
	}

	return state
}
