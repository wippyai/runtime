package queued

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type runtimeTask struct {
	ctx    context.Context
	task   runtime.Task
	method string
	result chan *runtime.Result
}

// TaskPool manages multiple Lua VMs with a task queue for runtime.Task execution.
type TaskPool struct {
	size       int
	logger     *zap.Logger
	factory    api.Factory
	tasks      chan *runtimeTask
	workers    int
	closed     atomic.Bool
	closeOnce  sync.Once
	done       chan struct{}
	workerWait sync.WaitGroup
	method     string
}

// TaskOption represents a TaskPool configuration option.
type TaskOption func(*TaskPool)

// WithTaskSize sets the size of the VM pool (unused in queued pool but provided for interface consistency).
func WithTaskSize(size int) TaskOption {
	return func(p *TaskPool) {
		if size > 0 {
			p.size = size
		}
	}
}

// WithTaskWorkers sets the number of worker goroutines.
func WithTaskWorkers(workers int) TaskOption {
	return func(p *TaskPool) {
		if workers > 0 {
			p.workers = workers
		}
	}
}

// WithTaskLogger sets the logger for the pool.
func WithTaskLogger(logger *zap.Logger) TaskOption {
	return func(p *TaskPool) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// NewTaskPool creates a new TaskPool with the given configuration.
func NewTaskPool(factory api.Factory, method string, opts ...TaskOption) (*TaskPool, error) {
	p := &TaskPool{
		size:    5,
		workers: 2,
		logger:  zap.NewNop(),
		tasks:   make(chan *runtimeTask, 1000),
		done:    make(chan struct{}),
		method:  method,
	}

	for _, opt := range opts {
		opt(p)
	}

	if err := p.init(factory); err != nil {
		return nil, fmt.Errorf("failed to initialize task pool: %w", err)
	}

	return p, nil
}

// init initializes the pool and starts worker goroutines.
func (p *TaskPool) init(factory api.Factory) error {
	p.factory = factory

	// Launch worker goroutines.
	for i := 0; i < p.workers; i++ {
		p.workerWait.Add(1)
		go p.worker()
	}

	return nil
}

// Execute queues a runtime.Task for execution and returns a channel for the result.
func (p *TaskPool) Execute(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	if p.closed.Load() {
		return nil, fmt.Errorf("pool is closed")
	}

	resultChan := make(chan *runtime.Result, 1)

	t := &runtimeTask{
		ctx:    ctx,
		task:   task,
		method: p.method,
		result: resultChan,
	}

	// Try to queue the task.
	select {
	case <-ctx.Done():
		close(resultChan)
		return nil, ctx.Err()
	case <-p.done:
		close(resultChan)
		return nil, fmt.Errorf("pool is closed")
	case p.tasks <- t:
		// Task was successfully queued
	}

	return resultChan, nil
}

// worker runs in its own goroutine and processes tasks.
func (p *TaskPool) worker() {
	defer p.workerWait.Done()

	// Spawn a VM for this worker.
	vm, err := p.factory.CreateVM()
	if err != nil {
		p.logger.Error("failed to create VM for worker", zap.Error(err))
		return
	}
	// Ensure the VM is closed when the worker exits.
	defer vm.Close()

	// processTask is a closure that executes a single task using the current VM.
	processTask := func(t *runtimeTask) {
		if t == nil {
			return
		}

		ctx := logs.WithLogger(t.ctx, p.logger.With(zap.String("func", t.task.ID.String())))

		// Get transcoder from context
		dtt := payload.GetTranscoder(ctx)
		if dtt == nil {
			select {
			case <-ctx.Done():
			case t.result <- &runtime.Result{
				Error: fmt.Errorf("no transcoder found in context"),
			}:
			}
			close(t.result)
			return
		}

		// Convert payloads to Lua values
		args := make([]lua.LValue, len(t.task.Payloads))
		for i, p := range t.task.Payloads {
			// Transcode to Lua format if needed
			luaPayload, err := dtt.Transcode(p, payload.Lua)
			if err != nil {
				select {
				case <-ctx.Done():
				case t.result <- &runtime.Result{
					Error: fmt.Errorf("failed to transcode payload: %w", err),
				}:
				}
				close(t.result)
				return
			}
			args[i] = luaPayload.Data().(lua.LValue)
		}

		// Execute the function
		result, err := vm.Execute(ctx, t.method, args...)

		// Create the runtime.Result
		runtimeResult := &runtime.Result{
			Error: err,
		}

		if err == nil {
			// Set the result value
			runtimeResult.Value = payload.NewPayload(result, payload.Lua)
		} else {
			// On error, close the faulty VM and attempt to create a new one.
			vm.Close()
			newVM, newErr := p.factory.CreateVM()
			if newErr != nil {
				p.logger.Error("failed to recreate VM", zap.Error(newErr))
			} else {
				vm = newVM
			}
		}

		// Send the result (if the task context isn't done).
		select {
		case <-ctx.Done():
		case t.result <- runtimeResult:
		default:
			p.logger.Error("failed to send result")
		}
		close(t.result)
	}

	for {
		select {
		// If the pool is shutting down, drain remaining tasks non-blockingly.
		case <-p.done:
			for {
				select {
				case t := <-p.tasks:
					if t.ctx.Err() != nil {
						t.result <- &runtime.Result{
							Error: t.ctx.Err(),
						}
						close(t.result)
						continue
					}

					processTask(t)
				default:
					// No more tasks to process; exit the worker.
					return
				}
			}
		// Process incoming task.
		case t := <-p.tasks:
			processTask(t)
		}
	}
}

// Close shuts down the pool.
func (p *TaskPool) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
		p.workerWait.Wait()
	})
}
