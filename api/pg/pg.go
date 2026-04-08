// SPDX-License-Identifier: MPL-2.0

// Package pg provides distributed named process groups.
//
// Processes from any node can join named groups. Group membership is
// propagated across all nodes in the cluster using an eventually consistent
// protocol. When a process exits, it is automatically removed from all groups.
//
// This implementation follows the semantics of Erlang/OTP's pg module.
package pg

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

func init() {
	dispatcher.MustRegisterCommands("pg",
		Join, Leave, GetMembers, GetLocalMembers,
		WhichGroups, Broadcast, BroadcastLocal,
	)
}

// Group is the identifier of a process group.
type Group = string

// Command IDs for process group operations.
// Range 200-209 is reserved for pg commands.
const (
	Join            dispatcher.CommandID = 200 // Join a group
	Leave           dispatcher.CommandID = 201 // Leave a group
	GetMembers      dispatcher.CommandID = 202 // Get all members across all nodes
	GetLocalMembers dispatcher.CommandID = 203 // Get members on local node only
	WhichGroups     dispatcher.CommandID = 204 // List all known groups
	Broadcast       dispatcher.CommandID = 205 // Send message to all group members
	BroadcastLocal  dispatcher.CommandID = 206 // Send message to local group members
)

// Relay host ID for the pg service.
const HostID pid.HostID = "pg"

// Inter-node protocol topics.
const (
	TopicDiscover = "pg.discover"
	TopicSync     = "pg.sync"
	TopicJoin     = "pg.join"
	TopicLeave    = "pg.leave"
)

// ProcessGroups defines the interface for process group operations.
type ProcessGroups interface {
	// Join adds a process to a group. A process can join the same group
	// multiple times and must leave the same number of times.
	Join(group Group, p pid.PID) error

	// Leave removes a process from a group. Returns ErrNotJoined if the
	// process is not a member of the group.
	Leave(group Group, p pid.PID) error

	// GetMembers returns all processes in the group across all nodes.
	// Returns an empty slice for non-existent groups.
	GetMembers(group Group) []pid.PID

	// GetLocalMembers returns all processes in the group on the local node.
	// Returns an empty slice for non-existent groups.
	GetLocalMembers(group Group) []pid.PID

	// WhichGroups returns a list of all known groups that have members.
	WhichGroups() []Group

	// Broadcast sends a message to all members of a group across all nodes.
	Broadcast(from pid.PID, group Group, topic string, payloads payload.Payloads) error

	// BroadcastLocal sends a message to local members of a group only.
	BroadcastLocal(from pid.PID, group Group, topic string, payloads payload.Payloads) error
}

// JoinCmd joins a process to a group.
type JoinCmd struct {
	Caller pid.PID
	Group  Group
}

var joinCmdPool = sync.Pool{New: func() any { return &JoinCmd{} }}

func AcquireJoinCmd() *JoinCmd                 { return joinCmdPool.Get().(*JoinCmd) }
func (c *JoinCmd) CmdID() dispatcher.CommandID { return Join }
func (c *JoinCmd) Release() {
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
	Caller pid.PID
	Group  Group
}

var leaveCmdPool = sync.Pool{New: func() any { return &LeaveCmd{} }}

func AcquireLeaveCmd() *LeaveCmd                { return leaveCmdPool.Get().(*LeaveCmd) }
func (c *LeaveCmd) CmdID() dispatcher.CommandID { return Leave }
func (c *LeaveCmd) Release() {
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
	Group Group
}

var getMembersCmdPool = sync.Pool{New: func() any { return &GetMembersCmd{} }}

func AcquireGetMembersCmd() *GetMembersCmd           { return getMembersCmdPool.Get().(*GetMembersCmd) }
func (c *GetMembersCmd) CmdID() dispatcher.CommandID { return GetMembers }
func (c *GetMembersCmd) Release() {
	c.Group = ""
	getMembersCmdPool.Put(c)
}

// GetMembersResult is the result of a get members operation.
type GetMembersResult struct {
	Members []pid.PID
}

// GetLocalMembersCmd queries local members of a group.
type GetLocalMembersCmd struct {
	Group Group
}

var getLocalMembersCmdPool = sync.Pool{New: func() any { return &GetLocalMembersCmd{} }}

func AcquireGetLocalMembersCmd() *GetLocalMembersCmd {
	return getLocalMembersCmdPool.Get().(*GetLocalMembersCmd)
}
func (c *GetLocalMembersCmd) CmdID() dispatcher.CommandID { return GetLocalMembers }
func (c *GetLocalMembersCmd) Release() {
	c.Group = ""
	getLocalMembersCmdPool.Put(c)
}

// GetLocalMembersResult is the result of a get local members operation.
type GetLocalMembersResult struct {
	Members []pid.PID
}

// WhichGroupsCmd queries all known groups.
type WhichGroupsCmd struct{}

var whichGroupsCmdPool = sync.Pool{New: func() any { return &WhichGroupsCmd{} }}

func AcquireWhichGroupsCmd() *WhichGroupsCmd          { return whichGroupsCmdPool.Get().(*WhichGroupsCmd) }
func (c *WhichGroupsCmd) CmdID() dispatcher.CommandID { return WhichGroups }
func (c *WhichGroupsCmd) Release()                    { whichGroupsCmdPool.Put(c) }

// WhichGroupsResult is the result of a which groups operation.
type WhichGroupsResult struct {
	Groups []Group
}

// BroadcastCmd sends a message to all group members.
type BroadcastCmd struct {
	From     pid.PID
	Group    Group
	Topic    string
	Payloads payload.Payloads
}

var broadcastCmdPool = sync.Pool{New: func() any { return &BroadcastCmd{} }}

func AcquireBroadcastCmd() *BroadcastCmd            { return broadcastCmdPool.Get().(*BroadcastCmd) }
func (c *BroadcastCmd) CmdID() dispatcher.CommandID { return Broadcast }
func (c *BroadcastCmd) Release() {
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
