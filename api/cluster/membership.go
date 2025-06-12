// Package cluster defines interfaces and types for cluster membership,
// node discovery, health checking, and associated eventing.
package cluster

import (
	"time"
)

// NodeID is a type alias for a string representing the unique identifier of a node
// within the cluster.
type NodeID string

// NodeInfo encapsulates all relevant information about a node in the cluster.
// This includes its identity, network address for gossip, application-specific
// metadata, and liveness information.
type NodeInfo struct {
	// ID is the unique, immutable identifier of the node within the cluster.
	ID NodeID

	// Address is the network address (e.g., "ip:port") that the underlying
	// membership provider (like Memberlist) uses for its gossip communication.
	Address string

	// Meta is an opaque byte slice containing application-defined metadata.
	// The application is responsible for the serialization format of this data
	// (e.g., JSON, Protobuf). This metadata is gossiped along with the node's
	// presence and health information.
	Meta []byte

	// LastConfirmedAt provides a timestamp indicating the last time the
	// membership service had a positive confirmation of this node's status
	// or received an update for its information. This is managed by the
	// Membership implementation.
	LastConfirmedAt time.Time
}

// Membership defines the core interface for interacting with a cluster membership
// service. It allows querying node information, subscribing to membership changes,
// and updating the local node's advertised metadata.
// The lifecycle management (e.g., starting and stopping the underlying gossip provider)
// is handled externally to this interface.
type Membership interface {
	// LocalNodeInfo returns the complete NodeInfo for the current (local) node,
	// reflecting its latest advertised state, including its Meta.
	LocalNodeInfo() NodeInfo

	// AllNodesInfo retrieves a list of NodeInfo for all nodes currently considered
	// active or part of the cluster from the local node's perspective.
	// This list includes their most recently known Meta data.
	AllNodesInfo() []NodeInfo

	// GetNodeInfo attempts to retrieve the NodeInfo for a specific node identified by its ID.
	// It returns the NodeInfo and a boolean indicating whether the node was found
	// in the local node's current view of the cluster.
	GetNodeInfo(id NodeID) (nodeInfo NodeInfo, found bool)

	// SubscribeToNodeEvents registers a handler function to be invoked whenever
	// a significant change in cluster membership occurs (NodeJoined, NodeLeft, NodeUpdated).
	// The handler receives a NodeEvent detailing the change.
	// It returns a function that can be called to unsubscribe the handler, and an error
	// if the subscription process fails.
	SubscribeToNodeEvents(handler func(NodeEvent)) (unsubscribe func(), err error)

	// UpdateLocalMeta allows the local node to modify its own application-specific
	// metadata (the Meta field of its NodeInfo). The Membership implementation
	// is responsible for taking the provided `meta` byte slice and ensuring it is
	// subsequently gossiped to other members of the cluster.
	// An error may be returned if the update cannot be processed or propagated.
	UpdateLocalMeta(meta []byte) error
}
