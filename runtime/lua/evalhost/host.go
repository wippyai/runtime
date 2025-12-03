package evalhost

import (
	"context"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
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
func NewHost(log *zap.Logger, modules []luaapi.ModuleV2, processFactory process.Factory) *Host {
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
		return nil, NewCompileError(err)
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
		return nil, NewCreateProcessError(err)
	}

	return proc, nil
}

// CreateProcessFromID creates a process from a prototype ID.
func (h *Host) CreateProcessFromID(ctx context.Context, id registry.ID) (process.Process, error) {
	if h.processFactory == nil {
		return nil, ErrProcessFactoryNotAvailable
	}

	proc, _, err := h.processFactory.Create(id)
	if err != nil {
		return nil, NewCreateProcessFromIDError(id.String(), err)
	}

	return proc, nil
}

// GetCompiler returns the compiler for direct use.
func (h *Host) GetCompiler() *Compiler {
	return h.compiler
}
