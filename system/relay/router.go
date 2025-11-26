package relay

import (
	"fmt"
	"sync"

	api "github.com/wippyai/runtime/api/relay"
)

// Router orchestrates message delivery between a local node and external upstreams.
// It acts as the primary Receiver for the system.
type Router struct {
	localNode    api.Node
	internode    api.Receiver
	virtualNodes sync.Map
}

// NewRouter creates a new router.
// localNode is the node for handling messages targeted to the local node ID.
// internode is the fallback receiver for all messages targeted to other nodes. Can be nil.
func NewRouter(localNode api.Node, internode api.Receiver) *Router {
	return &Router{
		localNode: localNode,
		internode: internode,
	}
}

// RegisterVirtualNode registers a virtual node with the router.
// This is an internal method called by VirtualNodeManager only.
func (r *Router) RegisterVirtualNode(nodeID api.NodeID, receiver api.Receiver) error {
	if nodeID == "" {
		return fmt.Errorf("nodeID cannot be empty")
	}
	if nodeID == r.localNode.ID() {
		return fmt.Errorf("nodeID conflicts with local node: %s", nodeID)
	}

	if _, loaded := r.virtualNodes.LoadOrStore(string(nodeID), receiver); loaded {
		return fmt.Errorf("virtual node already registered: %s", nodeID)
	}

	return nil
}

// UnregisterVirtualNode removes a virtual node from the router.
// This is an internal method called by VirtualNodeManager only.
// Returns true if the node existed and was removed, false if it didn't exist.
func (r *Router) UnregisterVirtualNode(nodeID api.NodeID) bool {
	_, existed := r.virtualNodes.LoadAndDelete(string(nodeID))
	return existed
}

// Send routes the package to the appropriate destination.
// If the package is for the local node, it's sent to the localNode.
// If the package is for a virtual node, it's sent to the virtual node receiver.
// Otherwise, it's forwarded to the internode receiver.
func (r *Router) Send(pkg *api.Package) error {
	if pkg == nil {
		return fmt.Errorf("cannot send nil package")
	}

	// Route to local node if target node is empty or matches the local node's ID.
	if pkg.Target.Node == "" || pkg.Target.Node == r.localNode.ID() {
		return r.localNode.Send(pkg)
	}

	// Check if it's for a virtual node.
	if receiver, ok := r.virtualNodes.Load(string(pkg.Target.Node)); ok {
		return receiver.(api.Receiver).Send(pkg)
	}

	// If it's for an external node, and we have an internode handler, use it.
	if r.internode != nil {
		return r.internode.Send(pkg)
	}

	// Otherwise, we can't route it.
	return fmt.Errorf("cannot route to node %s: not found", pkg.Target.Node)
}
