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

	Declare event.Kind = "queue.queue.declare"
	Delete  event.Kind = "queue.queue.delete"
)

// Queue represents a queue declaration with its configuration
type Queue struct {
	Options  attrs.Attributes
	ID       registry.ID
	DriverID registry.ID
	Name     string
}

// Manager provides queue publishing and lookup operations.
type Manager interface {
	Publish(ctx context.Context, queue registry.ID, msgs ...*Message) error
	GetDriver(id registry.ID) (Driver, bool)
	GetQueue(id registry.ID) (*Queue, bool)
	RegisterInterceptor(name string, interceptor PublishInterceptor, priority int)
	UnregisterInterceptor(name string)
}
