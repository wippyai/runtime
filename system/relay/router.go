// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

// FenceRejectFunc is invoked when a package is rejected because its fence
// token does not match the current registration of the referenced global
// name. The runtime wires this to the globalreg Service so it can emit the
// pg_fence_rejection_total metric without the relay package depending on
// metrics directly.
type FenceRejectFunc func(globalName, reason string)

// Router orchestrates message delivery between a local node and external upstreams.
// It acts as the primary Receiver for the system.
type Router struct {
	localNode     api.Node
	internode     api.Receiver
	globalReg     globalreg.Registry
	onFenceReject FenceRejectFunc
	peers         sync.Map // NodeID -> Receiver
	internodeMu   sync.RWMutex
	globalRegMu   sync.RWMutex
	fenceRejectMu sync.RWMutex
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
	r.internodeMu.Lock()
	r.internode = receiver
	r.internodeMu.Unlock()
}

// SetGlobalRegistry sets the global registry used for fence token validation.
// Called by the Raft component after boot.
func (r *Router) SetGlobalRegistry(reg globalreg.Registry) {
	r.globalRegMu.Lock()
	r.globalReg = reg
	r.globalRegMu.Unlock()
}

// SetOnFenceReject installs a callback invoked whenever a package is dropped
// due to a fence-token mismatch. Pass nil to clear.
func (r *Router) SetOnFenceReject(fn FenceRejectFunc) {
	r.fenceRejectMu.Lock()
	r.onFenceReject = fn
	r.fenceRejectMu.Unlock()
}

// Send routes the package to the appropriate destination.
// Routing priority: local node → peer nodes → internode fallback.
// If the package carries a fence token, the receiver's FSM validates it
// before routing — rejecting stale references to re-registered names.
func (r *Router) Send(pkg *api.Package) error {
	if pkg == nil {
		return NewNilPackageError()
	}

	// Validate fence token if present.
	if pkg.FenceToken > 0 && pkg.GlobalName != "" {
		r.globalRegMu.RLock()
		gr := r.globalReg
		r.globalRegMu.RUnlock()
		if gr != nil {
			if err := globalreg.ValidateFence(context.Background(), gr, pkg.GlobalName, pkg.FenceToken); err != nil {
				r.fenceRejectMu.RLock()
				cb := r.onFenceReject
				r.fenceRejectMu.RUnlock()
				if cb != nil {
					cb(pkg.GlobalName, "stale_token")
				}

				return err
			}
		}
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

	// Fallback to internode for unknown nodes.
	r.internodeMu.RLock()
	internode := r.internode
	r.internodeMu.RUnlock()
	if internode != nil {
		return internode.Send(pkg)
	}

	return NewNodeNotFoundError(pkg.Target.Node)
}
