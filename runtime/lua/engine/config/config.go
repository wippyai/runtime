package config

import (
	"fmt"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// VMConfig holds configuration for Callable instances in the pool
type VMConfig struct {
	Modules    []api.Module
	Libraries  []Library
	Globals    []Global
	Functions  []Function
	EngineOpts []engine.Option
	Logger     *zap.Logger
	compiled   bool
}

// Library represents a Lua library to be loaded
type Library struct {
	Name   string
	Script string
}

// Function represents a Lua function to be loaded
type Function struct {
	Name   string
	Script string
}

// Global represents a global variable in the Lua environment
type Global struct {
	Name  string
	Value lua.LValue
}

// NewVMConfig creates a new Callable configuration with default values
func NewVMConfig(logger *zap.Logger) *VMConfig {
	return &VMConfig{
		Modules:    make([]api.Module, 0),
		Libraries:  make([]Library, 0),
		Globals:    make([]Global, 0),
		Functions:  make([]Function, 0),
		EngineOpts: make([]engine.Option, 0),
		Logger:     logger,
	}
}

// VMConfigOption represents a configuration option for VMConfig
type VMConfigOption func(*VMConfig)

// WithModule adds a Lua module to Callable configuration
func WithModule(module api.Module) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Modules = append(cfg.Modules, module)
		cfg.compiled = false
	}
}

// WithLibrary adds a Lua library to Callable configuration
func WithLibrary(name, script string) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Libraries = append(cfg.Libraries, Library{
			Name:   name,
			Script: script,
		})
		cfg.compiled = false
	}
}

// WithGlobalValue adds a global variable to Callable configuration
func WithGlobalValue(name string, value lua.LValue) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Globals = append(cfg.Globals, Global{
			Name:  name,
			Value: value,
		})
		cfg.compiled = false
	}
}

// WithFunction adds a Lua function to Callable configuration
func WithFunction(name string, script string) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Functions = append(cfg.Functions, Function{
			Name:   name,
			Script: script,
		})
		cfg.compiled = false
	}
}

// WithEngineOptions adds engine-specific options to Callable configuration
func WithEngineOptions(opts ...engine.Option) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.EngineOpts = append(cfg.EngineOpts, opts...)
		cfg.compiled = false
	}
}

// todo: compile option here?

func (cfg *VMConfig) Compile() error {
	// todo: ??
	return nil
}

func (cfg *VMConfig) MakeVM() (api.VM, error) {
	return CreateVM(cfg)
}

// CreateVM creates a new Callable instance with the provided configuration
func CreateVM(cfg *VMConfig) (*engine.VM, error) {
	// Collect all options
	opts := make([]engine.Option, 0)

	// Add engine options first
	opts = append(opts, cfg.EngineOpts...)

	// Add modules
	for _, module := range cfg.Modules {
		opts = append(opts, engine.WithLoader(module.Name(), module.Loader))
	}

	// Add libraries as proper modules
	for _, lib := range cfg.Libraries {
		opts = append(opts, engine.WithLibrary(lib.Name, lib.Script))
	}

	// Add globals via options
	for _, global := range cfg.Globals {
		opts = append(opts, engine.WithGlobalValue(global.Name, global.Value))
	}

	// Create Callable with all options
	vm, err := engine.NewVM(cfg.Logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Callable: %w", err)
	}

	// Add bound functions via options
	for _, fn := range cfg.Functions {
		if err := vm.CompileFunction(fn.Name, fn.Script); err != nil {
			vm.Close()
			return nil, fmt.Errorf("failed to compile function %q: %w", fn.Name, err)
		}
	}

	// mount coroutine based VM

	return vm, nil
}
