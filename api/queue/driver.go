// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// OptionQueueName is a queue declaration option key
const (
	OptionQueueName = "queue_name" // Override queue name (default: ID.Name)
	OptionMaxLength = "max_length" // Maximum queue length
	OptionDurable   = "durable"    // Queue durability

	// Consume/receive options (per-queue, read by Attach)
	OptionExclusive   = "exclusive"    // Exclusive consumer (AMQP)
	OptionAutoAck     = "auto_ack"     // Automatic acknowledgment
	OptionNoLocal     = "no_local"     // Do not receive own publishes (AMQP)
	OptionNoWait      = "no_wait"      // Do not wait for server confirmation (AMQP)
	OptionConsumerTag = "consumer_tag" // Custom consumer tag prefix (AMQP)

	// Codec option (per-queue serialization format)
	OptionCodec = "codec" // Payload format for message body (e.g. "json/plain", "application/msgpack")

	// SQS receive options (per-queue, read by Attach)
	OptionMaxMessages       = "max_messages"       // Max messages per receive call (SQS: 1–10, default 10)
	OptionWaitTime          = "wait_time"          // Long-poll wait time in seconds (SQS: 0–20, default 20)
	OptionVisibilityTimeout = "visibility_timeout" // Visibility timeout in seconds (SQS: 0–43200)

	// OptionMaxBytes reserved for future use
	// OptionMessageTTL reserved for future use
	// OptionDeadLetterQueue reserved for future use
	// OptionDeadLetterExchange reserved for future use
	// OptionMaxRetryCount reserved for future use
	// OptionAutoDelete reserved for future use
	// OptionOrdering reserved for future use
	// OptionPartitions reserved for future use
	// OptionReplicationFactor reserved for future use
	// OptionRetentionPeriod reserved for future use

	// StatsMessageCount is a queue stats key (returned by GetQueueInfo)
	StatsMessageCount  = "message_count"  // Number of messages in queue
	StatsConsumerCount = "consumer_count" // Number of active consumers
	// StatsReady is queue stats key for messages ready for delivery.
	StatsReady = "ready"
	// StatsUnacked = "unacked" // reserved for future use
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
