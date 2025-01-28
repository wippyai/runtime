package command

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"sync/atomic"
)

// Type represents the type of command operation
type Type string

var (
	ErrCommandCanceled = errors.New("command canceled")
	commandCounter     atomic.Uint64
)

// NewCommand creates a new command with a response channel
func NewCommand(cmdType Type, params ...lua.LValue) (*Command, error) {
	if cmdType == "" {
		return nil, fmt.Errorf("command type cannot be empty")
	}

	// Generate unique channel name using atomic counter
	uniqueName := fmt.Sprintf("cmd.%s.%d", cmdType, commandCounter.Add(1))

	// Create response channel with capacity 1 for the single response
	ch := channel.Named(uniqueName, 1)
	if ch == nil {
		return nil, fmt.Errorf("failed to create response channel")
	}

	return &Command{
		cmdType:     cmdType,
		Params:      params,
		response:    ch,
		responseVal: ch.Value(),
	}, nil
}

// Command represents an async operation request
type Command struct {
	cmdType     Type
	Params      []lua.LValue
	response    *channel.Channel // Actual response channel
	responseVal lua.LValue       // Lua channel value representation
	result      lua.LValue
	err         error
	completed   bool
}

// IsComplete returns whether the command has completed (success or failure)
func (c *Command) IsComplete() bool {
	return c.completed
}

// Result returns the command's result value and any error
func (c *Command) Result() (lua.LValue, error) {
	if !c.completed {
		return nil, fmt.Errorf("command not completed")
	}
	return c.result, c.err
}

// Err returns just the error if any occurred
func (c *Command) Err() error {
	return c.err
}

// SetResult marks command as completed with a result
func (c *Command) SetResult(result lua.LValue) {
	c.result = result
	c.completed = true
}

// SetError marks command as failed with an error
func (c *Command) SetError(err error) {
	c.err = err
	c.completed = true
}

// Cancel marks command as canceled
func (c *Command) Cancel() {
	c.SetError(ErrCommandCanceled)
}
