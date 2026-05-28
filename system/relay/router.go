// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

// Router orchestrates message delivery between a local node and external upstreams.
// It acts as the primary Receiver for the system.
type Router struct {
	localNode api.Node
	internode atomic.Pointer[api.Receiver]
	peers     sync.Map // NodeID -> Receiver
}

// NewRouter creates a new router.
// localNode is the node for handling messages targeted to the local node ID.
// internode is the fallback receiver for all messages targeted to other nodes. Can be nil.
func NewRouter(localNode api.Node, internode api.Receiver) *Router {
	r := &Router{localNode: localNode}
	if internode != nil {
		r.internode.Store(&internode)
	}
	return r
}

// RegisterPeer registers a peer node receiver with the router.
// Peer nodes are external receivers (e.g., Temporal) that can receive packages.
func (r *Router) RegisterPeer(nodeID pid.NodeID, receiver api.Receiver) error {
	if nodeID == "" {
		return api.ErrEmptyNodeID
	}
	if nodeID == r.localNode.ID() {
		return NewPeerConflictError(nodeID)
	}

	if _, loaded := r.peers.LoadOrStore(nodeID, receiver); loaded {
		return NewPeerExistsError(nodeID)
	}

	return nil
}

// UnregisterPeer removes a peer node from the router.
// Returns true if the peer existed and was removed, false if it didn't exist.
func (r *Router) UnregisterPeer(nodeID pid.NodeID) bool {
	_, existed := r.peers.LoadAndDelete(nodeID)
	return existed
}

// SetInternode sets (or replaces) the internode fallback receiver.
// This is called by the cluster component after boot to enable cross-node routing.
func (r *Router) SetInternode(receiver api.Receiver) {
	if receiver == nil {
		r.internode.Store(nil)
		return
	}
	r.internode.Store(&receiver)
}

// Send routes the package to the appropriate destination.
// Routing priority: local node → peer nodes → internode fallback.
func (r *Router) Send(pkg *api.Package) error {
	if pkg == nil {
		return NewNilPackageError()
	}

	// Route to local node if target node is empty or matches the local node's ID.
	if pkg.Target.Node == "" || pkg.Target.Node == r.localNode.ID() {
		return r.localNode.Send(pkg)
	}

	// Check if it's for a registered peer node.
	if receiver, ok := r.peers.Load(pkg.Target.Node); ok {
		if rec, ok := receiver.(api.Receiver); ok {
			return rec.Send(pkg)
		}
	}

	// Fallback to internode for unknown nodes. Lock-free hot-path read:
	// atomic.Pointer.Load is a single MOV on every message send.
	if p := r.internode.Load(); p != nil {
		return (*p).Send(pkg)
	}

	return NewNodeNotFoundError(pkg.Target.Node)
}
