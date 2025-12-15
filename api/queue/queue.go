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

	DriverRegister event.Kind = "queue.driver.register"
	DriverDelete   event.Kind = "queue.driver.delete"

	QueueDeclare event.Kind = "queue.queue.declare"
	QueueDelete  event.Kind = "queue.queue.delete"
)

// Queue represents a queue declaration with its configuration
type Queue struct {
	ID       registry.ID      // Queue registry ID
	DriverID registry.ID      // Associated driver ID
	Name     string           // Actual queue name (from OptionQueueName or ID.Name)
	Options  attrs.Attributes // Queue configuration options
}

type Manager interface {
	Publish(ctx context.Context, queue registry.ID, msgs ...*Message) error
	GetDriver(id registry.ID) (Driver, bool)
	GetQueue(id registry.ID) (*Queue, bool)
	RegisterInterceptor(name string, interceptor PublishInterceptor, priority int)
	UnregisterInterceptor(name string)
}
