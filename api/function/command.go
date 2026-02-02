package function

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime"
)

func init() {
	dispatcher.MustRegisterCommands("function", Call, AsyncStart, AsyncCancel)
}

// Command IDs for function operations.
const (
	Call        dispatcher.CommandID = 140 // Execute function call (sync, blocking)
	AsyncStart  dispatcher.CommandID = 141 // Start async call, result sent via relay
	AsyncCancel dispatcher.CommandID = 142 // Cancel an async call
)

// CallCmd represents a function call to be executed.
type CallCmd struct {
	Task runtime.Task
}

var callCmdPool = sync.Pool{New: func() any { return &CallCmd{} }}

// AcquireCallCmd returns a pooled CallCmd.
func AcquireCallCmd() *CallCmd                 { return callCmdPool.Get().(*CallCmd) }
func (c *CallCmd) CmdID() dispatcher.CommandID { return Call }
func (c *CallCmd) Release() {
	c.Task = runtime.Task{}
	callCmdPool.Put(c)
}

// CallResult represents the result of a function call.
type CallResult struct {
	Value any
	Error error
}

// AsyncStartCmd starts an async function call.
// Topic identifies the channel where result will be sent via relay.
type AsyncStartCmd struct {
	Topic string
	Task  runtime.Task
}

var asyncStartCmdPool = sync.Pool{New: func() any { return &AsyncStartCmd{} }}

// AcquireAsyncStartCmd returns a pooled AsyncStartCmd.
func AcquireAsyncStartCmd() *AsyncStartCmd           { return asyncStartCmdPool.Get().(*AsyncStartCmd) }
func (c *AsyncStartCmd) CmdID() dispatcher.CommandID { return AsyncStart }
func (c *AsyncStartCmd) Release() {
	c.Task = runtime.Task{}
	c.Topic = ""
	asyncStartCmdPool.Put(c)
}

// AsyncStartResult returned by AsyncStartCmd.
type AsyncStartResult struct {
	Error error
}

// AsyncCancelCmd cancels an ongoing async call.
type AsyncCancelCmd struct {
	Topic string
}

var asyncCancelCmdPool = sync.Pool{New: func() any { return &AsyncCancelCmd{} }}

// AcquireAsyncCancelCmd returns a pooled AsyncCancelCmd.
func AcquireAsyncCancelCmd() *AsyncCancelCmd          { return asyncCancelCmdPool.Get().(*AsyncCancelCmd) }
func (c *AsyncCancelCmd) CmdID() dispatcher.CommandID { return AsyncCancel }
func (c *AsyncCancelCmd) Release() {
	c.Topic = ""
	asyncCancelCmdPool.Put(c)
}
