// SPDX-License-Identifier: MPL-2.0

package lazy

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler/pool"
)

// Pool creates processes on demand and destroys them after idle timeout.
// Starts with zero processes, scales up to MaxWorkers based on load.
type Pool struct {
	executors   sync.Pool
	lastUsed    time.Time
	dispatcher  dispatcher.Dispatcher
	hooks       pool.ExecutionHooks
	gate        *pool.AdmissionGate
	reaperDone  chan struct{}
	reaper      *time.Ticker
	done        chan struct{}
	factory     process.FactoryFunc
	activeExec  sync.Map
	idle        []process.Process
	waiters     []chan struct{}
	idleTimeout time.Duration
	active      int
	maxWorkers  int
	startOnce   sync.Once
	mu          sync.Mutex
	closed      atomic.Bool
}

// Config configures the lazy pool.
type Config struct {
	MaxWorkers  int
	IdleTimeout time.Duration
}

// New creates a lazy pool that starts with zero processes.
func New(factory process.FactoryFunc, d dispatcher.Dispatcher, cfg Config, hooks ...pool.ExecutionHooks) (*Pool, error) {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 16
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}

	var hooksCfg pool.ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
	}

	l := &Pool{
		factory:     factory,
		dispatcher:  d,
		hooks:       hooksCfg,
		maxWorkers:  cfg.MaxWorkers,
		idleTimeout: cfg.IdleTimeout,
		idle:        make([]process.Process, 0, cfg.MaxWorkers),
		done:        make(chan struct{}),
		reaperDone:  make(chan struct{}),
		gate:        pool.NewAdmissionGate(),
	}
	l.executors.New = func() any {
		return pool.NewExecutor(d).WithExecutionHooks(hooksCfg)
	}
	return l, nil
}

// Start begins the idle reaper.
func (l *Pool) Start() {
	l.startOnce.Do(func() {
		l.mu.Lock()
		l.reaper = time.NewTicker(l.idleTimeout / 2)
		l.mu.Unlock()
		go l.runReaper()
	})
}

// Stop shuts down and destroys all processes.
func (l *Pool) Stop() {
	if l.closed.Swap(true) {
		return
	}

	l.mu.Lock()
	reaper := l.reaper
	l.mu.Unlock()

	if reaper != nil {
		reaper.Stop()
		close(l.reaperDone)
	}

	l.gate.Stop()

	close(l.done)

	l.mu.Lock()
	for _, proc := range l.idle {
		proc.Close()
	}
	l.idle = nil
	l.mu.Unlock()
}

// Call executes using an idle or newly created process.
func (l *Pool) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if !l.gate.Begin() {
		return nil, pool.ErrPoolClosed
	}
	defer l.gate.End()

	proc, err := l.acquire(ctx)
	if err != nil {
		return nil, err
	}

	pid, _ := runtime.GetFramePID(ctx)
	executor := l.executors.Get().(*pool.Executor)
	l.activeExec.Store(pid.UniqID, executor)

	result := executor.Run(ctx, proc, method, input)

	l.activeExec.Delete(pid.UniqID)
	executor.Reset()
	l.executors.Put(executor)

	l.release(proc)
	return result, nil
}

// Send implements relay.Receiver. Routes package to target execution.
func (l *Pool) Send(pkg *relay.Package) error {
	v, ok := l.activeExec.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
	}
	return v.(*pool.Executor).Send(pkg)
}

// acquire gets an idle process or creates a new one.
// Waits if at max workers until one becomes available or ctx is canceled.
func (l *Pool) acquire(ctx context.Context) (process.Process, error) {
	for {
		// Check context first
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-l.done:
			return nil, pool.ErrPoolClosed
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
			return nil, pool.ErrPoolClosed
		}
	}
}

// release returns process to idle pool.
func (l *Pool) release(proc process.Process) {
	l.mu.Lock()

	l.active--
	l.lastUsed = time.Now()

	// Wake one waiter if any
	if len(l.waiters) > 0 {
		l.idle = append(l.idle, proc)
		waiter := l.waiters[0]
		l.waiters = l.waiters[1:]
		l.mu.Unlock()
		select {
		case waiter <- struct{}{}:
		default:
		}
		return
	}

	if l.closed.Load() {
		l.mu.Unlock()
		proc.Close()
		return
	}

	l.idle = append(l.idle, proc)
	l.mu.Unlock()
}

// runReaper periodically destroys idle processes.
func (l *Pool) runReaper() {
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
func (l *Pool) reapIdle() {
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
