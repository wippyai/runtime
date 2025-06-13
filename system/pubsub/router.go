package pubsub

import (
	"fmt"
	api "github.com/ponyruntime/pony/api/pubsub"
)

// Router orchestrates message delivery between a local node and external upstreams.
// It acts as the primary Receiver for the system.
type Router struct {
	localNode api.Node
	internode api.Receiver
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

// Send routes the package to the appropriate destination.
// If the package is for the local node, it's sent to the localNode.
// Otherwise, it's forwarded to the internode receiver.
func (r *Router) Send(pkg *api.Package) error {
	if pkg == nil {
		return fmt.Errorf("cannot send nil package")
	}

	// Route to local node if target node is empty or matches the local node's ID.
	if pkg.Target.Node == "" || pkg.Target.Node == r.localNode.ID() {
		return r.localNode.Send(pkg)
	}

	// If it's for an external node, and we have an internode handler, use it.
	if r.internode != nil {
		return r.internode.Send(pkg)
	}

	// Otherwise, we can't route it. This case happens in a single-node setup
	// where an external destination is specified by mistake.
	return fmt.Errorf("cannot route to external node %s: no upstream available", pkg.Target.Node)
}
