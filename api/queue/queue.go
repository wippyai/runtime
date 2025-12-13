// Package queue provides message queue abstractions for reliable message passing.
package queue

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

const (
	System event.System = "queue"

	KindDriverRegister event.Kind = "queue.driver.register"
	KindDriverDelete   event.Kind = "queue.driver.delete"

	KindQueueDeclare event.Kind = "queue.queue.declare"
	KindQueueDelete  event.Kind = "queue.queue.delete"
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
