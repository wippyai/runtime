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
		ID   NodeID   // unique identifier
		Addr string   // advertised address (e.g. Raft transport)
		Meta NodeMeta // free-form metadata
	}

	// Membership provides synchronous inspection of the node set.
	Membership interface {
		// Nodes returns a slice of NodeInfo describing every node the local process
		// currently knows about.
		Nodes() []NodeInfo

		// LocalNode returns information about the local node.
		LocalNode() NodeInfo
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
