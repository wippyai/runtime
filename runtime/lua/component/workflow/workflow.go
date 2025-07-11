package workflow

import (
	"context"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	baseprocess "github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// LuaWorkflow implements the process.Workflow interface
type LuaWorkflow struct {
	// State contains all state data and utility methods
	state *baseprocess.State
	queue *CommandQueue
}

// NewLuaWorkflow creates a new workflow instance
func NewLuaWorkflow(log *zap.Logger, runner *engine.Runner, funcName string) (process.Process, error) {
	// Create state directly
	state, err := baseprocess.NewState(log, runner, funcName)
	if err != nil {
		return nil, err
	}

	return &LuaWorkflow{
		state: state,
		queue: NewCommandQueue(),
	}, nil
}

// Start initializes and starts the workflow
func (w *LuaWorkflow) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	// Initialize the process state
	if err := w.state.InitContext(ctx, pid); err != nil {
		return err
	}

	w.state.UoW.Values().Set(commandQueueKey, w.queue)

	// Get the onStart callback for notification
	onStart := process.GetOnStart(w.state.Ctx)
	onStartFunc := func() {
		if onStart != nil {
			onStart(pid, w)
		}
	}

	// Serve the process using the state
	return w.state.Start(input, onStartFunc)
}

// Step advances the workflow state by one iteration
func (w *LuaWorkflow) Step() error {
	return w.state.Step(false)
}

// Ready returns the number of tasks ready to be processed
func (w *LuaWorkflow) Ready() int {
	return w.state.GetTaskCount()
}

// Send handles incoming messages
func (w *LuaWorkflow) Send(pkg *pubsub.Package) error {
	return w.state.SendPackage(pkg)
}

// Terminate stops the workflow
func (w *LuaWorkflow) Terminate() {
	w.state.Complete(process.ErrTerminated, lua.LNil)
}

// IsClosed returns whether the workflow has completed
func (w *LuaWorkflow) IsClosed() bool {
	return w.state.Closed.Load()
}

// Commands returns the current command pipeline
func (w *LuaWorkflow) Commands() []runtime.Command {
	return w.queue.Flush()
}
