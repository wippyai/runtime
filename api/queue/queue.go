// Package queue provides message queue abstractions for reliable message passing.
package queue

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

// Event system and kind constants for the queue package
const (
	// System identifies the queue system in the event bus
	System event.System = "queue"

	// Driver lifecycle events
	DriverRegister event.Kind = "queue.driver.register"
	DriverStart    event.Kind = "queue.driver.start"
	DriverStop     event.Kind = "queue.driver.stop"
	DriverDelete   event.Kind = "queue.driver.delete"

	// Queue management events
	QueueDeclare event.Kind = "queue.queue.declare"
	QueueDelete  event.Kind = "queue.queue.delete"

	// Consumer lifecycle events
	ConsumerRegister event.Kind = "queue.consumer.register"
	ConsumerStart    event.Kind = "queue.consumer.start"
	ConsumerStop     event.Kind = "queue.consumer.stop"
	ConsumerDelete   event.Kind = "queue.consumer.delete"
)

// Error definitions for queue operations
var (
	// ErrNoDriver indicates that the requested driver is not registered
	ErrNoDriver = errors.New("queue driver not found")

	// ErrNoQueue indicates that the requested queue is not declared
	ErrNoQueue = errors.New("queue not found")

	// ErrDriverNotStarted indicates that the driver is not yet started
	ErrDriverNotStarted = errors.New("queue driver not started")

	// ErrQueueFull indicates that the queue has reached its capacity
	ErrQueueFull = errors.New("queue is full")

	// ErrMessageExpired indicates that the message TTL has expired
	ErrMessageExpired = errors.New("message expired")

	// ErrConsumerClosed indicates that the consumer has been closed
	ErrConsumerClosed = errors.New("consumer closed")
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
