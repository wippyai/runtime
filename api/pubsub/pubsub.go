package pubsub

import (
	"context"
	"errors"
	contextApi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

// System constants for node management
const (
	// System identifies the node management system in the event context
	System       events.System = "node"
	RegisterHost events.Kind   = "node.register_host"
	DeleteHost   events.Kind   = "node.remove_host"
	AcceptHost   events.Kind   = "node.accept_host"
	RejectHost   events.Kind   = "node.reject_host"
)

var (
	// ErrAlreadyAttached indicates that a receiver is already attached to the specified PID
	ErrAlreadyAttached = errors.New("receiver already attached")
	// ErrHostNotFound indicates that the requested host could not be found
	ErrHostNotFound = errors.New("host not found")
	// ErrHostAlreadyExists indicates that a host with the given ID is already registered
	ErrHostAlreadyExists = errors.New("host already exists")
	// ErrUpstreamNotFound indicates that the requested upstream connection is not available
	ErrUpstreamNotFound = errors.New("upstream not found")
)

type (
	// Topic represents a string identifier for a message channel or category
	Topic = string

	// Message represents a single message in the pub/sub system containing a topic and payload data
	Message struct {
		Topic    Topic
		Payloads payload.Payloads
	}

	// PIDBatch combines a Process ID with a batch of messages for tracking message origin
	PIDBatch struct {
		PID   PID
		Batch *Batch
	}

	// Batch represents a collection of messages to be processed together
	Batch = []*Message

	// Host defines an interface for components that can receive and forward messages
	Host interface {
		Upstream
		Attach(PID, chan *Batch) (context.CancelFunc, error)
	}

	// BatchHost extends Host with support for PID-aware message batching
	BatchHost interface {
		Host
		// AttachWithPID attaches a receiver channel for PIDBatch messages.
		// This method is intended for consumers that need both the sender's PID and the batch payload.
		// It registers the channel to receive messages where each message wraps the PID along with the batch.
		// Note: Only one PIDBatch receiver may be attached per PID; if one already exists, an error is returned.
		AttachWithPID(pid PID, ch chan *PIDBatch) (context.CancelFunc, error)
		Detach(PID)
	}

	// Node represents a messaging node that can host and route messages between multiple hosts
	Node interface {
		Host
		ID() NodeID
		RegisterHost(HostID, Host) error
		UnregisterHost(HostID)
	}

	// Upstream defines the interface for components that can send messages upstream in the pub/sub system
	Upstream interface {
		Send(context.Context, PID, *Batch) error
	}

	// Downstream defines the interface for components that can receive messages from upstream sources
	Downstream interface {
		Send(*Batch) error
	}
)

// NewBatch creates a new message batch with the specified topic and payload items
func NewBatch(topic Topic, payloads ...payload.Payload) *Batch {
	return &Batch{
		{Topic: topic, Payloads: payloads},
	}
}

// GetNode retrieves the Node instance from the provided context
func GetNode(ctx context.Context) Node {
	return ctx.Value(contextApi.NodeCtx).(Node)
}
