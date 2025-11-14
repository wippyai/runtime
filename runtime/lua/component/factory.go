package component

import (
	"fmt"
	"sync"

	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// RunnerFactory creates and manages VM instances with consistent configuration
type RunnerFactory struct {
	log           *zap.Logger
	compiled      *code.CompiledMain
	engineOptions []engine.Option
	runnerOptions []engine.RunnerOption
	mu            sync.RWMutex
}

// Option configures the RunnerFactory
type Option func(*RunnerFactory)

// WithRunnerOption adds a runner option to the factory
func WithRunnerOption(opt ...engine.RunnerOption) Option {
	return func(f *RunnerFactory) {
		f.runnerOptions = append(f.runnerOptions, opt...)
	}
}

// WithEngineOption adds an engine option to the factory
func WithEngineOption(opt ...engine.Option) Option {
	return func(f *RunnerFactory) {
		f.engineOptions = append(f.engineOptions, opt...)
	}
}

// WithGlobal adds a global variable to the factory
func WithGlobal(name string, value lua.LValue) Option {
	return func(f *RunnerFactory) {
		f.engineOptions = append(f.engineOptions, engine.WithGlobalValue(name, value))
	}
}

// NewRunnerFactory creates and prepares a new RunnerFactory instance
func NewRunnerFactory(log *zap.Logger, compiled *code.CompiledMain, opts ...Option) (*RunnerFactory, error) {
	if log == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}
	if compiled == nil {
		return nil, fmt.Errorf("compiled code cannot be nil")
	}

	f := &RunnerFactory{
		log:           log,
		compiled:      compiled,
		engineOptions: make([]engine.Option, 0),
		runnerOptions: make([]engine.RunnerOption, 0),
	}

	// Apply options
	for _, opt := range opts {
		opt(f)
	}

	// Prepare immediately
	if err := f.prepare(); err != nil {
		return nil, fmt.Errorf("factory preparation failed: %w", err)
	}

	return f, nil
}

func (f *RunnerFactory) Compile() error {
	_, err := f.CreateVM()
	return err
}

func (f *RunnerFactory) CreateVM() (api.VM, error) {
	return f.CreateRunner()
}

// CreateRunner creates a new VM instance with the current configuration
func (f *RunnerFactory) CreateRunner() (*engine.Runner, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Spawn base VM with cached options
	vm, err := engine.NewCVM(f.log, f.engineOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create base VM: %w", err)
	}

	// Load main function
	if err := vm.Mount(f.compiled.Main, f.compiled.FuncName); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to load main function: %w", err)
	}

	runnerOpts := make([]engine.RunnerOption, len(f.runnerOptions))
	copy(runnerOpts, f.runnerOptions)

	return engine.NewRunner(vm, runnerOpts...), nil
}

// prepare sets up the factory's engine options with dependencies
func (f *RunnerFactory) prepare() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Process dependencies
	var opts []engine.Option
	for _, dep := range f.compiled.Preloaded {
		if dep.Node == nil {
			continue
		}

		if dep.Node.Module != nil {
			opts = append(opts, engine.WithPreloaded(dep.Name, dep.Node.Module.Loader))
		}
	}

	for _, dep := range f.compiled.Dependencies {
		if dep.Node == nil {
			continue
		}

		switch dep.Node.Kind {
		case api.KindModule:
			if dep.Node.Module != nil {
				opts = append(opts, engine.WithLoader(dep.Name, dep.Node.Module.Loader))
			}
		case api.KindLibrary, api.KindFunction, api.KindProcess:
			if dep.Proto != nil {
				opts = append(opts, engine.WithLibrary(dep.Name, dep.Proto))
			} else if dep.Node.Source != "" {
				opts = append(opts, engine.WithLibrary(dep.Name, dep.Node.Source))
			}
		}
	}

	// AddCleanup prepared dependency options to existing engine options
	f.engineOptions = append(f.engineOptions, opts...)
	return nil
}

// Close performs any necessary cleanup
func (f *RunnerFactory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.engineOptions = nil
	f.runnerOptions = nil
	return nil
}
