package host

import (
	"context"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/host"
	"github.com/ponyruntime/pony/api/supervisor"
	msg "github.com/ponyruntime/pony/system/pubsub"
	"go.uber.org/zap"
)

// ProcessPoolAPI defines the interface that a process pool must implement
type ProcessPoolAPI interface {
	// Add registers a new process with the pool
	Add(pid pubsub.PID, proc process.Process) error

	// Schedule adds a process to the work queue
	Schedule(pid pubsub.PID) error

	// Has checks if a process exists in the pool
	Has(pid pubsub.PID) bool

	// Start launches the worker goroutines
	Start()

	// Close gracefully shuts down the worker pool
	Close()

	// Terminate notifies a process about termination
	Terminate(pid pubsub.PID)

	// Remove removes a process from the pool
	Remove(pid pubsub.PID)

	// Cancel sends a cancellation signal to a specific process
	Cancel(pid pubsub.PID, deadline time.Time) error

	// CancelAll sends cancellation signals to all processes and waits for completion
	CancelAll(ctx context.Context, deadline time.Time) error

	// Send sends a message to a specific process
	Send(pid pubsub.PID, pkg *pubsub.Package) error
}

// MessageHostFactory defines an interface for creating message hosts
type MessageHostFactory interface {
	// CreateMessageHost creates a new message host
	CreateMessageHost(ctx context.Context, config *host.EntryConfig, logger *zap.Logger) (pubsub.Host, error)
}

// DefaultMessageHostFactory is the standard implementation of MessageHostFactory
type DefaultMessageHostFactory struct{}

// CreateMessageHost implements MessageHostFactory.CreateMessageHost
func (f *DefaultMessageHostFactory) CreateMessageHost(
	ctx context.Context,
	config *host.EntryConfig,
	logger *zap.Logger,
) (pubsub.Host, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	return msg.NewHost(ctx, msg.HostConfig{
		BufferSize:  config.HostConfig.BufferSize,
		WorkerCount: config.HostConfig.WorkerCount,
		Logger:      logger,
	}), nil
}

// API defines the interface that a host must implement
// This is essentially the process.Managed interface plus any additional
// methods that are specific to our Host implementation
type API interface {
	process.Managed
	supervisor.Service
}

// ProcessPoolFactory defines an interface for creating process pools
type ProcessPoolFactory interface {
	// CreateProcessPool creates a new process pool
	CreateProcessPool(
		ctx context.Context,
		workers int,
		maxProcesses int,
		logger *zap.Logger,
	) (ProcessPoolAPI, error)
}

// DefaultProcessPoolFactory is the standard implementation of ProcessPoolFactory
type DefaultProcessPoolFactory struct{}

// CreateProcessPool implements ProcessPoolFactory.CreateProcessPool
func (f *DefaultProcessPoolFactory) CreateProcessPool(
	ctx context.Context,
	workers int,
	maxProcesses int,
	logger *zap.Logger,
) (ProcessPoolAPI, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	return NewProcessPool(
		ctx,
		workers,
		maxProcesses,
		logger,
	), nil
}

// Factory defines an interface for creating process hosts
type Factory interface {
	// CreateHost creates a new process host
	CreateHost(id registry.ID, config *host.EntryConfig, logger *zap.Logger) (API, error)
}

// DefaultHostFactory is the standard implementation of Factory
type DefaultHostFactory struct {
	poolFactory ProcessPoolFactory
	msgFactory  MessageHostFactory
}

// NewDefaultHostFactory creates a new DefaultHostFactory with default factories
func NewDefaultHostFactory() *DefaultHostFactory {
	return &DefaultHostFactory{
		poolFactory: &DefaultProcessPoolFactory{},
		msgFactory:  &DefaultMessageHostFactory{},
	}
}

// NewDefaultHostFactoryWithFactories creates a new DefaultHostFactory with custom factories
func NewDefaultHostFactoryWithFactories(poolFactory ProcessPoolFactory, msgFactory MessageHostFactory) *DefaultHostFactory {
	if poolFactory == nil {
		poolFactory = &DefaultProcessPoolFactory{}
	}

	if msgFactory == nil {
		msgFactory = &DefaultMessageHostFactory{}
	}

	return &DefaultHostFactory{
		poolFactory: poolFactory,
		msgFactory:  msgFactory,
	}
}

// CreateHost implements Factory.CreateHost
func (f *DefaultHostFactory) CreateHost(
	id registry.ID,
	config *host.EntryConfig,
	logger *zap.Logger,
) (API, error) {
	if config == nil {
		return nil, fmt.Errorf("host config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid host config: %w", err)
	}

	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Create a new host with our factories
	return NewMultiProcessHost(
		id,
		config,
		logger,
		f.msgFactory,
		f.poolFactory,
	), nil
}
