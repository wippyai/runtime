package queued

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
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

// Pool manages multiple Lua VMs with a task queue for script execution
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

// Option represents a pool configuration option
type Option func(*Pool)

// WithSize sets the size of the VM pool
func WithSize(size int) Option {
	return func(p *Pool) {
		if size > 0 {
			p.size = size
		}
	}
}

// WithWorkers sets the number of worker goroutines
func WithWorkers(workers int) Option {
	return func(p *Pool) {
		if workers > 0 {
			p.workers = workers
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

// NewPool creates a new pool with the given configuration
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

// init initializes the pool and starts worker goroutines
func (p *Pool) init(factory api.Factory) error {
	p.factory = factory

	// Start worker goroutines
	for i := 0; i < p.workers; i++ {
		p.workerWait.Add(1)
		go p.worker()
	}

	return nil
}

// Execute queues a task for execution and returns its result
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

	// Try to queue the task
	select {
	case <-ctx.Done():
		close(t.result)
		return nil, ctx.Err()
	case <-p.done:
		close(t.result)
		return nil, fmt.Errorf("pool is closed")
	case p.tasks <- t:
	}

	// Wait for result or cancellation
	select {
	case res := <-t.result:
		return res.value, res.err
	case <-ctx.Done():
		// Note: task might still be picked up and executed by a worker,
		// but the caller won't wait for the result
		return nil, ctx.Err()
	}
}

// worker runs in its own goroutine and processes tasks
func (p *Pool) worker() {
	defer p.workerWait.Done() // Signal worker completion

	// Create a VM for this worker
	vm, err := p.factory.MakeVM()
	if err != nil {
		p.logger.Error("failed to create VM for worker", zap.Error(err))
		return
	}
	defer vm.Close()

	for {
		select {
		case <-p.done:
			// Process remaining tasks in the channel after done signal
			for t := range p.tasks {
				if t == nil {
					continue
				}

				result, err := vm.Execute(t.ctx, t.name, t.args...)
				select {
				case t.result <- taskResult{value: result, err: err}:
				default:
					p.logger.Error("failed to send result")
				}
				close(t.result)
			}
			return
		case t := <-p.tasks:
			if t == nil {
				continue
			}
			result, err := vm.Execute(t.ctx, t.name, t.args...)
			select {
			case t.result <- taskResult{value: result, err: err}:
			default:
				p.logger.Error("failed to send result")
			}
			close(t.result)
		}
	}
}

// Close shuts down the pool
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.done)
		close(p.tasks)
		p.workerWait.Wait()
	})
}
