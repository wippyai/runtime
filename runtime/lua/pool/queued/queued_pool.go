package queued

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type taskResult struct {
	value lua.LValue
	err   error
}

type task struct {
	ctx    context.Context
	name   string
	args   []lua.LValue
	result chan taskResult
}

// Pool manages multiple Lua VMs with a task queue for script execution.
type Pool struct {
	size       int
	logger     *zap.Logger
	factory    api.Factory
	tasks      chan *task
	workers    int
	closed     atomic.Bool
	closeOnce  sync.Once
	done       chan struct{}
	workerWait sync.WaitGroup
}

// Option represents a pool configuration option.
type Option func(*Pool)

// WithSize sets the size of the VM pool (unused in queued pool but provided for interface consistency).
func WithSize(size int) Option {
	return func(p *Pool) {
		if size > 0 {
			p.size = size
		}
	}
}

// WithWorkers sets the number of worker goroutines.
func WithWorkers(workers int) Option {
	return func(p *Pool) {
		if workers > 0 {
			p.workers = workers
		}
	}
}

// WithLogger sets the logger for the pool.
func WithLogger(logger *zap.Logger) Option {
	return func(p *Pool) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// NewPool creates a new pool with the given configuration.
func NewPool(factory api.Factory, opts ...Option) (*Pool, error) {
	p := &Pool{
		size:    5,
		workers: 2,
		logger:  zap.NewNop(),
		tasks:   make(chan *task, 1000),
		done:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(p)
	}

	if err := p.init(factory); err != nil {
		return nil, fmt.Errorf("failed to initialize pool: %w", err)
	}

	return p, nil
}

// init initializes the pool and starts worker goroutines.
func (p *Pool) init(factory api.Factory) error {
	p.factory = factory

	// Launch worker goroutines.
	for i := 0; i < p.workers; i++ {
		p.workerWait.Add(1)
		go p.worker()
	}

	return nil
}

// Execute queues a task for execution and returns its result.
func (p *Pool) Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error) {
	if p.closed.Load() {
		return nil, fmt.Errorf("pool is closed")
	}

	t := &task{
		ctx:    ctx,
		name:   name,
		args:   args,
		result: make(chan taskResult, 1),
	}

	// Try to queue the task.
	select {
	case <-ctx.Done():
		close(t.result)
		return nil, ctx.Err()
	case <-p.done:
		close(t.result)
		return nil, fmt.Errorf("pool is closed")
	case p.tasks <- t:
	}

	// Wait for result or cancellation.
	select {
	case res := <-t.result:
		return res.value, res.err
	case <-p.done:
		return nil, fmt.Errorf("pool is closed")
	case <-ctx.Done():
		// Task might still be executed by a worker, but we return immediately.
		return nil, ctx.Err()
	}
}

// worker runs in its own goroutine and processes tasks.
func (p *Pool) worker() {
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
	// If vm.Start returns an error, we close the current VM and attempt to recreate it.
	processTask := func(t *task) {
		if t == nil {
			return
		}

		result, err := vm.Execute(t.ctx, t.name, t.args...)
		if err != nil {
			// On error, close the faulty VM and attempt to create a new one.
			vm.Close()
			newVM, newErr := p.factory.CreateVM()
			if newErr != nil {
				p.logger.Error("failed to recreate VM", zap.Error(newErr))
			} else {
				vm = newVM
			}
		}

		// send the result (if the task context isn’t done).
		select {
		case <-t.ctx.Done():
		case t.result <- taskResult{value: result, err: err}:
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
						t.result <- taskResult{err: t.ctx.Err()}
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
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
		p.workerWait.Wait()
	})
}
