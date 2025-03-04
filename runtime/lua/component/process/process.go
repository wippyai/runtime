package process

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// LuaProcess represents a Lua process instance that uses a State under the hood
type LuaProcess struct {
	// State contains all state data and utility methods
	state *State
}

// NewLuaProcess creates a new Lua process instance
func NewLuaProcess(log *zap.Logger, runner *engine.Runner, funcName string) (process.Process, error) {
	state, err := NewState(log, runner, funcName)
	if err != nil {
		return nil, err
	}

	return &LuaProcess{
		state: state,
	}, nil
}

// Start initializes and starts the Lua process
func (p *LuaProcess) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	// Initialize the process state
	if err := p.state.InitContext(ctx, pid); err != nil {
		return err
	}

	// Get the onStart callback for notification
	onStart := process.GetOnStart(p.state.Ctx)
	onStartFunc := func() {
		if onStart != nil {
			onStart(pid, p)
		}
	}

	// Start the process using the state
	return p.state.Start(input, onStartFunc)
}

// Step advances the process state by one iteration
func (p *LuaProcess) Step() error {
	return p.state.Step()
}

// Ready returns the size of the runner's queue that is ready to be processed.
func (p *LuaProcess) Ready() int {
	return p.state.GetTaskCount()
}

// Send handles incoming messages to the process
func (p *LuaProcess) Send(pkg *pubsub.Package) error {
	return p.state.ProcessPackage(pkg)
}

// Terminate forcefully stops the process
func (p *LuaProcess) Terminate() {
	// Use a nil result to indicate forced termination
	p.state.Complete(process.ErrTerminated, lua.LNil)
}

// SetTrapExit controls whether the process will trap exit signals from linked processes
func (p *LuaProcess) SetTrapExit(trap bool) {
	p.state.SetTrapLinks(trap)
}

// IsClosed returns whether the process has completed execution
func (p *LuaProcess) IsClosed() bool {
	return p.state.Closed.Load()
}
