package function

import (
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"go.uber.org/zap"
)

// Factory implements a stateless factory that compiles on every VM creation
type Factory struct {
	log       *zap.Logger
	code      *code.Manager
	id        registry.ID
	buildOpts *code.BuildOptions
	imports   []code.Import
	options   component.Option
}

// NewCompilerFactory creates a new stateless factory
func NewCompilerFactory(
	log *zap.Logger,
	code *code.Manager,
	id registry.ID,
	buildOpts *code.BuildOptions,
	imports []code.Import,
	options component.Option,
) *Factory {
	return &Factory{
		log:       log,
		code:      code,
		id:        id,
		buildOpts: buildOpts,
		imports:   imports,
		options:   options,
	}
}

// Compile is a no-op for stateless factory
func (f *Factory) Compile() error {
	// No-op - compilation happens on demand for each CreateVM call
	return nil
}

// CreateVM creates a new VM instance with fresh compilation every time
func (f *Factory) CreateVM() (api.VM, error) {
	// Compile the function on demand
	compiled, err := f.code.Compile(f.id, f.buildOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}

	// Create a factory for this specific VM
	realFactory, err := component.NewRunnerFactory(f.log, compiled, f.options)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	// Compile the factory
	if err := realFactory.Compile(); err != nil {
		return nil, fmt.Errorf("failed to compile runner: %w", err)
	}

	// Create and return VM using the factory
	vm, err := realFactory.CreateVM()
	if err != nil {
		// Close the factory even if VM creation failed
		if closeErr := realFactory.Close(); closeErr != nil {
			f.log.Warn("failed to close runner after VM creation error", zap.Error(closeErr))
		}
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	// Close the factory after successful VM creation
	if err := realFactory.Close(); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to close runner: %w", err)
	}

	return vm, nil
}
