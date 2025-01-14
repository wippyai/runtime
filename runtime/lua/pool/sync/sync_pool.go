package sync

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/pool"
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

// Pool manages multiple Lua VMs for efficient script execution
type Pool struct {
	size      int
	logger    *zap.Logger
	vmConfig  *pool.VMConfig
	vms       chan *engine.VM
	closed    atomic.Bool
	closeOnce sync.Once
	done      chan struct{} // opChan for signaling shutdown
}

func NewPool(vmConfig *pool.VMConfig, opts ...Option) (*Pool, error) {
	p := &Pool{
		size:     5,
		logger:   zap.NewNop(),
		vmConfig: vmConfig,
		done:     make(chan struct{}), // Initialize done channel
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

func (p *Pool) Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error) {
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

	// Handle VM return based on execution result
	vm.Close()

	select {
	case <-p.done:
		return nil, err
	default:
		if newVM, createErr := pool.CreateVM(p.vmConfig); createErr == nil {
			select {
			case p.vms <- newVM:
			default:
				newVM.Close()
			}
		}
	}

	return nil, err
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
		p.cleanupVMs() // close existing VMs
	})
}
