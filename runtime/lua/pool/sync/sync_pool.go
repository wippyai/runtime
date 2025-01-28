package sync

import (
	"context"
	"fmt"
	"sync"

	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Option represents a pool configuration option
type Option func(*Pool)

// WithSize sets the size of the VM pool per function
func WithSize(size int) Option {
	return func(p *Pool) {
		if size > 0 {
			p.size = size
		}
	}
}

// WithLogger sets the logger for the pool
func WithLogger(logger *zap.Logger) Option {
	return func(p *Pool) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// Pool manages multiple Lua VMs for efficient script execution.
// It implements a fixed-size pool of VMs that can be reused across
// multiple script executions to improve performance and resource utilization.
type Pool struct {
	size      int
	logger    *zap.Logger
	factory   api.Factory
	vms       chan api.VM
	closeOnce sync.Once
	done      chan struct{} // Channel for signaling shutdown
}

// NewPool creates a new VM pool with the specified factory and options.
// By default, it creates a pool with size 5 and a no-op logger.
// Returns an error if pool initialization fails.
func NewPool(factory api.Factory, opts ...Option) (*Pool, error) {
	p := &Pool{
		size:    5,
		logger:  zap.NewNop(),
		factory: factory,
		done:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(p)
	}

	if err := p.init(); err != nil {
		return nil, err
	}

	return p, nil
}

// init initializes the pool by creating VM instances
func (p *Pool) init() error {
	if p.vms != nil {
		return fmt.Errorf("pool already initialized")
	}

	p.vms = make(chan api.VM, p.size)

	// Create initial VM pool
	for i := 0; i < p.size; i++ {
		vm, err := p.factory.MakeVM()
		if err != nil {
			close(p.vms)
			p.cleanupVMs()
			return fmt.Errorf("failed to initialize pool: %w", err)
		}
		p.vms <- vm
	}

	return nil
}

// Execute runs the specified Lua function with the given arguments using a VM from the pool.
// It manages VM lifecycle, handles errors, and ensures proper cleanup.
// Returns the function result and any error that occurred during execution.
// If the pool is closed or the context is canceled, returns an appropriate error.
func (p *Pool) Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error) {
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	default:
	}

	// Acquire VM from pool
	var vm api.VM
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	case vm = <-p.vms:
	}

	result, err := vm.Execute(ctx, name, args...)

	if err == nil {
		select {
		case p.vms <- vm:
		default:
			vm.Close()
		}
		return result, nil
	}

	// Never allow failed VMs to be returned to the pool
	vm.Close()

	select {
	case <-p.done:
		return nil, err
	default:
		if newVM, createErr := p.factory.MakeVM(); createErr == nil {
			select {
			case p.vms <- newVM:
			default:
				newVM.Close()
			}
		}
	}

	return nil, err
}

// cleanupVMs drains and closes all VMs in the pool
func (p *Pool) cleanupVMs() {
	for {
		select {
		case vm, ok := <-p.vms:
			if !ok {
				return
			}
			vm.Close()
		default:
			return
		}
	}
}

// Close shuts down the pool and releases all resources.
// It ensures that cleanup happens exactly once and is safe for concurrent use.
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.done)  // Signal shutdown
		p.cleanupVMs() // Close existing VMs
	})
}
