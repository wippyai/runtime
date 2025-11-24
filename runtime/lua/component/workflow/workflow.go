package workflow

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	baseprocess "github.com/wippyai/runtime/runtime/lua/component/process"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// LuaWorkflow wraps a Lua process state for workflow execution
type LuaWorkflow struct {
	*baseprocess.State
	log *zap.Logger
}

// NewLuaWorkflow creates a new Lua workflow from a runner
func NewLuaWorkflow(log *zap.Logger, state *baseprocess.State) *LuaWorkflow {
	return &LuaWorkflow{
		State: state,
		log:   log,
	}
}

// Commands returns all pending requests from the upstream handler in context
func (w *LuaWorkflow) Commands() []runtime.Command {
	if w.UoW == nil {
		return nil
	}

	ctx := w.UoW.Context()
	upstream, ok := runtime.GetUpstream(ctx)
	if !ok {
		return nil
	}

	return upstream.FlushRequests()
}

// Start initializes the workflow with context, PID, and input payloads
func (w *LuaWorkflow) Start(ctx context.Context, pid relay.PID, input payload.Payloads) error {
	if err := w.InitContext(ctx, pid); err != nil {
		return err
	}

	return w.State.Start(input, nil)
}

// Step advances the workflow state by one iteration
func (w *LuaWorkflow) Step() (process.StepResult, error) {
	err := w.State.Step(false)

	if err != nil {
		if errors.Is(err, supervisor.ErrExit) {
			return process.StepDone, nil
		}
		return process.StepContinue, err
	}

	if w.GetTaskCount() > 0 {
		return process.StepContinue, nil
	}

	return process.StepIdle, nil
}

// Send implements relay.Receiver by delegating to State.SendPackage
func (w *LuaWorkflow) Send(pkg *relay.Package) error {
	return w.SendPackage(pkg)
}

// Terminate implements process.Process
func (w *LuaWorkflow) Terminate() {
	w.Complete(process.ErrTerminated, lua.LNil)
}
