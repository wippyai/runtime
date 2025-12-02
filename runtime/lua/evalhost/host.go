package evalhost

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	lua2api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

// Host provides eval compilation and execution services.
type Host struct {
	log            *zap.Logger
	compiler       *Compiler
	processFactory process.Factory
}

// NewHost creates a new eval host.
func NewHost(log *zap.Logger, modules []lua2api.ModuleV2, processFactory process.Factory) *Host {
	return &Host{
		log:            log,
		compiler:       NewCompiler(modules),
		processFactory: processFactory,
	}
}

// Compile compiles Lua source into a reusable Program.
func (h *Host) Compile(ctx context.Context, cmd CompileCmd) (*Program, error) {
	program, err := h.compiler.Compile(cmd)
	if err != nil {
		return nil, fmt.Errorf("compile failed: %w", err)
	}
	return program, nil
}

// CreateProcess creates a process from a Program for sandbox use.
func (h *Host) CreateProcess(ctx context.Context, program *Program) (process.Process, error) {
	// Get module binder for the allowed modules
	binder := h.compiler.GetModuleBinder(program.Modules())

	// Create factory for this program and use it to create process
	factory := engine.NewFactoryFromProto(program.Proto(), binder)
	proc, err := factory()
	if err != nil {
		return nil, fmt.Errorf("failed to create process: %w", err)
	}

	return proc, nil
}

// CreateProcessFromID creates a process from a prototype ID.
func (h *Host) CreateProcessFromID(ctx context.Context, id registry.ID) (process.Process, error) {
	if h.processFactory == nil {
		return nil, fmt.Errorf("process factory not available")
	}

	proc, _, err := h.processFactory.Create(id)
	if err != nil {
		return nil, fmt.Errorf("failed to create process from ID %s: %w", id, err)
	}

	return proc, nil
}

// GetCompiler returns the compiler for direct use.
func (h *Host) GetCompiler() *Compiler {
	return h.compiler
}
