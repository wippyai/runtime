package factory

import (
	"fmt"
	"sync"

	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
)

type (
	// RunnerFactory creates and manages VM instances with consistent configuration
	RunnerFactory struct {
		log           *zap.Logger
		compiled      *code.CompiledMain
		config        BuildConfig
		engineOptions []engine.Option // Cached engine options
		mu            sync.RWMutex
		prepareOnce   sync.Once
		prepared      bool
	}

	// BuildConfig defines all configuration parameters for VM creation
	BuildConfig struct {
		runnerOptions []engine.RunnerOption
		EngineOptions []engine.Option
	}
)

// DefaultBuildConfig returns a BuildConfig with default values
func DefaultBuildConfig() BuildConfig {
	return BuildConfig{
		runnerOptions: make([]engine.RunnerOption, 0),
		EngineOptions: make([]engine.Option, 0),
	}
}

// New creates a new RunnerFactory instance
func New(log *zap.Logger, compiled *code.CompiledMain, cfg BuildConfig) (*RunnerFactory, error) {
	if log == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}
	if compiled == nil {
		return nil, fmt.Errorf("compiled code cannot be nil")
	}

	return &RunnerFactory{
		log:      log,
		compiled: compiled,
		config:   cfg,
	}, nil
}

// CreateRunner creates a new VM instance with the current configuration
func (f *RunnerFactory) CreateRunner() (*engine.Runner, error) {
	var prepareErr error
	f.prepareOnce.Do(func() {
		prepareErr = f.prepare()
	})
	if prepareErr != nil {
		return nil, fmt.Errorf("preparation failed: %w", prepareErr)
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	// Create base VM with cached options
	vm, err := engine.NewCVM(f.log, f.engineOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create base VM: %w", err)
	}

	// Load main function
	if err := vm.Mount(f.compiled.Main, f.compiled.Method); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to load main function: %w", err)
	}

	return engine.NewRunner(vm, f.config.runnerOptions...), nil
}

// prepare caches necessary components for VM creation (private implementation)
func (f *RunnerFactory) prepare() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Build and cache engine options
	var opts []engine.Option

	// Process dependencies first
	for _, dep := range f.compiled.Dependencies {
		if dep.Node == nil {
			continue
		}

		switch dep.Node.Kind {
		case api.KindModule:
			if dep.Node.Module != nil {
				opts = append(opts, engine.WithLoader(dep.Name, dep.Node.Module.Loader))
			}
		case api.KindLibrary, api.KindFunction:
			if dep.Proto != nil {
				opts = append(opts, engine.WithLibrary(dep.Name, dep.Proto))
			} else if dep.Node.Source != "" {
				opts = append(opts, engine.WithLibrary(dep.Name, dep.Node.Source))
			}
		}
	}

	// Cache the options
	f.engineOptions = append(f.config.EngineOptions, opts...)
	f.prepared = true

	return nil
}

// Compile prepares the Lua code for execution
func (f *RunnerFactory) Compile() error {
	var prepareErr error
	f.prepareOnce.Do(func() {
		prepareErr = f.prepare()
	})
	return prepareErr
}

// Close performs any necessary cleanup
func (f *RunnerFactory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Reset cached state
	f.engineOptions = nil
	f.prepared = false
	return nil
}
