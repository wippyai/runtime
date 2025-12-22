package evalhost

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

// Note: The Compiler caches module definitions but has no persistent resources.
// All process lifecycle is managed by frame context cleanup.

// Host provides eval compilation and execution services.
type Host struct {
	log      *zap.Logger
	compiler *Compiler
}

// NewHost creates a new eval host.
func NewHost(log *zap.Logger, modules []*luaapi.ModuleDef) *Host {
	return &Host{
		log:      log,
		compiler: NewCompiler(modules),
	}
}

// Compile compiles Lua source into a reusable Program.
func (h *Host) Compile(_ context.Context, cmd CompileCmd) (*Program, error) {
	program, err := h.compiler.Compile(cmd)
	if err != nil {
		return nil, NewCompileError(err)
	}
	return program, nil
}

// Run compiles and executes Lua source in one step.
func (h *Host) Run(ctx context.Context, cmd RunCmd) (any, error) {
	// Compile the source
	program, err := h.compiler.Compile(CompileCmd{
		Source:  cmd.Source,
		Method:  cmd.Method,
		Modules: cmd.Modules,
	})
	if err != nil {
		return nil, NewCompileError(err)
	}

	// Create and run the process
	binder := h.compiler.GetModuleBinder(program.Modules())
	factory := engine.NewFactoryFromProto(program.Proto(), binder)
	proc, err := factory()
	if err != nil {
		return nil, NewCreateProcessError(err)
	}
	defer proc.Close()

	// Create fresh frame context for the eval process
	evalCtx, fc := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)

	// Apply caller-provided context values
	if len(cmd.Context) > 0 {
		values := attrs.NewBagFrom(cmd.Context)
		if err := ctxapi.SetValues(evalCtx, values); err != nil {
			return nil, NewRunError(err)
		}
	}

	// Initialize with the method and arguments
	if err := proc.Init(evalCtx, cmd.Method, cmd.Args); err != nil {
		return nil, NewRunError(err)
	}

	// Step until done
	var output process.StepOutput
	for {
		// Check for context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return nil, NewRunError(err)
		}

		if output.IsDone() {
			result := output.Result()
			if result == nil {
				return nil, ErrNoResult
			}
			return result.Data(), nil
		}

		if output.IsIdle() {
			return nil, ErrProcessIdle
		}
	}
}

// GetCompiler returns the compiler for direct use.
func (h *Host) GetCompiler() *Compiler {
	return h.compiler
}
