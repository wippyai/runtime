package engine

import (
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
	lua "github.com/yuin/gopher-lua"
)

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
func NewFactory(cfg FactoryConfig) process.ProcessFactory {
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
func NewFactoryFromProto(proto *lua.FunctionProto, binders ...ModuleBinder) process.ProcessFactory {
	return NewFactory(FactoryConfig{
		Proto:         proto,
		ModuleBinders: binders,
	})
}

// NewFactoryFromScript creates a factory from a script string.
func NewFactoryFromScript(script, name string, binders ...ModuleBinder) process.ProcessFactory {
	return NewFactory(FactoryConfig{
		Script:        script,
		ScriptName:    name,
		ModuleBinders: binders,
	})
}

// CompileFactory compiles a script and returns a factory using the compiled proto.
// Returns error if compilation fails.
func CompileFactory(script, name string, binders ...ModuleBinder) (process.ProcessFactory, error) {
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

	// Load core libs
	libs := []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.CoroutineLibName, lua.OpenCoroutine},
		{lua.LoadLibName, OpenRestrictedPackage},
	}

	for _, lib := range libs {
		state.Push(state.NewFunction(lib.fn))
		state.Push(lua.LString(lib.name))
		state.Call(1, 0)
	}

	// Load os module (time, date, clock)
	ostime.Bind(state)

	// Apply module binders
	for _, binder := range f.moduleBinders {
		binder(state)
	}

	return state
}
