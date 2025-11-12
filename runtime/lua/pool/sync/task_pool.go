package sync

import (
	"context"
	"fmt"
	"sync"

	luaconv "github.com/ponyruntime/pony/system/payload/lua"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
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

// Execute handles a runtime.Task directly, performing the necessary transcoding
// and executing the Lua function with a VM from the pool.
// It returns a channel that will receive exactly one result (or be closed on error).
func (p *TaskPool) Execute(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	// Check if the pool is closed
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	default:
	}

	// Create the result channel with buffer size 1 to avoid blocking
	resultChan := make(chan *runtime.Result, 1)

	// Ensure context has a logger (todo: drop it)
	ctx = logs.WithLogger(ctx, p.logger)

	// Get transcoder from context
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		close(resultChan)
		return nil, fmt.Errorf("no transcoder found in context")
	}

	// Convert payloads to Lua values before acquiring a VM
	args := make([]lua.LValue, len(task.Payloads))
	for i, p := range task.Payloads {
		// Transcode to Lua format if needed
		luaPayload, err := dtt.Transcode(p, payload.Lua)
		if err != nil {
			close(resultChan)
			return nil, fmt.Errorf("failed to transcode payload: %w", err)
		}
		args[i] = luaPayload.Data().(lua.LValue)
	}

	// Acquire VM from pool
	var vm api.VM
	select {
	case <-p.done:
		close(resultChan)
		return nil, fmt.Errorf("pool is closed")
	case <-ctx.Done():
		close(resultChan)
		return nil, ctx.Err()
	case vm = <-p.vms:
	}

	// Execute the function
	result, err := vm.Execute(ctx, p.method, args...)

	// Create a runtime.Result
	runtimeResult := &runtime.Result{
		Error: err,
	}

	if err == nil {
		// Set the result value
		runtimeResult.Value = luaconv.ExportPayload(result)

		// Return VM to the pool
		select {
		case p.vms <- vm:
		default:
			vm.Close()
		}
	} else {
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

	// Send the result to the channel
	select {
	case resultChan <- runtimeResult:
	default:
		p.logger.Error("failed to send result to channel")
	}
	close(resultChan)

	return resultChan, nil
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
