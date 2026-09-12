// SPDX-License-Identifier: MPL-2.0

// Package relay provides message relay and routing for inter-process communication.
package relay

import (
	"context"
	"sync/atomic"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

// System identifies the relay system in the event bus.
const System event.System = "relay"

// Event kinds for host operations.
const (
	HostRegister event.Kind = "host.register"
	HostDelete   event.Kind = "host.delete"
	HostAccept   event.Kind = "host.accept"
	HostReject   event.Kind = "host.reject"
)

// Event kinds for peer node operations.
// Peer nodes are external receivers (e.g., Temporal) that can receive packages.
const (
	PeerRegister event.Kind = "peer.register"
	PeerDelete   event.Kind = "peer.delete"
	PeerAccept   event.Kind = "peer.accept"
	PeerReject   event.Kind = "peer.reject"
)

type (
	// Topic represents a message channel identifier.
	Topic = string

	// RetentionLease represents ownership of a bounded message-retention
	// reservation. The producer of a bounded message attaches a lease before
	// handing the message to a queue. A consumer takes the lease when it
	// transfers the message into its own mailbox and releases it when the
	// message is delivered or discarded.
	//
	// The interface deliberately lives in relay rather than process: relay
	// packages can cross scheduler and internode boundaries without depending
	// on any particular consumer implementation.
	RetentionLease interface {
		Release()
	}

	messageRetentionLease struct {
		lease RetentionLease
	}

	// Message represents a single message with topic and payload.
	Message struct {
		// retention is an atomic ownership handoff for the reservation charged
		// by a bounded destination. It is intentionally not serialized: leases
		// are local to a process handoff and must never cross the wire.
		retention atomic.Pointer[messageRetentionLease]
		Topic     Topic
		Payloads  payload.Payloads
		// PayloadBytes is the logical retained size of Payloads. It is
		// optional metadata used by bounded subscribers; zero preserves the
		// historical unbounded relay behavior.
		PayloadBytes int64
		// MaxBytes is the per-destination backlog limit for this message's
		// topic. Zero means that the destination applies no byte limit.
		MaxBytes int64
		// MaxItems is the per-destination message backlog limit for this
		// topic. Zero means that the destination applies no item limit.
		MaxItems int
	}

	// Package combines source, target and messages for delivery.
	Package struct {
		// ReceivedFrom is the immediate connection peer stamped by native
		// internode delivery. It is local metadata, never a wire field. A
		// claimed Source may differ (for example during forwarding).
		// Authentication depends on the transport's configured policy; this
		// field alone proves neither process ownership nor proxy authority.
		ReceivedFrom pid.NodeID `codec:"-" json:"-"`
		Source       pid.PID
		Target       pid.PID
		Messages     []*Message
	}

	// PeerInfo identifies one local peer registration lifetime.
	// Publish the same *PeerInfo as Data in PeerRegister and PeerDelete.
	// Allocate a fresh object for each registration, even for the same receiver,
	// and do not copy it or mutate its metadata after publication. A delete is never address-only.
	// Peer nodes are external receivers (e.g., Temporal) registered at runtime.
	PeerInfo struct {
		retired  atomic.Bool
		Receiver Receiver
		NodeID   pid.NodeID
	}
)

// Retire permanently closes this registration lifetime. It also fences a register
// event still queued when its corresponding delete is processed. This is local
// event ownership state, not a remote process-death assertion.
func (p *PeerInfo) Retire() { p.retired.Store(true) }

// Retired reports whether this registration lifetime has ended.
func (p *PeerInfo) Retired() bool { return p.retired.Load() }

// SetRetentionLease attaches a bounded-retention ownership token to m.
//
// A message has at most one lease. Replacing an existing lease releases the
// old token first, which keeps pooled messages leak-free even when a caller
// accidentally reuses a message that was already admitted elsewhere.
func (m *Message) SetRetentionLease(lease RetentionLease) {
	if m == nil || lease == nil {
		return
	}
	old := m.retention.Swap(&messageRetentionLease{lease: lease})
	if old != nil && old.lease != nil {
		old.lease.Release()
	}
}

// TakeRetentionLease transfers the message's reservation to its consumer.
// It is safe for a concurrent package release; exactly one caller receives
// the token and the other observes nil.
func (m *Message) TakeRetentionLease() RetentionLease {
	if m == nil {
		return nil
	}
	entry := m.retention.Swap(nil)
	if entry == nil {
		return nil
	}
	return entry.lease
}

type (
	// Receiver defines the interface for message delivery.
	Receiver interface {
		Send(*Package) error
	}

	// ContextSender is the cancellable delivery capability. Implementations
	// must stop waiting for delivery when ctx is canceled. It is optional so
	// existing receivers keep the original Send contract; lifecycle-sensitive
	// dispatchers can require this capability instead of detaching a blocked
	// Send goroutine.
	//
	// Ownership is transactional: a nil error transfers the package to the
	// receiver (or its accepted queue); a non-nil error means the receiver did
	// not retain it and the caller must release it exactly once.
	ContextSender interface {
		SendContext(context.Context, *Package) error
	}

	// AttachableReceiver extends Receiver with channel-based message delivery.
	AttachableReceiver interface {
		Receiver
		Attach(pid.PID, chan *Package) (context.CancelFunc, error)
		Detach(pid.PID)
	}

	// OwnedHostRegistrar is an optional native composition capability. Its release
	// removes only the registration it created, never a later replacement, and
	// is safe to call repeatedly. Failure returns no release authority.
	// Release does not drain ordinary calls already dispatched to the receiver.
	// Implementations offering LocalBinder may cancel bound admission and reserve
	// the address until those calls return, preventing reuse during admission.
	// Registration is routing configuration, not remote operation authorization.
	OwnedHostRegistrar interface {
		RegisterOwnedHost(pid.HostID, Receiver) (context.CancelFunc, error)
	}

	// PeerBinding identifies one local virtual-peer registration. It is native
	// routing provenance, not a physical network ingress identity. Current is
	// an observation of registration state, not a lifetime pin or death proof.
	PeerBinding interface {
		Receiver() Receiver
		Current() bool
		// WithReceiver holds this exact registration through call. Retirement
		// cancels its context and prevents address reuse until call returns.
		// The callback must honor cancellation and must not retain the supplied
		// context as authority for later work; later calls need fresh admission.
		// Already admitted work may finish; retirement is not process death.
		WithReceiver(context.Context, func(context.Context, Receiver) error) error
	}

	// LocalPeerResolver resolves only explicitly registered local providers;
	// physical internode fallback is never returned by this capability.
	LocalPeerResolver interface {
		LookupLocalPeer(pid.NodeID) (PeerBinding, bool)
	}

	// OwnedPeerRegistrar removes only its own registration, even if a receiver
	// is later registered again at the same address. Release does not join calls
	// already dispatched by ordinary Send; receiver lifecycle owns their drain.
	// WithReceiver admission is canceled and reserves the address until it returns.
	OwnedPeerRegistrar interface {
		RegisterOwnedPeer(pid.NodeID, Receiver) (context.CancelFunc, error)
	}

	// Node represents a messaging node that hosts and routes messages.
	Node interface {
		Receiver
		ID() pid.NodeID
		RegisterHost(pid.HostID, Receiver) error
		UnregisterHost(pid.HostID)
		GetHost(pid.HostID) (Receiver, bool)
		Attach(pid.PID, chan *Package) (context.CancelFunc, error)
		Detach(pid.PID)
	}

	// NodeManager manages relay nodes and hosts.
	NodeManager interface {
		Node() Node
		Start(ctx context.Context) error
		Stop() error
	}
)

// AddMessage adds a new message to the package.
func (p *Package) AddMessage(topic Topic, payloads ...payload.Payload) {
	msg := AcquireMessage()
	msg.Topic = topic
	msg.Payloads = payloads
	p.Messages = append(p.Messages, msg)
}
