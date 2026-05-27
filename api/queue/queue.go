// SPDX-License-Identifier: MPL-2.0

// Package queue provides message queue abstractions for reliable message passing.
package queue

import (
	"context"

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

// Queue is the in-memory view of a declared queue held by the Manager.
// Config carries the full typed configuration passed to the driver at
// declare time; publishers use it to apply per-queue defaults to message
// headers before handing to the driver.
type Queue struct {
	Config   *Config
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
