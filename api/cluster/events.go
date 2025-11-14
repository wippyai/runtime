// Package cluster centralizes event-bus metadata that higher-level components
// use to observe membership changes, leader transitions, and KV mutations.
package cluster

import "github.com/wippyai/runtime/api/event"

/*
   Constants
   --------------------------------------------------------------------------
   • System               – topic prefix for every event defined here.
   • Node*EventKind       – membership lifecycle events.
   • KV*EventKind         – Raft-backed key/value mutations.
   • RaftLeader*EventKind – leadership state of the local Raft node.
   • Node*                – concrete values for NodeEvent.Type.
*/

const (
	// System is the event.System identifier shared by all cluster events.
	System event.System = "cluster"

	// NodeJoinedEventKind is published when a new node becomes visible.
	NodeJoinedEventKind event.Kind = "node.joined"

	// NodeLeftEventKind is published when a node departs or becomes unreachable.
	NodeLeftEventKind event.Kind = "node.left"

	// NodeUpdatedEventKind indicates that a node’s metadata has changed.
	NodeUpdatedEventKind event.Kind = "node.updated"

	// KVPutEventKind accompanies a successful key creation or update.
	KVPutEventKind event.Kind = "kv.put"

	// KVDeleteEventKind accompanies a key deletion.
	KVDeleteEventKind event.Kind = "kv.delete"

	// RaftLeaderElectedEventKind is emitted by the node that just became leader.
	RaftLeaderElectedEventKind event.Kind = "raft.leader.elected"

	// RaftLeaderLostEventKind is emitted by the node that just stepped down.
	RaftLeaderLostEventKind event.Kind = "raft.leader.lost"
)

/*
Types
--------------------------------------------------------------------------
NodeEvent     – payload for membership events.
Change        – payload for KV put / delete events.
*/
type (
	// NodeEvent carries information about a node join/leave/update.
	NodeEvent struct {
		Node NodeInfo // snapshot of the node at event time
	}

	// Change describes a single KV mutation.
	//
	// • Key is always the full absolute key.
	// • Val is nil for deletions.
	// • Rev is 0 on delete and the new revision on put.
	Change struct {
		Key string
		Rev uint64
		Val []byte
	}
)
