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

		// CommitIndex returns the highest committed Raft log index. The
		// join-epoch barrier reads it to stamp the strong_index a snapshot of
		// PENDING∪ACTIVE Strong names was taken at, so a concurrently-admitted
		// name is either fully in or fully out of the snapshot.
		CommitIndex() uint64

		// AddVoter adds a node as a voting member of the cluster.
		// Only the leader may call this; returns ErrNotLeader otherwise.
		// If the node already exists as a non-voter, it is promoted to voter.
		AddVoter(id ServerID, addr ServerAddress, timeout time.Duration) error

		// AddNonvoter adds a node as a non-voting (learner) member.
		// Non-voters receive log replication but do not count toward quorum
		// and do not participate in elections.
		// Only the leader may call this; returns ErrNotLeader otherwise.
		AddNonvoter(id ServerID, addr ServerAddress, timeout time.Duration) error

		// DemoteVoter demotes an existing voter to a non-voter.
		// Only the leader may call this; returns ErrNotLeader otherwise.
		DemoteVoter(id ServerID, timeout time.Duration) error

		// RemoveServer removes a node from the cluster.
		// Only the leader may call this; returns ErrNotLeader otherwise.
		RemoveServer(id ServerID, timeout time.Duration) error

		// LeadershipTransfer attempts to transfer leadership to another voter.
		// When id is empty, Raft picks a target. Used before self-removal so
		// the cluster doesn't lose its leader during reconfiguration.
		// Only the leader may call this; returns ErrNotLeader otherwise.
		LeadershipTransfer(id ServerID, timeout time.Duration) error

		// GetConfiguration returns the current cluster membership.
		GetConfiguration() ([]Server, error)

		// Stats returns a snapshot of the underlying Raft node's runtime
		// statistics as string key/value pairs (term, commit_index,
		// last_log_index, applied_index, fsm_pending, etc.). Read-only.
		Stats() map[string]string
	}

	// Server describes a node in the Raft cluster.
	Server struct {
		ID      ServerID
		Address ServerAddress
		IsVoter bool
	}
)
