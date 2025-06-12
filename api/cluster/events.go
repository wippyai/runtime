// Package cluster defines interfaces and types for cluster membership,
// node discovery, health checking, and associated eventing.
package cluster

// NodeEventType indicates the type of change in cluster membership.
type NodeEventType int

const (
	// NodeJoined indicates a new node's NodeInfo is now available and
	// the node is considered part of the cluster.
	NodeJoined NodeEventType = iota

	// NodeLeft indicates a node is no longer considered part of the cluster,
	// either due to a graceful leave or being detected as unresponsive.
	NodeLeft

	// NodeUpdated indicates that an existing node's NodeInfo, typically
	// its application-specific Meta field, has changed.
	NodeUpdated
)

// NodeEvent represents an event detailing a change in the cluster's membership.
// Subscribers to Membership.SubscribeToNodeEvents will receive these events.
type NodeEvent struct {
	// Type specifies the kind of membership change that occurred.
	Type NodeEventType

	// Node contains the full NodeInfo of the node affected by this event,
	// including its current Meta data.
	Node NodeInfo
}

// --- Constants for Event Bus Integration ---
// These constants are provided for convenience if these cluster events
// are to be bridged or represented on a more generic, application-wide event bus
// (e.g., one that uses string identifiers for event systems and kinds).

// System can be used as the system identifier
// (e.g., event.System) when publishing NodeEvents to a broader event bus.
const System = "cluster"

// ClusterEventKind defines string constants for specific kinds of cluster events,
// suitable for use as event.Kind on a broader event bus.
const (
	NodeJoinedEventKind  = "node.joined"
	NodeLeftEventKind    = "node.left"
	NodeUpdatedEventKind = "node.updated"
)
