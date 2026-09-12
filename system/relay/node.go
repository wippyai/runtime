// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

// Node represents a messaging node in the pub/sub system that manages a
// local collection of hosts. It is responsible for routing messages to the
// correct host within this node.
//
// This implementation does not handle routing to external nodes; all messages
// must be targeted to a host registered within this Node instance.
type Node struct {
	nodeID pid.NodeID
	hosts  sync.Map // stores mapping: HostID -> *hostRegistration
}

// NewNode creates a new, isolated messaging node with the specified ID.
func NewNode(nodeID pid.NodeID) *Node {
	return &Node{
		nodeID: nodeID,
	}
}

// ID returns the node's identifier.
func (n *Node) ID() pid.NodeID {
	return n.nodeID
}

// hostRegistration distinguishes successive registrations even when they reuse
// the same receiver. Optional receiver capabilities are never wrapped or hidden.
type hostRegistration struct {
	registrationLifetime
	receiver api.Receiver
}

var _ api.OwnedHostRegistrar = (*Node)(nil)

// RegisterHost adds a host until explicit UnregisterHost by its owning composition.
func (n *Node) RegisterHost(hostID pid.HostID, host api.Receiver) error {
	_, err := n.RegisterOwnedHost(hostID, host)
	return err
}

// RegisterOwnedHost returns an idempotent release for this registration only.
// Release stops new lookups; a receiver already obtained by Send, GetHost or
// Attach may still be in use. The receiver owns admission fencing and draining.
// Bound admission is canceled; its address remains reserved until those calls return.
func (n *Node) RegisterOwnedHost(hostID pid.HostID, host api.Receiver) (context.CancelFunc, error) {
	registration := &hostRegistration{receiver: host}
	registration.init(func() { n.hosts.CompareAndDelete(hostID, registration) })
	if _, loaded := n.hosts.LoadOrStore(hostID, registration); loaded {
		registration.cancel()
		return nil, NewHostExistsError(hostID, n.nodeID)
	}
	return registration.retire, nil
}

// UnregisterHost retires the current host and cancels bound admission. The
// address becomes reusable after those calls return. It remains available to
// the owning composition; replaceable components should use RegisterOwnedHost.
func (n *Node) UnregisterHost(hostID pid.HostID) {
	if value, ok := n.hosts.Load(hostID); ok {
		if registration, ok := value.(*hostRegistration); ok {
			registration.retire()
		}
	}
}

// lookupHost keeps missing registrations distinct from invalid stored values.
func (n *Node) lookupHost(hostID pid.HostID) (api.Receiver, bool) {
	value, found := n.hosts.Load(hostID)
	if !found {
		return nil, false
	}
	registration, ok := value.(*hostRegistration)
	if !ok {
		return nil, true
	}
	if registration.retired.Load() {
		return nil, false
	}
	return registration.receiver, true
}

// GetHost returns the original receiver with all its optional capabilities.
func (n *Node) GetHost(hostID pid.HostID) (api.Receiver, bool) {
	receiver, found := n.lookupHost(hostID)
	return receiver, found && receiver != nil
}

// Send delivers a package to its destination. The destination must be a host
// registered within this node.
func (n *Node) Send(pkg *api.Package) error {
	return n.send(context.Background(), pkg)
}

// SendContext delivers a package while honoring cancellation in a
// context-aware host. Hosts that do not expose ContextSender retain the
// historical synchronous receiver contract.
func (n *Node) SendContext(ctx context.Context, pkg *api.Package) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return n.send(ctx, pkg)
}

func (n *Node) send(ctx context.Context, pkg *api.Package) error {
	if pkg == nil {
		return NewNilPackageError()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if pkg.Target.Node != "" && pkg.Target.Node != n.nodeID {
		return NewExternalNodeError(pkg.Target.Node)
	}

	h, ok := n.lookupHost(pkg.Target.Host)
	if !ok {
		return NewHostNotFoundError(pkg.Target.Host, n.nodeID)
	}

	receiver := h
	if receiver == nil {
		return NewInvalidHostTypeError(pkg.Target.Host, n.nodeID)
	}

	if sender, ok := receiver.(api.ContextSender); ok {
		return sender.SendContext(ctx, pkg)
	}
	if ctx.Done() != nil {
		return NewContextUnsupportedError(pkg.Target.Host, n.nodeID)
	}
	return receiver.Send(pkg)
}

// Attach connects a process ID to a channel for receiving packages.
// Only works with hosts that implement AttachableReceiver.
func (n *Node) Attach(p pid.PID, ch chan *api.Package) (context.CancelFunc, error) {
	if p.Node != "" && p.Node != n.nodeID {
		return nil, NewExternalNodeError(p.Node)
	}

	h, ok := n.lookupHost(p.Host)
	if !ok {
		return nil, NewHostNotFoundError(p.Host, n.nodeID)
	}

	attachable, ok := h.(api.AttachableReceiver)
	if !ok {
		return nil, NewHostNotAttachableError(p.Host)
	}

	return attachable.Attach(p, ch)
}

// Detach disconnects a process ID from its receive channel.
func (n *Node) Detach(p pid.PID) {
	if p.Node != "" && p.Node != n.nodeID {
		return
	}

	h, ok := n.lookupHost(p.Host)
	if !ok {
		return
	}

	if attachable, ok := h.(api.AttachableReceiver); ok {
		attachable.Detach(p)
	}
}
