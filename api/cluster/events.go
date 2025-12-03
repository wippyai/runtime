package cluster

import "github.com/wippyai/runtime/api/event"

// Event system and kinds for cluster events.
const (
	// System is the event.System identifier shared by all cluster events.
	System event.System = "cluster"

	// NodeJoinedEventKind is published when a new node becomes visible.
	NodeJoinedEventKind event.Kind = "node.joined"

	// NodeLeftEventKind is published when a node departs or becomes unreachable.
	NodeLeftEventKind event.Kind = "node.left"

	// NodeUpdatedEventKind indicates that a node's metadata has changed.
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

type (
	// NodeEvent carries information about a node join/leave/update.
	NodeEvent struct {
		Node NodeInfo
	}

	// Change describes a single KV mutation.
	Change struct {
		Key string
		Rev uint64
		Val []byte // nil for deletions
	}
)
