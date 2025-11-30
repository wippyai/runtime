package evalhost

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/eval"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

// Host provides eval compilation and execution services.
type Host struct {
	log            *zap.Logger
	compiler       *Compiler
	processFactory process2.Factory
}

// NewHost creates a new eval host.
func NewHost(log *zap.Logger, modules []lua2api.Module, processFactory process2.Factory) *Host {
	return &Host{
		log:            log,
		compiler:       NewCompiler(modules),
		processFactory: processFactory,
	}
}

// Compile compiles Lua source into a reusable Program.
func (h *Host) Compile(ctx context.Context, cmd eval.CompileCmd) (eval.Program, error) {
	program, err := h.compiler.Compile(cmd)
	if err != nil {
		return nil, fmt.Errorf("compile failed: %w", err)
	}
	return program, nil
}

// Run compiles and executes Lua code.
// This is a placeholder - actual execution happens through the dispatcher.
func (h *Host) Run(ctx context.Context, cmd eval.RunCmd) (any, error) {
	return nil, fmt.Errorf("Run must be called through dispatcher")
}

// CreateProcess creates a process from a Program for sandbox use.
func (h *Host) CreateProcess(ctx context.Context, program eval.Program) (process2.Process, error) {
	prog, ok := program.(*Program)
	if !ok {
		return nil, fmt.Errorf("invalid program type")
	}

	// Get module binder for the allowed modules
	binder := h.compiler.GetModuleBinder(prog.Modules())

	// Create process with proto and module binder
	proc := engine.NewProcess(
		engine.WithProto(prog.Proto()),
		engine.WithModuleBinder(binder),
	)

	if err := proc.Init(); err != nil {
		return nil, fmt.Errorf("failed to init process: %w", err)
	}

	return proc, nil
}

// CreateProcessFromID creates a process from a prototype ID.
func (h *Host) CreateProcessFromID(ctx context.Context, id registry.ID) (process2.Process, error) {
	if h.processFactory == nil {
		return nil, fmt.Errorf("process factory not available")
	}

	proc, err := h.processFactory.Create(id)
	if err != nil {
		return nil, fmt.Errorf("failed to create process from ID %s: %w", id, err)
	}

	return proc, nil
}

// GetCompiler returns the compiler for direct use.
func (h *Host) GetCompiler() *Compiler {
	return h.compiler
}
