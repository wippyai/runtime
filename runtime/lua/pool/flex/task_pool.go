package flex

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	luaconv "github.com/ponyruntime/pony/system/payload/lua"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// TaskPool implements an on-demand pool for Lua VMs.
// It creates VMs on demand and destroys them after use,
// optimizing for rarely called functions.
type TaskPool struct {
	logger    *zap.Logger
	method    string
	maxSize   int
	factory   api.Factory
	semaphore chan struct{}
	closed    atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
}

// TaskOption represents a configuration option for TaskPool
type TaskOption func(*TaskPool)

// WithTaskMaxSize sets the maximum number of concurrent executions
func WithTaskMaxSize(maxSize int) TaskOption {
	return func(p *TaskPool) {
		if maxSize > 0 {
			p.maxSize = maxSize
		}
	}
}

// WithTaskLogger sets the logger for the pool
func WithTaskLogger(logger *zap.Logger) TaskOption {
	return func(p *TaskPool) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// NewTaskPool creates a new flex task pool
func NewTaskPool(factory api.Factory, method string, opts ...TaskOption) (*TaskPool, error) {
	if factory == nil {
		return nil, fmt.Errorf("factory cannot be nil")
	}

	p := &TaskPool{
		logger:  zap.NewNop(),
		method:  method,
		maxSize: 100, // Default max size
		factory: factory,
		done:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(p)
	}

	// Initialize semaphore to limit concurrent executions
	p.semaphore = make(chan struct{}, p.maxSize)

	return p, nil
}

// Execute executes a task with a VM created on demand
func (p *TaskPool) Execute(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	// Check if the pool is closed
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	default:
	}

	// Create the result channel with buffer size 1 to avoid blocking
	resultChan := make(chan *runtime.Result, 1)

	// Ensure context has a logger
	ctx = logs.WithLogger(ctx, p.logger.With(zap.String("func", task.ID.String())))

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

	// Acquire semaphore to limit concurrent executions
	select {
	case <-p.done:
		close(resultChan)
		return nil, fmt.Errorf("pool is closed")
	case <-ctx.Done():
		close(resultChan)
		return nil, ctx.Err()
	case p.semaphore <- struct{}{}:
		// Continue with execution
	}

	// Create a new VM for this execution
	vm, err := p.factory.CreateVM()
	if err != nil {
		<-p.semaphore // Release semaphore on error

		// Return the error through the result channel instead of as immediate error
		resultChan <- &runtime.Result{
			Error: fmt.Errorf("failed to create VM: %w", err),
		}
		close(resultChan)
		return resultChan, nil
	}

	// Execute the function
	result, err := vm.Execute(ctx, p.method, args...)

	// Always close the VM after use - this is the key difference with flex pool
	vm.Close()

	// Release the semaphore
	<-p.semaphore

	// Create a runtime.Result
	runtimeResult := &runtime.Result{
		Error: err,
	}

	if err == nil {
		// Set the result value
		runtimeResult.Value = luaconv.ExportPayload(result)
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

// Close closes the pool and releases all resources
func (p *TaskPool) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
	})
}
