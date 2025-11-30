package engine

import (
	"github.com/wippyai/runtime/api/scheduler"
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

// NewFactory creates a ProcessFactory for Lua processes.
// The factory returns processes that are already initialized.
func NewFactory(cfg FactoryConfig) scheduler.ProcessFactory {
	return func() (scheduler.Process, error) {
		opts := make([]ProcessOption, 0, 4)

		if cfg.Proto != nil {
			opts = append(opts, WithProto(cfg.Proto))
		} else if cfg.Script != "" {
			opts = append(opts, WithScript(cfg.Script, cfg.ScriptName))
		}

		for _, binder := range cfg.ModuleBinders {
			opts = append(opts, WithModuleBinder(binder))
		}

		if cfg.StateOptions != nil {
			opts = append(opts, WithStateOptions(*cfg.StateOptions))
		}

		proc := NewProcess(opts...)
		if err := proc.Init(); err != nil {
			return nil, err
		}

		return proc, nil
	}
}

// NewFactoryFromProto creates a factory from a precompiled proto with default module bindings.
func NewFactoryFromProto(proto *lua.FunctionProto, binders ...ModuleBinder) scheduler.ProcessFactory {
	return NewFactory(FactoryConfig{
		Proto:         proto,
		ModuleBinders: binders,
	})
}

// NewFactoryFromScript creates a factory from a script string.
func NewFactoryFromScript(script, name string, binders ...ModuleBinder) scheduler.ProcessFactory {
	return NewFactory(FactoryConfig{
		Script:        script,
		ScriptName:    name,
		ModuleBinders: binders,
	})
}

// CompileFactory compiles a script and returns a factory using the compiled proto.
// Returns error if compilation fails.
func CompileFactory(script, name string, binders ...ModuleBinder) (scheduler.ProcessFactory, error) {
	proto, err := lua.CompileString(script, name)
	if err != nil {
		return nil, err
	}
	return NewFactoryFromProto(proto, binders...), nil
}
