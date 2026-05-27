// SPDX-License-Identifier: MPL-2.0

// Package cluster provides cluster membership and inter-node communication APIs.
package cluster

import "github.com/wippyai/runtime/api/relay"

type (
	// NodeID is the stable identifier each node advertises to its peers.
	NodeID = string

	// NodeMeta carries arbitrary key-value metadata (build hash, region, etc.)
	NodeMeta map[string]string

	// NodeInfo is an immutable snapshot describing a node at a specific moment.
	NodeInfo struct {
		Meta NodeMeta
		ID   NodeID
		Addr string
	}

	// Membership provides synchronous inspection of the node set and a
	// hook to advertise the local node's gossip metadata to peers.
	Membership interface {
		// Nodes returns a slice of NodeInfo describing every node the local process
		// currently knows about.
		Nodes() []NodeInfo

		// LocalNode returns information about the local node.
		LocalNode() NodeInfo

		// UpdateMeta merges the supplied keys into the local node's gossip
		// metadata and triggers a re-broadcast. Existing keys not present
		// in updates are preserved. Used by subsystems that need to
		// advertise dynamic state (e.g. raft_status during cluster
		// formation, role transitions). Empty-string values are kept;
		// callers that want to clear a key should pass it explicitly.
		UpdateMeta(updates map[string]string)
	}

	// MessageCodec handles encoding and decoding of relay packages for
	// transmission over the network between cluster nodes.
	MessageCodec interface {
		// Encode serializes a relay.Package into a byte slice suitable for
		// network transmission.
		Encode(pkg *relay.Package) ([]byte, error)

		// Decode deserializes a byte slice back into a relay.Package.
		Decode(data []byte) (*relay.Package, error)
	}
)
