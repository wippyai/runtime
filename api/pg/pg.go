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
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

// Group is the identifier of a process group.
type Group = string

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

	// JoinGroups adds a process to multiple groups atomically.
	JoinGroups(groups []Group, p pid.PID) error

	// Leave removes a process from a group. Returns ErrNotJoined if the
	// process is not a member of the group.
	Leave(group Group, p pid.PID) error

	// LeaveGroups removes a process from multiple groups. It uses best-effort
	// semantics: leaves all groups where the process is a member and skips
	// groups where it isn't. Returns ErrNotJoined only if the process was
	// not a member of ANY of the specified groups.
	LeaveGroups(groups []Group, p pid.PID) error

	// GetMembers returns all processes in the group across all nodes.
	// Returns an empty slice for non-existent groups.
	GetMembers(group Group) []pid.PID

	// GetLocalMembers returns all processes in the group on the local node.
	// Returns an empty slice for non-existent groups.
	GetLocalMembers(group Group) []pid.PID

	// WhichGroups returns a list of all known groups that have members.
	WhichGroups() []Group

	// WhichLocalGroups returns a list of groups that have at least one local member.
	WhichLocalGroups() []Group

	// Broadcast sends a message to all members of a group across all nodes.
	// Returns the number of members the message was successfully sent to.
	Broadcast(from pid.PID, group Group, topic string, payloads payload.Payloads) (int, error)

	// BroadcastLocal sends a message to local members of a group only.
	// Returns the number of members the message was successfully sent to.
	BroadcastLocal(from pid.PID, group Group, topic string, payloads payload.Payloads) (int, error)
}

// ScopeService is the full interface for a PG scope instance. It extends
// ProcessGroups with monitor/events operations that require atomicity
// guarantees (subscription + snapshot in one event loop tick). Each command
// struct carries a ScopeService reference so the dispatcher is stateless.
type ScopeService interface {
	ProcessGroups

	// Monitor atomically subscribes to a group's membership events and
	// returns the current members. No join/leave can interleave between
	// the subscription setup and the membership snapshot.
	Monitor(group Group, p pid.PID, topic string) MonitorResult

	// Events atomically subscribes to all group membership events and
	// returns a snapshot of all current groups and their members.
	Events(p pid.PID, topic string) EventsResult
}
