package pool

import (
	"fmt"
	"github.com/ponyruntime/go-lua"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
)

// VMConfig holds configuration for VM instances in the pool
type VMConfig struct {
	Modules    map[string]api.Module
	Libraries  map[string]string
	Globals    map[string]lua.LValue
	Functions  map[string]string
	EngineOpts []engine.Option
	Logger     *zap.Logger
}

// NewVMConfig creates a new VM configuration with default values
func NewVMConfig(logger *zap.Logger) *VMConfig {
	return &VMConfig{
		Modules:    make(map[string]api.Module),
		Libraries:  make(map[string]string),
		Globals:    make(map[string]lua.LValue),
		Functions:  make(map[string]string),
		EngineOpts: make([]engine.Option, 0),
		Logger:     logger,
	}
}

// VMConfigOption represents a configuration option for VMConfig
type VMConfigOption func(*VMConfig)

// WithModule adds a Lua module to VM configuration
func WithModule(name string, module api.Module) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Modules[name] = module
	}
}

// WithLibrary adds a Lua library to VM configuration
func WithLibrary(name, script string) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Libraries[name] = script
	}
}

// WithGlobalValue adds a global variable to VM configuration
func WithGlobalValue(name string, value lua.LValue) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Globals[name] = value
	}
}

// WithFunction adds a Lua function to VM configuration
func WithFunction(name string, script string) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.Functions[name] = script
	}
}

// WithEngineOptions adds engine-specific options to VM configuration
func WithEngineOptions(opts ...engine.Option) VMConfigOption {
	return func(cfg *VMConfig) {
		cfg.EngineOpts = append(cfg.EngineOpts, opts...)
	}
}

// CreateVM creates a new VM instance with the provided configuration
func CreateVM(cfg *VMConfig) (*engine.VM, error) {
	// Collect all options
	opts := make([]engine.Option, 0)

	// Add engine options first
	opts = append(opts, cfg.EngineOpts...)

	// Add modules
	for modName, module := range cfg.Modules {
		opts = append(opts, engine.WithLoader(modName, module.Loader))
	}

	// Add libraries as proper modules
	for libName, libScript := range cfg.Libraries {
		opts = append(opts, engine.WithLibrary(libName, libScript))
	}

	// Add globals via options
	for name, value := range cfg.Globals {
		opts = append(opts, engine.WithGlobalValue(name, value))
	}

	// Create VM with all options
	vm, err := engine.NewVM(cfg.Logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	// Add bound functions via options
	for name, script := range cfg.Functions {
		if err := vm.CompileFunction(name, script); err != nil {
			vm.Close()
			return nil, fmt.Errorf("failed to compile function %q: %w", name, err)
		}
	}

	return vm, nil
}
