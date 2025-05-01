package command

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

var (
	ErrCommandCompleted = errors.New("command already completed")

	ErrCommandCanceled = errors.New("command canceled")

	commandCounter atomic.Uint64
)

// Command represents an asynchronous operation
type Command struct {
	// Command identifier
	id      string
	cmdType runtime.Type

	// Input parameters
	params []payload.Payload

	// Internal state
	mu        sync.Mutex
	completed bool
	canceled  bool
	result    *runtime.Result

	// Channel-related fields
	responseChannel *channel.Channel
	channelValue    lua.LValue
	unitOfWork      engine.UnitOfWork

	// Callback for cancellation
	onCancel runtime.Canceller
}

// NewCommand creates a new command
func NewCommand(l *lua.LState, cmdType runtime.Type, onCancel runtime.Canceller, params ...payload.Payload) *Command {
	id := fmt.Sprintf("cmd.%s.%d", cmdType, commandCounter.Add(1))

	// Get unit of work from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return nil
	}

	// Create response channel
	chanName := fmt.Sprintf("cmd.%s.%d", cmdType, commandCounter.Load())
	respChan := channel.Named(chanName, 1)
	respValue := channel.Wrap(l, respChan)

	return &Command{
		id:              id,
		cmdType:         cmdType,
		params:          params,
		responseChannel: respChan,
		channelValue:    respValue,
		unitOfWork:      uw,
		onCancel:        onCancel,
	}
}

// ID returns the command's ID
func (c *Command) ID() runtime.ID {
	return c.id
}

// Type returns the command's type
func (c *Command) Type() runtime.Type {
	return c.cmdType
}

func (c *Command) Params() payload.Payloads {
	return c.params
}

// Result returns the command's result
func (c *Command) Result() *runtime.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

// Complete implements the Command interface, completing the command with a result
func (c *Command) Complete(result *runtime.Result) error {
	c.mu.Lock()

	if c.completed || c.canceled {
		c.mu.Unlock()
		return ErrCommandCompleted
	}

	c.completed = true
	c.result = result

	// Get a local reference to the channel
	respChan := c.responseChannel
	state := c.unitOfWork.State()

	c.mu.Unlock()

	if result.Error != nil {
		return channel.Close(state, respChan)
	}

	err := channel.Send(state, respChan, payloadmod.WrapPayload(state, result.Value))
	if err != nil {
		return err
	}

	return channel.Close(state, respChan)
}

// Cancel cancels the command
func (c *Command) Cancel() error {
	c.mu.Lock()

	if c.completed {
		c.mu.Unlock()
		return ErrCommandCompleted
	}

	if c.canceled {
		c.mu.Unlock()
		return nil // Already canceled
	}

	c.canceled = true
	c.result = &runtime.Result{
		Value: nil,
		Error: ErrCommandCanceled,
	}

	// Get local references
	respChan := c.responseChannel

	c.mu.Unlock()

	if c.onCancel != nil {
		c.onCancel(c)
	}

	return channel.Close(c.unitOfWork.State(), respChan)
}

// IsCompleted checks if the command has completed
func (c *Command) isCompleted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.completed || c.canceled
}

// IsCanceled checks if the command was canceled
func (c *Command) isCanceled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.canceled
}
