// Package funcapi provides function call command types for the dispatcher system.
package funcapi

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime"
)

func init() {
	dispatcher.MustRegisterCommands("func",
		CmdCall, CmdAsyncStart, CmdAsyncAwait, CmdAsyncCancel,
	)
}

// Command IDs for function operations.
// Range 200-209 is reserved for function commands (exec uses 210-219).
const (
	CmdCall dispatcher.CommandID = 200 // Execute function call (sync, blocking)

	// Async function calls - decomposed one-shot pattern like ticker
	CmdAsyncStart  dispatcher.CommandID = 201 // Start async call, returns callID
	CmdAsyncAwait  dispatcher.CommandID = 202 // Wait for call to complete, returns result
	CmdAsyncCancel dispatcher.CommandID = 203 // Cancel an async call
)

// CallCmd represents a function call to be executed.
type CallCmd struct {
	Task runtime.Task
}

var callCmdPool = sync.Pool{New: func() any { return &CallCmd{} }}

func AcquireCallCmd() *CallCmd                 { return callCmdPool.Get().(*CallCmd) }
func (c *CallCmd) CmdID() dispatcher.CommandID { return CmdCall }
func (c *CallCmd) Release() {
	c.Task = runtime.Task{}
	callCmdPool.Put(c)
}

// Response represents the result of a function call.
type Response struct {
	Value any
	Error error
}

// AsyncStartCmd starts an async function call.
type AsyncStartCmd struct {
	Task runtime.Task
}

var asyncStartCmdPool = sync.Pool{New: func() any { return &AsyncStartCmd{} }}

func AcquireAsyncStartCmd() *AsyncStartCmd           { return asyncStartCmdPool.Get().(*AsyncStartCmd) }
func (c *AsyncStartCmd) CmdID() dispatcher.CommandID { return CmdAsyncStart }
func (c *AsyncStartCmd) Release() {
	c.Task = runtime.Task{}
	asyncStartCmdPool.Put(c)
}

// AsyncAwaitCmd waits for an async call to complete.
type AsyncAwaitCmd struct {
	CallID uint64
}

var asyncAwaitCmdPool = sync.Pool{New: func() any { return &AsyncAwaitCmd{} }}

func AcquireAsyncAwaitCmd() *AsyncAwaitCmd           { return asyncAwaitCmdPool.Get().(*AsyncAwaitCmd) }
func (c *AsyncAwaitCmd) CmdID() dispatcher.CommandID { return CmdAsyncAwait }
func (c *AsyncAwaitCmd) Release() {
	c.CallID = 0
	asyncAwaitCmdPool.Put(c)
}

// AsyncCancelCmd cancels an ongoing async call.
type AsyncCancelCmd struct {
	CallID uint64
}

var asyncCancelCmdPool = sync.Pool{New: func() any { return &AsyncCancelCmd{} }}

func AcquireAsyncCancelCmd() *AsyncCancelCmd          { return asyncCancelCmdPool.Get().(*AsyncCancelCmd) }
func (c *AsyncCancelCmd) CmdID() dispatcher.CommandID { return CmdAsyncCancel }
func (c *AsyncCancelCmd) Release() {
	c.CallID = 0
	asyncCancelCmdPool.Put(c)
}

// AsyncStartResponse returned by AsyncStartCmd.
type AsyncStartResponse struct {
	CallID uint64
	Error  error
}

// AsyncAwaitResponse returned by AsyncAwaitCmd.
type AsyncAwaitResponse struct {
	Value     any
	Error     error
	Cancelled bool
}
