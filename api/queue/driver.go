package queue

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Queue declaration option keys
const (
	OptionQueueName = "queue_name" // Override queue name (default: ID.Name)
	OptionMaxLength = "max_length" // Maximum queue length
	OptionDurable   = "durable"    // Queue durability
	// OptionMaxBytes reserved for future use
	// OptionMessageTTL reserved for future use
	// OptionDeadLetterQueue reserved for future use
	// OptionDeadLetterExchange reserved for future use
	// OptionMaxRetryCount reserved for future use
	// OptionExclusive reserved for future use
	// OptionAutoDelete reserved for future use
	// OptionOrdering reserved for future use
	// OptionPartitions reserved for future use
	// OptionReplicationFactor reserved for future use
	// OptionRetentionPeriod reserved for future use

	// Queue stats keys (returned by GetQueueInfo)
	StatsMessageCount  = "message_count"  // Number of messages in queue
	StatsConsumerCount = "consumer_count" // Number of active consumers
	StatsByteSize      = "byte_size"      // Total size in bytes
	StatsDeliveryCount = "delivery_count" // Total deliveries
	StatsAckCount      = "ack_count"      // Total acknowledges
	StatsNackCount     = "nack_count"     // Total negative acknowledges
	StatsOldestMessage = "oldest_message" // Timestamp of oldest message
	StatsLastDelivery  = "last_delivery"  // Timestamp of last delivery
	StatsReady         = "ready"          // Messages ready for delivery
	StatsUnacked       = "unacked"        // Messages delivered but not acked
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
