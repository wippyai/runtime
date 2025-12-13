// Package queue provides message queue abstractions for reliable message passing.
package queue

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

// Event system and kind constants for the queue package
const (
	// System identifies the queue system in the event bus
	System event.System = "queue"

	// Driver lifecycle events
	KindDriverRegister event.Kind = "queue.driver.register"
	KindDriverStart    event.Kind = "queue.driver.start"
	KindDriverStop     event.Kind = "queue.driver.stop"
	KindDriverDelete   event.Kind = "queue.driver.delete"

	// Queue management events
	KindQueueDeclare event.Kind = "queue.queue.declare"
	KindQueueDelete  event.Kind = "queue.queue.delete"

	// Consumer lifecycle events
	KindConsumerRegister event.Kind = "queue.consumer.register"
	KindConsumerStart    event.Kind = "queue.consumer.start"
	KindConsumerStop     event.Kind = "queue.consumer.stop"
	KindConsumerDelete   event.Kind = "queue.consumer.delete"
)

// Queue represents a queue declaration with its configuration
type Queue struct {
	ID       registry.ID      // Queue registry ID
	DriverID registry.ID      // Associated driver ID
	Name     string           // Actual queue name (from OptionQueueName or ID.Name)
	Options  attrs.Attributes // Queue configuration options
}

// Manager manages queue drivers, queues, and consumers
type Manager interface {
	// Publish sends messages to a queue with interceptor chain applied
	Publish(ctx context.Context, queue registry.ID, msgs ...*Message) error

	// GetDriver returns a driver by its registry ID
	GetDriver(id registry.ID) (Driver, bool)

	// GetQueue returns a queue declaration by its registry ID
	GetQueue(id registry.ID) (*Queue, bool)
}
