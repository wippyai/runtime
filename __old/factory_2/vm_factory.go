// Package factory provides a flexible configuration system for creating and
// customizing Lua virtual machines in the Pony runtime environment
package factory_2

import (
	"fmt"

	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// PoolFactory holds configuration for Lua VM instances in the pool. It manages
// modules, libraries, global variables, and functions that will be available
// to each created VM instance.
type Factory struct {
	Modules           []api.Module        // Lua modules to be loaded
	Libraries         []Library           // Pre-compiled Lua libraries
	Globals           []Global            // Global variables
	Functions         []Function          // Functions to be registered
	ExportedFunctions map[string]struct{} // Set of exported function names
	EngineOpts        []engine.Option     // Engine-specific configuration
	Logger            *zap.Logger         // Logger instance
	Coroutines        bool                // Enable coroutine support
	compiled          bool                // Internal compilation state
}

// Library represents a Lua library to be loaded into the VM.
// The Script field contains the library's source code.
type Library struct {
	Name   string // Library name
	Script string // Library source code
}

// Function represents a Lua function to be loaded into the VM.
// The Script field contains the function's source code.
type Function struct {
	Name   string // Function name
	Script string // Function source code
}

// Global represents a global variable to be set in the Lua environment.
type Global struct {
	Name  string     // Variable name
	Value lua.LValue // Variable value
}

// NewPoolFactory creates a new PoolFactory instance with default values.
// The provided logger will be used for all VMs created by this factory.
func NewFactory(logger *zap.Logger) *Factory {
	return &Factory{
		Modules:           make([]api.Module, 0),
		Libraries:         make([]Library, 0),
		Globals:           make([]Global, 0),
		Functions:         make([]Function, 0),
		ExportedFunctions: make(map[string]struct{}),
		EngineOpts:        make([]engine.Option, 0),
		Logger:            logger,
	}
}

// VMConfigOption represents a configuration option function for PoolFactory.
// It follows the functional options pattern for configuring PoolFactory instances.
type VMConfigOption func(*Factory)

// WithModule adds a Lua module to the PoolFactory configuration.
// Modules are loaded when new VM instances are created.
func WithModule(module api.Module) VMConfigOption {
	return func(cfg *Factory) {
		cfg.Modules = append(cfg.Modules, module)
		cfg.compiled = false
	}
}

// WithLibrary adds a Lua library to the PoolFactory configuration.
// Libraries are pre-compiled and loaded into each new VM instance.
func WithLibrary(name, script string) VMConfigOption {
	return func(cfg *Factory) {
		cfg.Libraries = append(cfg.Libraries, Library{
			Name:   name,
			Script: script,
		})
		cfg.compiled = false
	}
}

// WithGlobalValue adds a global variable to the PoolFactory configuration.
// These variables will be available in the global scope of each new VM instance.
func WithGlobalValue(name string, value lua.LValue) VMConfigOption {
	return func(cfg *Factory) {
		cfg.Globals = append(cfg.Globals, Global{
			Name:  name,
			Value: value,
		})
		cfg.compiled = false
	}
}

// WithFunction adds a Lua function to the PoolFactory configuration.
// Functions are compiled and made available in each new VM instance.
func WithFunction(name string, script string) VMConfigOption {
	return func(cfg *Factory) {
		cfg.Functions = append(cfg.Functions, Function{
			Name:   name,
			Script: script,
		})
		cfg.compiled = false
	}
}

// WithEngineOptions adds engine-specific options to the PoolFactory configuration.
// These options are applied when creating new VM instances.
func WithEngineOptions(opts ...engine.Option) VMConfigOption {
	return func(cfg *Factory) {
		cfg.EngineOpts = append(cfg.EngineOpts, opts...)
		cfg.compiled = false
	}
}

// Compile prepares the PoolFactory for VM creation by pre-compiling libraries
// and validating configuration. Must be called before MakeVM.
func (cfg *Factory) Compile() error {
	// todo: implement
	return nil
}

// MakeVM creates a new VM instance with the current PoolFactory configuration.
// Returns an error if compilation fails or VM creation encounters issues.
func (cfg *Factory) MakeVM() (api.VM, error) {
	if !cfg.compiled {
		if err := cfg.Compile(); err != nil {
			return nil, err
		}
	}

	base, err := createVM(cfg)
	if err != nil {
		return nil, err
	}

	return base, nil
}

// createVM creates a new VM instance with the provided configuration.
// It sets up modules, libraries, globals, and functions, then wraps the VM
// with necessary execution layers for channels, async operations, and coroutines.
func createVM(cfg *Factory) (api.VM, error) {
	// Collect all options
	opts := make([]engine.Option, 0)

	// Add engine options first
	opts = append(opts, cfg.EngineOpts...)

	// Add modules
	for _, module := range cfg.Modules {
		// todo: with preloaded?
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

	internal := []engine.Option{
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	}

	vm, err := engine.NewCVM(cfg.Logger, append(internal, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CoroutineVM: %w", err)
	}

	// Add bound functions via options
	for _, fn := range cfg.Functions {
		if err := vm.Import(fn.Script, fn.Name, fn.Name); err != nil {
			vm.Close()
			return nil, fmt.Errorf("failed to compile function %q: %w", fn.Name, err)
		}
	}

	channels := channel.NewChannelLayer()

	// wrapping into execution layer
	wrap := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(async.NewAsyncLayer(channels, 4096)),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)

	return wrap, nil
}

// todo: merge with cvm factory
// todo:
