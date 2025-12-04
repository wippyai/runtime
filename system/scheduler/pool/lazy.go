package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// Lazy creates processes on demand and destroys them after idle timeout.
// Zero memory footprint when no work is happening.
// Implements relay.Receiver for message delivery to running processes.
//
// Lifecycle:
//   - No processes at init
//   - Creates process when call arrives and no idle process available
//   - Reuses idle processes for subsequent calls
//   - Destroys all processes after idle timeout
//
// Use cases:
//   - Rarely called functions
//   - Memory-constrained environments
//   - Functions that may never be called but need to exist
type Lazy struct {
	factory     Factory
	dispatcher  Dispatcher
	hooks       ExecutionHooks
	maxWorkers  int
	idleTimeout time.Duration

	mu       sync.Mutex
	idle     []process.Process
	waiters  []chan struct{}
	active   int
	lastUsed time.Time

	done       chan struct{}
	closed     atomic.Bool
	reaperDone chan struct{}
	reaper     *time.Ticker

	// Active executions indexed by PID.UniqID for message routing
	activeExec sync.Map // map[string]*Executor

	// Executor pool for reuse
	executors sync.Pool
}

// LazyConfig configures the lazy pool.
type LazyConfig struct {
	MaxWorkers  int
	IdleTimeout time.Duration
}

// NewLazy creates a lazy pool that starts with zero processes.
func NewLazy(factory Factory, dispatcher Dispatcher, cfg LazyConfig, hooks ...ExecutionHooks) (*Lazy, error) {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 16
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}

	var hooksCfg ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
	}

	l := &Lazy{
		factory:     factory,
		dispatcher:  dispatcher,
		hooks:       hooksCfg,
		maxWorkers:  cfg.MaxWorkers,
		idleTimeout: cfg.IdleTimeout,
		idle:        make([]process.Process, 0, cfg.MaxWorkers),
		done:        make(chan struct{}),
		reaperDone:  make(chan struct{}),
	}
	l.executors.New = func() any {
		return NewExecutor(dispatcher).WithExecutionHooks(hooksCfg)
	}
	return l, nil
}

// Start begins the idle reaper.
func (l *Lazy) Start() {
	l.reaper = time.NewTicker(l.idleTimeout / 2)
	go l.runReaper()
}

// Stop shuts down and destroys all processes.
func (l *Lazy) Stop() {
	if l.closed.Swap(true) {
		return
	}
	close(l.done)

	if l.reaper != nil {
		l.reaper.Stop()
		close(l.reaperDone)
	}

	l.mu.Lock()
	for _, proc := range l.idle {
		proc.Close()
	}
	l.idle = nil
	l.mu.Unlock()
}

// Call executes using an idle or newly created process.
func (l *Lazy) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if l.closed.Load() {
		return nil, ErrPoolClosed
	}

	proc, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}

	// Get PID from frame context (set by function registry)
	pid, _ := runtime.GetFramePID(ctx)

	// Get executor from pool, set process, register
	executor := l.executors.Get().(*Executor)
	executor.proc = proc
	l.activeExec.Store(pid.UniqID, executor)

	result := executor.Run(ctx, proc, method, input)

	// Unregister and return executor to pool
	l.activeExec.Delete(pid.UniqID)
	executor.Reset()
	l.executors.Put(executor)

	l.release(proc)
	return result, nil
}

// Send implements relay.Receiver. Routes package to target execution.
func (l *Lazy) Send(pkg *relay.Package) error {
	v, ok := l.activeExec.Load(pkg.Target.UniqID)
	if !ok {
		return ErrProcessNotFound
	}
	return v.(*Executor).Send(pkg)
}

// acquire gets an idle process or creates a new one.
// Waits if at max workers until one becomes available or ctx is cancelled.
func (l *Lazy) acquire(ctx context.Context) (process.Process, error) {
	for {
		// Check context first
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-l.done:
			return nil, ErrPoolClosed
		default:
		}

		l.mu.Lock()

		// Try to get idle process
		if len(l.idle) > 0 {
			proc := l.idle[len(l.idle)-1]
			l.idle = l.idle[:len(l.idle)-1]
			l.active++
			l.mu.Unlock()
			return proc, nil
		}

		// Check if we can create more
		if l.active < l.maxWorkers {
			l.active++
			l.mu.Unlock()

			// Create new process outside lock
			proc, err := l.factory()
			if err != nil {
				l.mu.Lock()
				l.active--
				// Wake one waiter on factory error
				if len(l.waiters) > 0 {
					waiter := l.waiters[0]
					l.waiters = l.waiters[1:]
					l.mu.Unlock()
					select {
					case waiter <- struct{}{}:
					default:
					}
				} else {
					l.mu.Unlock()
				}
				return nil, err
			}

			return proc, nil
		}

		// At max workers - wait for signal via channel
		waiter := make(chan struct{}, 1)
		l.waiters = append(l.waiters, waiter)
		l.mu.Unlock()

		// Wait for worker release or context cancellation
		select {
		case <-waiter:
			// Woken up - retry
		case <-ctx.Done():
			// Remove from waiters
			l.mu.Lock()
			for i, w := range l.waiters {
				if w == waiter {
					l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
					break
				}
			}
			l.mu.Unlock()
			return nil, ctx.Err()
		case <-l.done:
			return nil, ErrPoolClosed
		}
	}
}

// release returns process to idle pool.
func (l *Lazy) release(proc process.Process) {
	l.mu.Lock()

	l.active--
	l.lastUsed = time.Now()

	if l.closed.Load() {
		l.mu.Unlock()
		proc.Close()
		return
	}

	l.idle = append(l.idle, proc)

	// Wake one waiter if any
	if len(l.waiters) > 0 {
		waiter := l.waiters[0]
		l.waiters = l.waiters[1:]
		l.mu.Unlock()
		select {
		case waiter <- struct{}{}:
		default:
		}
		return
	}

	l.mu.Unlock()
}

// runReaper periodically destroys idle processes.
func (l *Lazy) runReaper() {
	for {
		select {
		case <-l.reaperDone:
			return
		case <-l.reaper.C:
			l.reapIdle()
		}
	}
}

// reapIdle destroys all processes if pool has been idle long enough.
func (l *Lazy) reapIdle() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Don't reap if there's active work or recent activity
	if l.active > 0 {
		return
	}

	if time.Since(l.lastUsed) < l.idleTimeout {
		return
	}

	// Destroy all idle processes
	for _, proc := range l.idle {
		proc.Close()
	}
	l.idle = l.idle[:0]
}
