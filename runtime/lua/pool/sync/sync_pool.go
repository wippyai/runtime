package sync

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/pool"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
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

// WithDefaultTimeout sets the timeout for VM acquisition
func WithDefaultTimeout(timeout time.Duration) Option {
	return func(p *Pool) {
		if timeout > 0 {
			p.defaultTimeout = timeout
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

// Pool manages multiple Lua VMs for efficient script execution
type Pool struct {
	size           int
	defaultTimeout time.Duration
	logger         *zap.Logger
	vmConfig       *pool.VMConfig
	vms            chan *engine.VM
	closed         atomic.Bool
	closeOnce      sync.Once
	done           chan struct{} // Channel for signaling shutdown
}

func NewPool(vmConfig *pool.VMConfig, opts ...Option) *Pool {
	p := &Pool{
		size:           5,
		defaultTimeout: time.Minute,
		logger:         zap.NewNop(),
		vmConfig:       vmConfig,
		done:           make(chan struct{}), // Initialize done channel
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Init initializes the pool by creating VM instances
func (p *Pool) Init() error {
	if p.vms != nil {
		return fmt.Errorf("pool already initialized")
	}

	p.vms = make(chan *engine.VM, p.size)

	// Create initial VM pool
	for i := 0; i < p.size; i++ {
		vm, err := pool.CreateVM(p.vmConfig)
		if err != nil {
			close(p.vms)
			p.cleanupVMs()
			return fmt.Errorf("failed to initialize pool: %w", err)
		}
		p.vms <- vm
	}

	return nil
}

func (p *Pool) Execute(ctx context.Context, name string, args lua.LValue) (lua.LValue, error) {
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	default:
	}

	// Acquire VM from pool
	var vm *engine.VM
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.defaultTimeout):
		return nil, fmt.Errorf("timeout waiting for available VM")
	case vm = <-p.vms:
	}

	// Execute function
	result, err := vm.Execute(ctx, name, args)

	// Handle VM return based on execution result
	if err != nil {
		p.logger.Error("VM execution failed",
			zap.String("function", name),
			zap.Error(err))

		// Clean up failed VM
		vm.Close()

		// Create replacement VM if pool is still open
		select {
		case <-p.done:
			return nil, err
		default:
			// Try create new VM
			if newVM, createErr := pool.CreateVM(p.vmConfig); createErr == nil {
				select {
				case <-p.done:
					newVM.Close()
				case p.vms <- newVM:
				}
			}
		}
		return nil, err
	}

	// Return healthy VM to pool if not closed
	select {
	case <-p.done:
		vm.Close()
	case p.vms <- vm:
	}

	return result, nil
}

func (p *Pool) cleanupVMs() {
	// Drain and close all VMs
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

func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.done)  // Signal shutdown
		p.cleanupVMs() // Cleanup existing VMs
	})
}

func (p *Pool) IsClosed() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}
