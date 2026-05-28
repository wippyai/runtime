// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

// Dispatcher command registration and the pooled command/result structs for
// each process-group operation. Each command carries a ScopeService so the
// dispatcher stays stateless; Acquire/Release recycle the structs.

func init() {
	dispatcher.MustRegisterCommands("pg",
		Join, Leave, GetMembers, GetLocalMembers,
		WhichGroups, Broadcast, BroadcastLocal,
		WhichLocalGroups, Monitor, Events,
		JoinGroups, LeaveGroups,
	)
}

// Command IDs for process group operations.
// Range 200-209 is reserved for pg commands.
const (
	Join             dispatcher.CommandID = 200 // Join a group
	Leave            dispatcher.CommandID = 201 // Leave a group
	GetMembers       dispatcher.CommandID = 202 // Get all members across all nodes
	GetLocalMembers  dispatcher.CommandID = 203 // Get members on local node only
	WhichGroups      dispatcher.CommandID = 204 // List all known groups
	Broadcast        dispatcher.CommandID = 205 // Send message to all group members
	BroadcastLocal   dispatcher.CommandID = 206 // Send message to local group members
	WhichLocalGroups dispatcher.CommandID = 207 // List groups with local members
	Monitor          dispatcher.CommandID = 208 // Atomic subscribe + snapshot
	Events           dispatcher.CommandID = 209 // Subscribe to all group events + snapshot
	JoinGroups       dispatcher.CommandID = 210 // Batch join multiple groups
	LeaveGroups      dispatcher.CommandID = 211 // Batch leave multiple groups
)

// JoinCmd joins a process to a group.
type JoinCmd struct {
	Instance ScopeService
	Caller   pid.PID
	Group    Group
}

var joinCmdPool = sync.Pool{New: func() any { return &JoinCmd{} }}

func AcquireJoinCmd() *JoinCmd                 { return joinCmdPool.Get().(*JoinCmd) }
func (c *JoinCmd) CmdID() dispatcher.CommandID { return Join }
func (c *JoinCmd) Release() {
	c.Instance = nil
	c.Caller = pid.PID{}
	c.Group = ""
	joinCmdPool.Put(c)
}

// JoinResult is the result of a join operation.
type JoinResult struct {
	Error error
}

// LeaveCmd removes a process from a group.
type LeaveCmd struct {
	Instance ScopeService
	Caller   pid.PID
	Group    Group
}

var leaveCmdPool = sync.Pool{New: func() any { return &LeaveCmd{} }}

func AcquireLeaveCmd() *LeaveCmd                { return leaveCmdPool.Get().(*LeaveCmd) }
func (c *LeaveCmd) CmdID() dispatcher.CommandID { return Leave }
func (c *LeaveCmd) Release() {
	c.Instance = nil
	c.Caller = pid.PID{}
	c.Group = ""
	leaveCmdPool.Put(c)
}

// LeaveResult is the result of a leave operation.
type LeaveResult struct {
	Error error
}

// GetMembersCmd queries all members of a group.
type GetMembersCmd struct {
	Instance ScopeService
	Group    Group
}

var getMembersCmdPool = sync.Pool{New: func() any { return &GetMembersCmd{} }}

func AcquireGetMembersCmd() *GetMembersCmd           { return getMembersCmdPool.Get().(*GetMembersCmd) }
func (c *GetMembersCmd) CmdID() dispatcher.CommandID { return GetMembers }
func (c *GetMembersCmd) Release() {
	c.Instance = nil
	c.Group = ""
	getMembersCmdPool.Put(c)
}

// GetMembersResult is the result of a get members operation.
type GetMembersResult struct {
	Members []pid.PID
}

// GetLocalMembersCmd queries local members of a group.
type GetLocalMembersCmd struct {
	Instance ScopeService
	Group    Group
}

var getLocalMembersCmdPool = sync.Pool{New: func() any { return &GetLocalMembersCmd{} }}

func AcquireGetLocalMembersCmd() *GetLocalMembersCmd {
	return getLocalMembersCmdPool.Get().(*GetLocalMembersCmd)
}
func (c *GetLocalMembersCmd) CmdID() dispatcher.CommandID { return GetLocalMembers }
func (c *GetLocalMembersCmd) Release() {
	c.Instance = nil
	c.Group = ""
	getLocalMembersCmdPool.Put(c)
}

// GetLocalMembersResult is the result of a get local members operation.
type GetLocalMembersResult struct {
	Members []pid.PID
}

// WhichGroupsCmd queries all known groups.
type WhichGroupsCmd struct {
	Instance ScopeService
}

var whichGroupsCmdPool = sync.Pool{New: func() any { return &WhichGroupsCmd{} }}

func AcquireWhichGroupsCmd() *WhichGroupsCmd          { return whichGroupsCmdPool.Get().(*WhichGroupsCmd) }
func (c *WhichGroupsCmd) CmdID() dispatcher.CommandID { return WhichGroups }
func (c *WhichGroupsCmd) Release() {
	c.Instance = nil
	whichGroupsCmdPool.Put(c)
}

// WhichGroupsResult is the result of a which groups operation.
type WhichGroupsResult struct {
	Groups []Group
}

// WhichLocalGroupsCmd queries groups that have at least one local member.
type WhichLocalGroupsCmd struct {
	Instance ScopeService
}

var whichLocalGroupsCmdPool = sync.Pool{New: func() any { return &WhichLocalGroupsCmd{} }}

func AcquireWhichLocalGroupsCmd() *WhichLocalGroupsCmd {
	return whichLocalGroupsCmdPool.Get().(*WhichLocalGroupsCmd)
}
func (c *WhichLocalGroupsCmd) CmdID() dispatcher.CommandID { return WhichLocalGroups }
func (c *WhichLocalGroupsCmd) Release() {
	c.Instance = nil
	whichLocalGroupsCmdPool.Put(c)
}

// WhichLocalGroupsResult is the result of a which local groups operation.
type WhichLocalGroupsResult struct {
	Groups []Group
}

// BroadcastCmd sends a message to all group members.
type BroadcastCmd struct {
	Instance ScopeService
	From     pid.PID
	Group    Group
	Topic    string
	Payloads payload.Payloads
}

var broadcastCmdPool = sync.Pool{New: func() any { return &BroadcastCmd{} }}

func AcquireBroadcastCmd() *BroadcastCmd            { return broadcastCmdPool.Get().(*BroadcastCmd) }
func (c *BroadcastCmd) CmdID() dispatcher.CommandID { return Broadcast }
func (c *BroadcastCmd) Release() {
	c.Instance = nil
	c.From = pid.PID{}
	c.Group = ""
	c.Topic = ""
	c.Payloads = nil
	broadcastCmdPool.Put(c)
}

// BroadcastResult is the result of a broadcast operation.
type BroadcastResult struct {
	Error error
	Sent  int
}

// BroadcastLocalCmd sends a message to local group members only.
type BroadcastLocalCmd struct {
	Instance ScopeService
	From     pid.PID
	Group    Group
	Topic    string
	Payloads payload.Payloads
}

var broadcastLocalCmdPool = sync.Pool{New: func() any { return &BroadcastLocalCmd{} }}

func AcquireBroadcastLocalCmd() *BroadcastLocalCmd {
	return broadcastLocalCmdPool.Get().(*BroadcastLocalCmd)
}
func (c *BroadcastLocalCmd) CmdID() dispatcher.CommandID { return BroadcastLocal }
func (c *BroadcastLocalCmd) Release() {
	c.Instance = nil
	c.From = pid.PID{}
	c.Group = ""
	c.Topic = ""
	c.Payloads = nil
	broadcastLocalCmdPool.Put(c)
}

// BroadcastLocalResult is the result of a local broadcast operation.
type BroadcastLocalResult struct {
	Error error
	Sent  int
}

// MonitorCmd subscribes to a group's membership events and atomically
// returns the current members. This prevents the race where events could
// be missed between subscribing and querying members separately.
type MonitorCmd struct {
	Instance ScopeService
	Group    Group
	PID      pid.PID
	Topic    string
}

var monitorCmdPool = sync.Pool{New: func() any { return &MonitorCmd{} }}

func AcquireMonitorCmd() *MonitorCmd              { return monitorCmdPool.Get().(*MonitorCmd) }
func (c *MonitorCmd) CmdID() dispatcher.CommandID { return Monitor }
func (c *MonitorCmd) Release() {
	c.Instance = nil
	c.Group = ""
	c.PID = pid.PID{}
	c.Topic = ""
	monitorCmdPool.Put(c)
}

// MonitorResult is the result of a monitor operation.
type MonitorResult struct {
	Unsubscribe func()
	Members     []pid.PID
}

// EventsCmd subscribes to all group membership events and atomically
// returns a snapshot of all current groups and their members.
type EventsCmd struct {
	Instance ScopeService
	PID      pid.PID
	Topic    string
}

var eventsCmdPool = sync.Pool{New: func() any { return &EventsCmd{} }}

func AcquireEventsCmd() *EventsCmd               { return eventsCmdPool.Get().(*EventsCmd) }
func (c *EventsCmd) CmdID() dispatcher.CommandID { return Events }
func (c *EventsCmd) Release() {
	c.Instance = nil
	c.PID = pid.PID{}
	c.Topic = ""
	eventsCmdPool.Put(c)
}

// EventsResult is the result of an events subscribe operation.
type EventsResult struct {
	Groups      map[Group][]pid.PID
	Unsubscribe func()
}

// JoinGroupsCmd joins a process to multiple groups atomically.
type JoinGroupsCmd struct {
	Instance ScopeService
	Caller   pid.PID
	Groups   []Group
}

var joinGroupsCmdPool = sync.Pool{New: func() any { return &JoinGroupsCmd{} }}

func AcquireJoinGroupsCmd() *JoinGroupsCmd           { return joinGroupsCmdPool.Get().(*JoinGroupsCmd) }
func (c *JoinGroupsCmd) CmdID() dispatcher.CommandID { return JoinGroups }
func (c *JoinGroupsCmd) Release() {
	c.Instance = nil
	c.Caller = pid.PID{}
	c.Groups = nil
	joinGroupsCmdPool.Put(c)
}

// JoinGroupsResult is the result of a batch join operation.
type JoinGroupsResult struct {
	Error error
}

// LeaveGroupsCmd removes a process from multiple groups atomically.
type LeaveGroupsCmd struct {
	Instance ScopeService
	Caller   pid.PID
	Groups   []Group
}

var leaveGroupsCmdPool = sync.Pool{New: func() any { return &LeaveGroupsCmd{} }}

func AcquireLeaveGroupsCmd() *LeaveGroupsCmd          { return leaveGroupsCmdPool.Get().(*LeaveGroupsCmd) }
func (c *LeaveGroupsCmd) CmdID() dispatcher.CommandID { return LeaveGroups }
func (c *LeaveGroupsCmd) Release() {
	c.Instance = nil
	c.Caller = pid.PID{}
	c.Groups = nil
	leaveGroupsCmdPool.Put(c)
}

// LeaveGroupsResult is the result of a batch leave operation.
type LeaveGroupsResult struct {
	Error error
}
