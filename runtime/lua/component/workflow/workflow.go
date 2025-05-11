package workflow

import (
	"context"
	"sync"

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

	// Command management fallback (for backwards compatibility)
	commandsMu sync.Mutex
	commands   []runtime.Command
}

// NewLuaWorkflow creates a new workflow instance
func NewLuaWorkflow(log *zap.Logger, runner *engine.Runner, funcName string) (process.Process, error) {
	// Create state directly
	state, err := baseprocess.NewState(log, runner, funcName)
	if err != nil {
		return nil, err
	}

	return &LuaWorkflow{
		state:    state,
		commands: make([]runtime.Command, 0),
	}, nil
}

// Start initializes and starts the workflow
func (w *LuaWorkflow) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	// Initialize the process state
	if err := w.state.InitContext(ctx, pid); err != nil {
		return err
	}

	// Initialize the command queue in UnitOfWork
	if w.state.UoW != nil {
		// Create a new queue and store it
		queue := NewCommandQueue()
		w.state.UoW.Values().Set(CommandQueueKey, queue)
	}

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
	// todo: iterate internally? for while ready
	// Then delegate to the state
	return w.state.Step(false)
}

// Ready returns the number of tasks ready to be processed
func (w *LuaWorkflow) Ready() int {
	// Get command count from queue or local storage
	var commandCount int

	if w.state != nil && w.state.UoW != nil {
		queue := GetCommandQueue(w.state.UoW)
		if queue != nil {
			commandCount = queue.Count()
		} else {
			w.commandsMu.Lock()
			commandCount = len(w.commands)
			w.commandsMu.Unlock()
		}
	} else {
		w.commandsMu.Lock()
		commandCount = len(w.commands)
		w.commandsMu.Unlock()
	}

	// Add the command count to the state's ready count
	return commandCount + w.state.GetTaskCount()
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
	// First try to use the shared command queue if available
	if w.state != nil && w.state.UoW != nil {
		queue := GetCommandQueue(w.state.UoW)
		if queue != nil {
			return queue.GetAll()
		}
	}

	// Fall back to local command management if no shared queue
	w.commandsMu.Lock()
	defer w.commandsMu.Unlock()

	// Return a copy to prevent external modification
	result := make([]runtime.Command, len(w.commands))
	copy(result, w.commands)
	return result
}

// AddCommand adds a command to the pipeline
func (w *LuaWorkflow) AddCommand(cmd runtime.Command) {
	// First try to use the shared command queue if available
	if w.state != nil && w.state.UoW != nil {
		queue := GetCommandQueue(w.state.UoW)
		if queue != nil {
			queue.Push(cmd)

			// Wake up the unit of work to process the new command
			w.state.UoW.Tasks().WakeUp()
			return
		}
	}

	// Fall back to local command management if no shared queue
	w.commandsMu.Lock()
	defer w.commandsMu.Unlock()

	w.commands = append(w.commands, cmd)

	// Wake up the unit of work to process the new command
	if w.state != nil && w.state.UoW != nil {
		w.state.UoW.Tasks().WakeUp()
	}
}
