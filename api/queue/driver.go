// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Queue declaration and operation option constants.
// Only driver-agnostic options live here; driver-specific options
// belong in their respective api/service packages.
const (
	OptionQueueName = "queue_name" // Override queue name (default: ID.Name)
	OptionMaxLength = "max_length" // Maximum queue length
	OptionDurable   = "durable"    // Queue durability

	// OptionAutoAck enables automatic acknowledgment on consume.
	OptionAutoAck = "auto_ack"

	// OptionCodec sets the per-queue serialization format for message bodies
	// (e.g. "json/plain", "application/msgpack").
	OptionCodec = "codec"

	// StatsMessageCount is a queue stats key (returned by GetQueueInfo)
	StatsMessageCount  = "message_count"  // Number of messages in queue
	StatsConsumerCount = "consumer_count" // Number of active consumers
	// StatsReady is queue stats key for messages ready for delivery.
	StatsReady = "ready"
)

// Delivery represents a message delivery to a consumer
type Delivery struct {
	Message *Message                        // The delivered message
	Ack     func(ctx context.Context) error // Acknowledge successful processing
	Nack    func(ctx context.Context) error // Negative acknowledge (requeue/DLQ)
}

// Driver provides queue operations
type Driver interface {
	// Publish sends one or more messages to a queue
	// Messages are published as a batch when possible for efficiency
	Publish(ctx context.Context, queue registry.ID, msgs ...*Message) error

	// Attach starts consuming messages from a queue
	// Messages are delivered through the channel
	// Call the returned cancel function to stop consuming
	Attach(ctx context.Context, queue registry.ID, deliveries chan<- *Delivery) (context.CancelFunc, error)

	// DeclareQueue creates or updates a queue with the given options
	// The actual queue name can be overridden via OptionQueueName
	DeclareQueue(ctx context.Context, queue registry.ID, opts attrs.Attributes) error

	// GetQueueInfo returns operational information about a queue (if supported)
	// Returns driver-specific stats like message count, consumer count, etc.
	// Returns nil if queue doesn't exist or stats not supported
	GetQueueInfo(ctx context.Context, queue registry.ID) (attrs.Attributes, error)
}

// DriverService combines Driver operations with Service lifecycle
type DriverService interface {
	Driver
	supervisor.Service
}
