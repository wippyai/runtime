// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Queue stats keys returned by Driver.GetQueueInfo.
const (
	// StatsMessageCount is the total number of messages currently attributed
	// to the queue: both ready-to-deliver and in-flight (awaiting ack).
	StatsMessageCount = "message_count"
	// StatsConsumerCount is the number of active consumer attachments.
	StatsConsumerCount = "consumer_count"
	// StatsReady is the number of messages immediately deliverable.
	StatsReady = "ready"
	// StatsInFlight is the number of messages delivered but not yet acked.
	StatsInFlight = "in_flight"
)

// Delivery represents a single message delivery to a consumer.
// Ack signals successful processing; Nack signals failure and requests
// redelivery or dead-lettering per driver policy.
//
// A Delivery carries a pooled *Message. The consumer returns that
// *Message to sync.Pool once the handler returns, so anything holding
// on to the Delivery (such as a Lua userdata wrapper) must stop
// dereferencing Message after Invalidate is called — the pool entry may
// belong to a different message by then. Accessors gate on Released().
type Delivery struct {
	Message  *Message
	Ack      func(ctx context.Context) error
	Nack     func(ctx context.Context) error
	released atomic.Bool
	settled  atomic.Bool
}

// Invalidate marks the Delivery as released. Called by the consumer
// immediately before ReleaseMessage(delivery.Message) so wrappers that
// outlived the handler can short-circuit on their next accessor call.
// Idempotent: double Invalidate keeps the flag true.
func (d *Delivery) Invalidate() { d.released.Store(true) }

// Released reports whether the underlying *Message has been handed back
// to sync.Pool. Wrapper accessors (Lua and otherwise) must bail out with
// an error when this returns true instead of touching Message.
func (d *Delivery) Released() bool { return d.released.Load() }

// MarkSettled claims the single-shot ack/nack slot for this delivery.
// Returns true for the first caller (who proceeds to invoke Ack or Nack
// on the broker) and false for every later caller. Used by both the
// manual path (Lua wrapper's msg:ack()/msg:nack()) and the consumer's
// post-handler auto-ack to prevent the broker from seeing two settles
// for the same delivery.
func (d *Delivery) MarkSettled() bool { return d.settled.CompareAndSwap(false, true) }

// Settled reports whether Ack or Nack has already been invoked on this
// delivery. Cheap read path — callers that only need to observe the
// state (not claim it) should use this instead of MarkSettled.
func (d *Delivery) Settled() bool { return d.settled.Load() }

// Driver is the broker-facing interface every queue driver implements.
// All methods are keyed by the queue's registry.ID; the driver stores
// whatever broker-side state it needs internally keyed on that ID.
type Driver interface {
	// Publish sends one or more messages to a queue. Drivers merge
	// queue-level defaults (from the Config captured at DeclareQueue time)
	// with message headers before the broker call; per-message headers win.
	Publish(ctx context.Context, queue registry.ID, msgs ...*Message) error

	// Attach starts consuming messages from a queue. Consumer-scoped
	// options (auto_ack, prefetch hints, broker-specific consumer flags)
	// come from opts, not from the queue declaration.
	// Returns a cancel function that stops delivery when called.
	Attach(ctx context.Context, queue registry.ID, opts *ConsumerOptions, deliveries chan<- *Delivery) (context.CancelFunc, error)

	// DeclareQueue creates or updates a queue on the broker using the
	// typed Config. Drivers read their own sub-bag via cfg.DriverBag("<drv>").
	DeclareQueue(ctx context.Context, queue registry.ID, cfg *Config) error

	// GetQueueInfo returns operational stats (message count, consumer count,
	// etc.) using the StatsMessageCount / StatsConsumerCount / StatsReady
	// keys. Returns ErrQueueNotFound when the queue is unknown.
	GetQueueInfo(ctx context.Context, queue registry.ID) (attrs.Attributes, error)
}

// DriverService combines the Driver operations with a supervised lifecycle.
type DriverService interface {
	Driver
	supervisor.Service
}
