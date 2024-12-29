package queued

import (
	"context"
	"fmt"
	"github.com/ponyruntime/go-lua"
	lua2 "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"sync"
	"time"
)

// Task represents a single unit of work to be executed
type Task struct {
	name     string     // function name to execute
	args     lua.LValue // arguments for the function
	ctx      context.Context
	respChan chan TaskResult
}

// TaskResult holds the result of a task execution
type TaskResult struct {
	Value lua.LValue
	Err   error
}

// QueuedPool manages multiple Lua VMs with a task queue for load distribution
type QueuedPool struct {
	size           int
	defaultTimeout time.Duration
	logger         *zap.Logger
	vmConfig       VMConfig

	mu        sync.RWMutex
	vms       chan *engine.VM
	taskQueue chan *Task
	closeOnce sync.Once
	closeChan chan struct{}
	workerWg  sync.WaitGroup
}

// NewQueuedPool creates a new queued VM pool with the given options
func NewQueuedPool(opts ...Option) *QueuedPool {
	p := &QueuedPool{
		size:           5,           // Default pool size
		defaultTimeout: time.Minute, // Default timeout
		logger:         zap.NewNop(),
		vmConfig: VMConfig{
			Modules:   make(map[string]lua2.Module),
			Libraries: make(map[string]string),
			Globals:   make(map[string]lua.LValue),
			Functions: make(map[string]string),
		},
		taskQueue: make(chan *Task, 100), // Buffered task queue
		closeChan: make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Init initializes the pool and starts worker goroutines
func (p *QueuedPool) Init() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create buffered channel for VM pool
	p.vms = make(chan *engine.VM, p.size)

	// Create VMs and add them to the pool
	for i := 0; i < p.size; i++ {
		vm, err := p.createVM()
		if err != nil {
			// Cleanup already created VMs
			for j := 0; j < i; j++ {
				select {
				case vm := <-p.vms:
					vm.Close()
				default:
				}
			}
			return fmt.Errorf("failed to create VM %d: %w", i, err)
		}
		p.vms <- vm
	}

	// Start worker goroutines
	for i := 0; i < p.size; i++ {
		p.workerWg.Add(1)
		go p.worker()
	}

	return nil
}

// Execute queues a function execution and returns a channel for the result
func (p *QueuedPool) Execute(ctx context.Context, name string, args lua.LValue) (lua.LValue, error) {
	select {
	case <-p.closeChan:
		return nil, fmt.Errorf("pool is closed")
	default:
	}

	// Create response channel
	respChan := make(chan TaskResult, 1)

	// Create and queue the task
	task := &Task{
		name:     name,
		args:     args,
		ctx:      ctx,
		respChan: respChan,
	}

	// Try to queue the task
	select {
	case p.taskQueue <- task:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.defaultTimeout):
		return nil, fmt.Errorf("timeout queuing task")
	}

	// Wait for result
	select {
	case result := <-respChan:
		return result.Value, result.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.defaultTimeout):
		return nil, fmt.Errorf("timeout waiting for result")
	}
}

// worker processes tasks from the queue
func (p *QueuedPool) worker() {
	defer p.workerWg.Done()

	for {
		// Try to get a VM
		var vm *engine.VM
		select {
		case vm = <-p.vms:
			// Got a VM
		case <-p.closeChan:
			return
		}

		// Process tasks while we have a VM
		for {
			select {
			case task := <-p.taskQueue:
				// Execute the task
				result, err := vm.Execute(task.ctx, task.name, task.args)

				// Send result
				select {
				case task.respChan <- TaskResult{Value: result, Err: err}:
				default:
					p.logger.Warn("failed to send result - channel full or closed")
				}
				close(task.respChan)

				// If execution failed, recreate VM
				if err != nil {
					vm.Close()
					newVM, createErr := p.createVM()
					if createErr != nil {
						p.logger.Error("failed to recreate VM",
							zap.Error(createErr),
							zap.Error(err))
						// Return VM to pool if recreation failed
						p.vms <- vm
						break
					}
					vm = newVM
				}

			case <-p.closeChan:
				vm.Close()
				return

			default:
				// No tasks, return VM to pool
				select {
				case p.vms <- vm:
					// VM returned to pool, go back to VM acquisition
					break
				default:
					// SyncPool is full (shouldn't happen)
					vm.Close()
				}
				break
			}
		}
	}
}

// Close shuts down the pool and cleans up resources
func (p *QueuedPool) Close() {
	p.closeOnce.Do(func() {
		close(p.closeChan)

		// Wait for workers to finish
		p.workerWg.Wait()

		// Clean up VMs
		p.mu.Lock()
		defer p.mu.Unlock()

		if p.vms != nil {
		cleanup:
			for {
				select {
				case vm := <-p.vms:
					vm.Close()
				default:
					break cleanup
				}
			}
			close(p.vms)
			close(p.taskQueue)
		}
	})
}
