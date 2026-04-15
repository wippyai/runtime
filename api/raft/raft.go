// SPDX-License-Identifier: MPL-2.0

// Package raft provides the API for the Raft consensus layer.
package raft

import (
	"time"
)

// State represents the current state of a Raft node.
type State int

const (
	// Follower is the initial state; the node receives log entries from the leader.
	Follower State = iota
	// Candidate is the state when attempting to become leader.
	Candidate
	// Leader is the state when the node has been elected leader.
	Leader
	// Shutdown is the state when the node is shutting down.
	Shutdown
)

// String returns a human-readable representation of the Raft state.
func (s State) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	case Shutdown:
		return "Shutdown"
	default:
		return "Unknown"
	}
}

type (
	// ServerID is a unique identifier for a Raft server.
	ServerID = string

	// ServerAddress is the network address of a Raft server.
	ServerAddress = string

	// ApplyResponse wraps the result of applying a command to the FSM.
	ApplyResponse struct {
		// Response is the FSM's response to the applied command. May be nil or an error.
		Response any
		// Index is the Raft log index at which the entry was committed.
		Index uint64
	}

	// Service provides access to the Raft consensus layer.
	// All write operations are linearizable; reads from the local FSM may be stale.
	Service interface {
		// Apply proposes a command to the Raft log. Blocks until the entry is
		// committed by a quorum or the timeout expires.
		// Returns ErrNotLeader if this node is not the current leader.
		Apply(cmd []byte, timeout time.Duration) (*ApplyResponse, error)

		// Leader returns the current leader's server ID and address.
		// Returns empty strings and ErrNoLeader if no leader is known.
		Leader() (ServerID, ServerAddress, error)

		// IsLeader returns true if this node is the current leader.
		IsLeader() bool

		// LeaderCh returns a channel that emits true when this node becomes
		// leader and false when it loses leadership.
		LeaderCh() <-chan bool

		// State returns the current Raft state of this node.
		State() State

		// Barrier issues a barrier request. The barrier ensures that all
		// preceding log entries have been applied to the FSM when it returns.
		// Useful for read-after-write consistency.
		Barrier(timeout time.Duration) error

		// AddVoter adds a node as a voting member of the cluster.
		// Only the leader may call this; returns ErrNotLeader otherwise.
		AddVoter(id ServerID, addr ServerAddress, timeout time.Duration) error

		// RemoveServer removes a node from the cluster.
		// Only the leader may call this; returns ErrNotLeader otherwise.
		RemoveServer(id ServerID, timeout time.Duration) error

		// GetConfiguration returns the current cluster membership.
		GetConfiguration() ([]Server, error)
	}

	// Server describes a node in the Raft cluster.
	Server struct {
		ID      ServerID
		Address ServerAddress
		IsVoter bool
	}
)
