package sync

import (
	"context"
	"fmt"
	"sync"

	luaconv "github.com/wippyai/runtime/system/payload/lua"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// TaskOption represents a TaskPool configuration option
type TaskOption func(*TaskPool)

// WithTaskPoolSize sets the size of the VM pool per function
func WithTaskPoolSize(size int) TaskOption {
	return func(p *TaskPool) {
		if size > 0 {
			p.size = size
		}
	}
}

// WithTaskPoolLogger sets the logger for the pool
func WithTaskPoolLogger(logger *zap.Logger) TaskOption {
	return func(p *TaskPool) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// TaskPool manages multiple Lua VMs for efficient task execution.
// It implements a fixed-size pool of VMs that can be reused across
// multiple task executions to improve performance and resource utilization.
// Unlike the regular Pool, TaskPool directly handles runtime.Task execution
// and result transcoding in a way that minimizes goroutine creation.
type TaskPool struct {
	size      int
	logger    *zap.Logger
	factory   api.Factory
	method    string
	vms       chan api.VM
	closeOnce sync.Once
	done      chan struct{} // Channel for signaling shutdown
}

// NewTaskPool creates a new TaskPool with the specified factory, method name, and options.
// By default, it creates a pool with size 5 and a no-op logger.
// Returns an error if pool initialization fails.
func NewTaskPool(factory api.Factory, method string, opts ...TaskOption) (*TaskPool, error) {
	p := &TaskPool{
		size:    5,
		logger:  zap.NewNop(),
		factory: factory,
		method:  method,
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
func (p *TaskPool) init() error {
	if p.vms != nil {
		return fmt.Errorf("pool already initialized")
	}

	p.vms = make(chan api.VM, p.size)

	// Spawn initial VM pool
	for i := 0; i < p.size; i++ {
		vm, err := p.factory.CreateVM()
		if err != nil {
			close(p.vms)
			p.cleanupVMs()
			return fmt.Errorf("failed to initialize pool: %w", err)
		}
		p.vms <- vm
	}

	return nil
}

// Execute handles a runtime.Task synchronously, performing the necessary transcoding
// and executing the Lua function with a VM from the pool. Blocks until completion.
func (p *TaskPool) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	// Check if the pool is closed
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	default:
	}

	// Ensure context has a logger (todo: drop it)
	ctx = logs.WithLogger(ctx, p.logger)

	// Get transcoder from context
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, fmt.Errorf("no transcoder found in context")
	}

	// Convert payloads to Lua values before acquiring a VM
	args := make([]lua.LValue, len(task.Payloads))
	for i, p := range task.Payloads {
		// Transcode to Lua format if needed
		luaPayload, err := dtt.Transcode(p, payload.Lua)
		if err != nil {
			return nil, fmt.Errorf("failed to transcode payload: %w", err)
		}
		args[i] = luaPayload.Data().(lua.LValue)
	}

	// Apply Task.Context pairs to the execution context
	execCtx := ctx
	if len(task.Context) > 0 {
		// Open or get frame context
		frameCtx, fc := ctxapi.OpenFrameContext(ctx)
		// Apply all context pairs from the task
		if err := fc.SetMultiple(task.Context...); err != nil {
			return nil, fmt.Errorf("failed to set task context: %w", err)
		}
		execCtx = frameCtx
	}

	// Acquire VM from pool
	var vm api.VM
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	case <-execCtx.Done():
		return nil, execCtx.Err()
	case vm = <-p.vms:
	}

	// Execute the function (blocking)
	result, err := vm.Execute(execCtx, p.method, args...)

	if err == nil {
		// Return VM to the pool
		select {
		case p.vms <- vm:
		default:
			vm.Close()
		}

		// Return result
		return &runtime.Result{
			Value: luaconv.ExportPayload(result),
		}, nil
	}

	// Never allow failed VMs to be returned to the pool
	vm.Close()

	// Try to create a replacement VM
	select {
	case <-p.done:
		// Pool is shutting down, don't create a new VM
	default:
		if newVM, createErr := p.factory.CreateVM(); createErr == nil {
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
func (p *TaskPool) cleanupVMs() {
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
func (p *TaskPool) Close() {
	p.closeOnce.Do(func() {
		close(p.done)  // Signal shutdown
		p.cleanupVMs() // close existing VMs
	})
}
