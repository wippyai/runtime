package contract

import (
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
)

func init() {
	dispatcher.MustRegisterCommands("contract",
		CmdOpen, CmdCall, CmdAsyncCall, CmdAsyncCancel,
	)
}

// Command IDs for contract operations.
const (
	CmdOpen        dispatcher.CommandID = 300 // Open binding, get instance
	CmdCall        dispatcher.CommandID = 301 // Call method on instance (sync)
	CmdAsyncCall   dispatcher.CommandID = 302 // Call method on instance (async)
	CmdAsyncCancel dispatcher.CommandID = 303 // Cancel async call
)

// OpenCmd opens a contract binding and returns an instance.
type OpenCmd struct {
	BindingID     registry.ID
	Scope         attrs.Bag         // Context values for the instance
	Values        contextapi.Values // Chained context values
	Actor         secapi.Actor      // Security actor
	HasActor      bool              // Whether actor is set
	SecurityScope secapi.Scope      // Security scope
	HasScope      bool              // Whether scope is set
}

var openCmdPool = sync.Pool{New: func() any { return &OpenCmd{} }}

func AcquireOpenCmd() *OpenCmd                 { return openCmdPool.Get().(*OpenCmd) }
func (c *OpenCmd) CmdID() dispatcher.CommandID { return CmdOpen }
func (c *OpenCmd) Release() {
	c.BindingID = registry.ID{}
	c.Scope = nil
	c.Values = nil
	c.Actor = secapi.Actor{}
	c.HasActor = false
	c.SecurityScope = nil
	c.HasScope = false
	openCmdPool.Put(c)
}

// OpenResult is returned by OpenCmd.
type OpenResult struct {
	Instance Instance
	Error    error
}

// CallCmd calls a method on a contract instance.
type CallCmd struct {
	Instance Instance
	Method   string
	Args     payload.Payloads
}

var callCmdPool = sync.Pool{New: func() any { return &CallCmd{} }}

func AcquireCallCmd() *CallCmd                 { return callCmdPool.Get().(*CallCmd) }
func (c *CallCmd) CmdID() dispatcher.CommandID { return CmdCall }
func (c *CallCmd) Release() {
	c.Instance = nil
	c.Method = ""
	c.Args = nil
	callCmdPool.Put(c)
}

// CallResult is returned by CallCmd.
type CallResult struct {
	Value any
	Error error
}

// AsyncCallCmd calls a method asynchronously.
type AsyncCallCmd struct {
	Instance Instance
	Method   string
	Args     payload.Payloads
	Topic    string // Relay topic for result
}

var asyncCallCmdPool = sync.Pool{New: func() any { return &AsyncCallCmd{} }}

func AcquireAsyncCallCmd() *AsyncCallCmd            { return asyncCallCmdPool.Get().(*AsyncCallCmd) }
func (c *AsyncCallCmd) CmdID() dispatcher.CommandID { return CmdAsyncCall }
func (c *AsyncCallCmd) Release() {
	c.Instance = nil
	c.Method = ""
	c.Args = nil
	c.Topic = ""
	asyncCallCmdPool.Put(c)
}

// AsyncCallResult is returned by AsyncCallCmd to confirm start.
type AsyncCallResult struct {
	Error error
}

// AsyncCancelCmd cancels an ongoing async call.
type AsyncCancelCmd struct {
	Topic string
}

var asyncCancelCmdPool = sync.Pool{New: func() any { return &AsyncCancelCmd{} }}

func AcquireAsyncCancelCmd() *AsyncCancelCmd          { return asyncCancelCmdPool.Get().(*AsyncCancelCmd) }
func (c *AsyncCancelCmd) CmdID() dispatcher.CommandID { return CmdAsyncCancel }
func (c *AsyncCancelCmd) Release() {
	c.Topic = ""
	asyncCancelCmdPool.Put(c)
}
