// Package funcpool provides interfaces for managing function execution pools.
// Pools are reusable worker pools that execute stateless functions efficiently.
package funcpool

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

// PoolConfig defines configuration for a function pool.
type PoolConfig struct {
	// Workers is the number of worker goroutines.
	Workers int

	// QueueSize is the size of the work queue per shard.
	QueueSize int
}

// Pool represents an active function execution pool.
type Pool interface {
	// Call executes a function with the given method and input.
	Call(ctx context.Context, method string, input []byte) (*runtime.Result, error)

	// Stop gracefully stops the pool.
	Stop()
}

// Host manages multiple function execution pools.
type Host interface {
	// Register creates a new pool with an explicit factory.
	Register(id registry.ID, factory process2.ProcessFactory, config PoolConfig) error

	// RegisterByID creates a pool using the Factory to lookup the process factory.
	// Returns error if Factory is not configured or ID not found.
	RegisterByID(id registry.ID, config PoolConfig) error

	// Deregister removes and stops a pool.
	Deregister(id registry.ID) error

	// Get returns the pool for a function ID, or nil if not found.
	Get(id registry.ID) Pool

	// Call routes a call to the appropriate pool.
	Call(ctx context.Context, id registry.ID, task runtime.Task) (*runtime.Result, error)

	// Start starts the host.
	Start() error

	// Stop stops all pools.
	Stop()
}

// HostConfig configures the Host.
type HostConfig struct {
	// Dispatcher handles yield commands from functions.
	Dispatcher dispatcher.Dispatcher

	// Bus is the event bus for function registration.
	Bus event.Bus

	// Factory provides ProcessFactory lookup by ID for RegisterByID.
	Factory process2.Factory

	// DefaultPoolConfig is used when pool config is not specified.
	DefaultPoolConfig PoolConfig
}
