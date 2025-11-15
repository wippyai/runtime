package flex

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	luaconv "github.com/wippyai/runtime/system/payload/lua"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
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

// Execute executes a task synchronously with a VM created on demand.
// Blocks until execution completes or context is cancelled.
func (p *TaskPool) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	// Check if the pool is closed
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	default:
	}

	// Ensure context has a logger
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

	// Acquire semaphore to limit concurrent executions
	select {
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.semaphore <- struct{}{}:
		// Continue with execution
	}
	defer func() { <-p.semaphore }() // Release semaphore on return

	// Create a new VM for this execution
	vm, err := p.factory.CreateVM()
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}
	defer vm.Close() // Always close VM after use

	// Execute the function (blocking)
	result, err := vm.Execute(ctx, p.method, args...)
	if err != nil {
		return nil, err
	}

	// Return result
	return &runtime.Result{
		Value: luaconv.ExportPayload(result),
	}, nil
}

// Close closes the pool and releases all resources
func (p *TaskPool) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
	})
}
