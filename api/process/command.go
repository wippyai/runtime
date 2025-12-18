package process

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

func init() {
	dispatcher.MustRegisterCommands("process",
		Send, Spawn, Terminate, Cancel,
		Monitor, Unmonitor, Link, Unlink,
		Call,
	)
}

// Command IDs for process operations.
const (
	Send      dispatcher.CommandID = 1 // Send message to process
	Spawn     dispatcher.CommandID = 2 // Spawn new process
	Terminate dispatcher.CommandID = 3 // Terminate process
	Cancel    dispatcher.CommandID = 4 // Cancel process with deadline
	Monitor   dispatcher.CommandID = 5 // Monitor process
	Unmonitor dispatcher.CommandID = 6 // Stop monitoring process
	Link      dispatcher.CommandID = 7 // Link to process
	Unlink    dispatcher.CommandID = 8 // Unlink from process
	Call      dispatcher.CommandID = 9 // Call process and wait for result
)

// SendCmd sends a message to a process.
type SendCmd struct {
	From     pid.PID
	To       pid.PID
	Topic    string
	Payloads payload.Payloads
}

var sendCmdPool = sync.Pool{New: func() any { return &SendCmd{} }}

func AcquireSendCmd() *SendCmd                 { return sendCmdPool.Get().(*SendCmd) }
func (c *SendCmd) CmdID() dispatcher.CommandID { return Send }
func (c *SendCmd) Release() {
	c.From = pid.PID{}
	c.To = pid.PID{}
	c.Topic = ""
	c.Payloads = nil
	sendCmdPool.Put(c)
}

// SendResult is the result of a send operation.
type SendResult struct {
	Error error
}

// SpawnCmd spawns a new process.
type SpawnCmd struct {
	Start   *Start
	Monitor bool
	Link    bool
}

var spawnCmdPool = sync.Pool{New: func() any { return &SpawnCmd{} }}

func AcquireSpawnCmd() *SpawnCmd                { return spawnCmdPool.Get().(*SpawnCmd) }
func (c *SpawnCmd) CmdID() dispatcher.CommandID { return Spawn }
func (c *SpawnCmd) Release() {
	c.Start = nil
	c.Monitor = false
	c.Link = false
	spawnCmdPool.Put(c)
}

// SpawnResult is the result of a spawn operation.
type SpawnResult struct {
	PID   pid.PID
	Error error
}

// TerminateCmd terminates a process.
type TerminateCmd struct {
	Target pid.PID
}

var terminateCmdPool = sync.Pool{New: func() any { return &TerminateCmd{} }}

func AcquireTerminateCmd() *TerminateCmd            { return terminateCmdPool.Get().(*TerminateCmd) }
func (c *TerminateCmd) CmdID() dispatcher.CommandID { return Terminate }
func (c *TerminateCmd) Release() {
	c.Target = pid.PID{}
	terminateCmdPool.Put(c)
}

// CancelCmd cancels a process with optional deadline.
type CancelCmd struct {
	From     pid.PID
	Target   pid.PID
	Deadline time.Time
}

var cancelCmdPool = sync.Pool{New: func() any { return &CancelCmd{} }}

func AcquireCancelCmd() *CancelCmd               { return cancelCmdPool.Get().(*CancelCmd) }
func (c *CancelCmd) CmdID() dispatcher.CommandID { return Cancel }
func (c *CancelCmd) Release() {
	c.From = pid.PID{}
	c.Target = pid.PID{}
	c.Deadline = time.Time{}
	cancelCmdPool.Put(c)
}

// MonitorCmd starts monitoring a process.
type MonitorCmd struct {
	Watcher pid.PID
	Target  pid.PID
}

var monitorCmdPool = sync.Pool{New: func() any { return &MonitorCmd{} }}

func AcquireMonitorCmd() *MonitorCmd              { return monitorCmdPool.Get().(*MonitorCmd) }
func (c *MonitorCmd) CmdID() dispatcher.CommandID { return Monitor }
func (c *MonitorCmd) Release() {
	c.Watcher = pid.PID{}
	c.Target = pid.PID{}
	monitorCmdPool.Put(c)
}

// UnmonitorCmd stops monitoring a process.
type UnmonitorCmd struct {
	Watcher pid.PID
	Target  pid.PID
}

var unmonitorCmdPool = sync.Pool{New: func() any { return &UnmonitorCmd{} }}

func AcquireUnmonitorCmd() *UnmonitorCmd            { return unmonitorCmdPool.Get().(*UnmonitorCmd) }
func (c *UnmonitorCmd) CmdID() dispatcher.CommandID { return Unmonitor }
func (c *UnmonitorCmd) Release() {
	c.Watcher = pid.PID{}
	c.Target = pid.PID{}
	unmonitorCmdPool.Put(c)
}

// LinkCmd creates a bidirectional link between processes.
type LinkCmd struct {
	From pid.PID
	To   pid.PID
}

var linkCmdPool = sync.Pool{New: func() any { return &LinkCmd{} }}

func AcquireLinkCmd() *LinkCmd                 { return linkCmdPool.Get().(*LinkCmd) }
func (c *LinkCmd) CmdID() dispatcher.CommandID { return Link }
func (c *LinkCmd) Release() {
	c.From = pid.PID{}
	c.To = pid.PID{}
	linkCmdPool.Put(c)
}

// UnlinkCmd removes a link between processes.
type UnlinkCmd struct {
	From pid.PID
	To   pid.PID
}

var unlinkCmdPool = sync.Pool{New: func() any { return &UnlinkCmd{} }}

func AcquireUnlinkCmd() *UnlinkCmd               { return unlinkCmdPool.Get().(*UnlinkCmd) }
func (c *UnlinkCmd) CmdID() dispatcher.CommandID { return Unlink }
func (c *UnlinkCmd) Release() {
	c.From = pid.PID{}
	c.To = pid.PID{}
	unlinkCmdPool.Put(c)
}

// CallCmd calls a process and waits for its result.
type CallCmd struct {
	Source registry.ID      // process to call
	Input  payload.Payloads // arguments
	HostID pid.HostID       // target host (empty = default)
}

var callCmdPool = sync.Pool{New: func() any { return &CallCmd{} }}

func AcquireCallCmd() *CallCmd                 { return callCmdPool.Get().(*CallCmd) }
func (c *CallCmd) CmdID() dispatcher.CommandID { return Call }
func (c *CallCmd) Release() {
	c.Source = registry.ID{}
	c.Input = nil
	c.HostID = ""
	callCmdPool.Put(c)
}

// CallResult is the result of a call operation.
type CallResult struct {
	Result *runtime.Result
}
