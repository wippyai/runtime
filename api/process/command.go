// SPDX-License-Identifier: MPL-2.0

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
		Exec,
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
	Exec      dispatcher.CommandID = 9 // Execute process and wait for result
)

// SendCmd sends a message to a process.
type SendCmd struct {
	From     pid.PID
	To       pid.PID
	Topic    string
	Payloads payload.Payloads
}

var sendCmdPool = sync.Pool{New: func() any { return &SendCmd{} }}

// AcquireSendCmd returns a pooled SendCmd.
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
	Start *Start
}

var spawnCmdPool = sync.Pool{New: func() any { return &SpawnCmd{} }}

// AcquireSpawnCmd returns a pooled SpawnCmd.
func AcquireSpawnCmd() *SpawnCmd                { return spawnCmdPool.Get().(*SpawnCmd) }
func (c *SpawnCmd) CmdID() dispatcher.CommandID { return Spawn }
func (c *SpawnCmd) Release() {
	c.Start = nil
	spawnCmdPool.Put(c)
}

// SpawnResult is the result of a spawn operation.
type SpawnResult struct {
	Error error
	PID   pid.PID
}

// TerminateCmd terminates a process.
type TerminateCmd struct {
	Target pid.PID
}

var terminateCmdPool = sync.Pool{New: func() any { return &TerminateCmd{} }}

// AcquireTerminateCmd returns a pooled TerminateCmd.
func AcquireTerminateCmd() *TerminateCmd            { return terminateCmdPool.Get().(*TerminateCmd) }
func (c *TerminateCmd) CmdID() dispatcher.CommandID { return Terminate }
func (c *TerminateCmd) Release() {
	c.Target = pid.PID{}
	terminateCmdPool.Put(c)
}

// CancelCmd cancels a process with optional deadline.
type CancelCmd struct {
	Deadline time.Time
	From     pid.PID
	Target   pid.PID
}

var cancelCmdPool = sync.Pool{New: func() any { return &CancelCmd{} }}

// AcquireCancelCmd returns a pooled CancelCmd.
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

// AcquireMonitorCmd returns a pooled MonitorCmd.
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

// AcquireUnmonitorCmd returns a pooled UnmonitorCmd.
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

// AcquireLinkCmd returns a pooled LinkCmd.
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

// AcquireUnlinkCmd returns a pooled UnlinkCmd.
func AcquireUnlinkCmd() *UnlinkCmd               { return unlinkCmdPool.Get().(*UnlinkCmd) }
func (c *UnlinkCmd) CmdID() dispatcher.CommandID { return Unlink }
func (c *UnlinkCmd) Release() {
	c.From = pid.PID{}
	c.To = pid.PID{}
	unlinkCmdPool.Put(c)
}

// ExecCmd executes a process and waits for its result.
type ExecCmd struct {
	Source registry.ID
	HostID pid.HostID
	Input  payload.Payloads
}

var execCmdPool = sync.Pool{New: func() any { return &ExecCmd{} }}

// AcquireExecCmd returns a pooled ExecCmd.
func AcquireExecCmd() *ExecCmd                 { return execCmdPool.Get().(*ExecCmd) }
func (c *ExecCmd) CmdID() dispatcher.CommandID { return Exec }
func (c *ExecCmd) Release() {
	c.Source = registry.ID{}
	c.Input = nil
	c.HostID = ""
	execCmdPool.Put(c)
}

// ExecResult is the result of an exec operation.
type ExecResult struct {
	Result *runtime.Result
}
