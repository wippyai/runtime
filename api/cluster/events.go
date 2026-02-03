package cluster

import "github.com/wippyai/runtime/api/event"

// Event system and kinds for cluster events.
const (
	// System is the event.System identifier shared by all cluster events.
	System event.System = "cluster"

	// NodeJoined is published when a new node becomes visible.
	NodeJoined event.Kind = "node.joined"

	// NodeLeft is published when a node departs or becomes unreachable.
	NodeLeft event.Kind = "node.left"

	// NodeUpdated indicates that a node's metadata has changed.
	NodeUpdated event.Kind = "node.updated"

	// KVPut accompanies a successful key creation or update.
	KVPut event.Kind = "kv.put"

	// KVDelete accompanies a key deletion.
	KVDelete event.Kind = "kv.delete"

	// LeaderElected is emitted by the node that just became leader.
	LeaderElected event.Kind = "raft.leader.elected"

	// LeaderLost is emitted by the node that just stepped down.
	LeaderLost event.Kind = "raft.leader.lost"
)

type (
	// NodeEvent carries information about a node join/leave/update.
	NodeEvent struct {
		Node NodeInfo
	}

	// Change describes a single KV mutation.
	Change struct {
		Key string
		Val []byte
		Rev uint64
	}
)
