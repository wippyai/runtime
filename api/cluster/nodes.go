// Package cluster exposes a read-only view of cluster membership and an event
// stream that notifies callers about node joins, leaves, and meta updates.
package cluster

// NodeID is the stable identifier each node advertises to its peers.
type NodeID string

// NodeMeta carries arbitrary key-value metadata (build hash, region, etc.)
type NodeMeta map[string]string

// NodeInfo is an immutable snapshot describing a node at a specific moment.
type NodeInfo struct {
	ID   NodeID   // unique identifier
	Addr string   // advertised address (e.g. Raft transport)
	Meta NodeMeta // free-form metadata
}

// Membership provides synchronous inspection of the node set plus an
// asynchronous subscription API.
//
// Implementations publish NodeEvent values using the constants in events.go
// and the System identifier "cluster".
type Membership interface {
	// Nodes returns a slice of NodeInfo describing every node the local process
	// currently knows about.  The snapshot is consistent with the most recent
	// membership event the caller has consumed.
	Nodes() []NodeInfo
}
