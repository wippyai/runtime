// SPDX-License-Identifier: MPL-2.0

package adaptive

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
	"go.uber.org/zap"
)

const (
	DefaultMaxWorkers      = 16
	DefaultQueueMultiplier = 64
)

// Pool is an adaptive worker pool that scales based on throughput optimization.
// Uses probe-based scaling: add worker, measure improvement, keep or remove.
type Pool struct {
	reqPool      sync.Pool
	executors    sync.Pool
	dispatcher   dispatcher.Dispatcher
	hooks        pool.ExecutionHooks
	gate         *pool.AdmissionGate
	ctrl         *controller
	log          *zap.Logger
	factory      process.FactoryFunc
	tasks        chan *pool.Request
	done         chan struct{}
	active       sync.Map
	workers      []*worker
	wg           sync.WaitGroup
	minWorkers   int
	completedOps atomic.Int64
	queueSize    int
	maxWorkers   int
	startOnce    sync.Once
	ctrlMu       sync.Mutex
	mu           sync.Mutex
	busyWorkers  atomic.Int32
	workerCount  atomic.Int32
	closed       atomic.Bool
}

// worker processes tasks from the shared queue.
type worker struct {
	pool     *Pool
	process  process.Process
	executor *pool.Executor
	stop     chan struct{}
}

// Option configures an adaptive pool.
type Option func(*Pool)

// WithMaxWorkers sets the maximum number of workers.
func WithMaxWorkers(n int) Option {
	return func(a *Pool) {
		if n > 0 {
			a.maxWorkers = n
		}
	}
}

// WithControlInterval sets how often scaling decisions are evaluated.
func WithControlInterval(d time.Duration) Option {
	return func(a *Pool) {
		if a.ctrl != nil && d > 0 {
			a.ctrl.ControlInterval = d
			a.ctrl.cooldown = a.ctrl.Cooldown()
		}
	}
}

// WithIdleTicks sets ticks of low utilization before scale-down.
func WithIdleTicks(n int) Option {
	return func(a *Pool) {
		if a.ctrl != nil && n > 0 {
			a.ctrl.IdleTicks = n
		}
	}
}

// WithProbeTicks sets ticks to evaluate probe result.
func WithProbeTicks(n int) Option {
	return func(a *Pool) {
		if a.ctrl != nil && n > 0 {
			a.ctrl.ProbeTicks = n
			a.ctrl.cooldown = a.ctrl.Cooldown()
		}
	}
}

// WithLogger sets the logger.
func WithLogger(log *zap.Logger) Option {
	return func(a *Pool) {
		a.log = log
		if a.ctrl != nil {
			a.ctrl.log = log
		}
	}
}

// WithExecutionHooks sets hooks for execution events.
func WithExecutionHooks(h pool.ExecutionHooks) Option {
	return func(a *Pool) {
		a.hooks = h
	}
}

// New creates an adaptive pool.
func New(factory process.FactoryFunc, d dispatcher.Dispatcher, opts ...Option) (*Pool, error) {
	cfg := DefaultControllerConfig(DefaultMaxWorkers)
	ctrl := newController(cfg)

	a := &Pool{
		factory:    factory,
		dispatcher: d,
		maxWorkers: DefaultMaxWorkers,
		minWorkers: 1,
		ctrl:       ctrl,
		done:       make(chan struct{}),
		gate:       pool.NewAdmissionGate(),
	}

	for _, opt := range opts {
		opt(a)
	}

	// Sync controller config with pool config
	a.ctrl.MaxWorkers = a.maxWorkers
	a.ctrl.MinWorkers = a.minWorkers

	if a.queueSize <= 0 {
		a.queueSize = a.maxWorkers * DefaultQueueMultiplier
	}

	a.tasks = make(chan *pool.Request, a.queueSize)
	a.workers = make([]*worker, 0, a.maxWorkers)

	a.reqPool.New = func() any {
		return &pool.Request{ResultCh: make(chan *runtime.Result, 1)}
	}

	a.executors.New = func() any {
		return pool.NewExecutor(d).WithExecutionHooks(a.hooks)
	}

	return a, nil
}

// Start launches the pool.
func (a *Pool) Start() {
	a.startOnce.Do(func() {
		if a.log != nil {
			a.log.Debug("starting pool",
				zap.Int("min", a.minWorkers),
				zap.Int("max", a.maxWorkers),
				zap.Int("queue", cap(a.tasks)))
		}

		for i := 0; i < a.minWorkers; i++ {
			if err := a.spawnWorker(); err != nil {
				if a.log != nil {
					a.log.Error("failed to spawn initial worker", zap.Error(err))
				}
				break
			}
		}

		a.wg.Add(1)
		go a.controlLoop()
	})
}

// Stop gracefully shuts down the pool.
func (a *Pool) Stop() {
	if a.closed.Swap(true) {
		return
	}

	a.gate.Stop()

	close(a.done)

	a.mu.Lock()
	for _, w := range a.workers {
		close(w.stop)
	}
	a.mu.Unlock()

	a.wg.Wait()

	if a.log != nil {
		a.log.Debug("pool stopped")
	}
}

// WorkerCount returns current worker count.
func (a *Pool) WorkerCount() int32 {
	return a.workerCount.Load()
}

// BusyWorkers returns current busy worker count.
func (a *Pool) BusyWorkers() int32 {
	return a.busyWorkers.Load()
}

// QueueLen returns current queue length.
func (a *Pool) QueueLen() int {
	return len(a.tasks)
}

// Send implements relay.Receiver for message routing.
func (a *Pool) Send(pkg *relay.Package) error {
	v, ok := a.active.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
	}
	return v.(*pool.Executor).Send(pkg)
}

// Call executes a function call using an available worker.
func (a *Pool) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if !a.gate.Begin() {
		return nil, pool.ErrPoolClosed
	}
	defer a.gate.End()

	req := a.reqPool.Get().(*pool.Request)
	req.Ctx = ctx
	req.Method = method
	req.Input = input

	select {
	case a.tasks <- req:
	default:
		select {
		case a.tasks <- req:
		case <-ctx.Done():
			a.reqPool.Put(req)
			return nil, ctx.Err()
		case <-a.done:
			a.reqPool.Put(req)
			return nil, pool.ErrPoolClosed
		}
	}

	result := <-req.ResultCh
	a.reqPool.Put(req)
	return result, nil
}

func (a *Pool) spawnWorker() error {
	a.mu.Lock()
	if a.closed.Load() {
		a.mu.Unlock()
		return nil
	}
	if len(a.workers) >= a.maxWorkers {
		a.mu.Unlock()
		return nil
	}

	proc, err := a.factory()
	if err != nil {
		a.mu.Unlock()
		return err
	}

	w := &worker{
		pool:     a,
		process:  proc,
		executor: a.executors.Get().(*pool.Executor),
		stop:     make(chan struct{}),
	}
	a.workers = append(a.workers, w)
	if len(a.workers) <= (1<<31 - 1) {
		a.workerCount.Store(int32(len(a.workers)))
	}
	a.mu.Unlock()

	a.wg.Add(1)
	go w.run()

	return nil
}

func (a *Pool) removeWorker() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.workers) <= a.minWorkers {
		return false
	}

	idx := len(a.workers) - 1
	w := a.workers[idx]
	a.workers = a.workers[:idx]
	if len(a.workers) <= (1<<31 - 1) {
		a.workerCount.Store(int32(len(a.workers)))
	}

	close(w.stop)
	return true
}

func (a *Pool) removeWorkersTo(target int32) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if target < 0 {
		target = 0
	}
	for int32(len(a.workers)) > target && len(a.workers) > a.minWorkers {
		idx := len(a.workers) - 1
		w := a.workers[idx]
		a.workers = a.workers[:idx]
		close(w.stop)
	}
	a.workerCount.Store(int32(len(a.workers)))
}

func (a *Pool) controlLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.ctrl.ControlInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.done:
			return
		case now := <-ticker.C:
			a.control(now)
		}
	}
}

func (a *Pool) control(now time.Time) {
	if a.closed.Load() {
		return
	}

	workers := a.workerCount.Load()
	busy := a.busyWorkers.Load()
	if busy > workers {
		busy = workers
	}
	queueLen := len(a.tasks)
	ops := a.completedOps.Load()

	a.ctrlMu.Lock()
	decision, target := a.ctrl.tick(now, ops, workers, busy, queueLen)
	a.ctrlMu.Unlock()

	switch decision {
	case scaleNone:
		// no action
	case scaleUp:
		// target = workers to add (multiplicative scale-up)
		for i := int32(0); i < target; i++ {
			if err := a.spawnWorker(); err != nil {
				break
			}
		}
	case probeSuccess:
		// keep the workers added during probe
	case probeFail:
		// target = workers to remove (rollback probe)
		for i := int32(0); i < target; i++ {
			if !a.removeWorker() {
				break
			}
		}
	case scaleDown:
		a.removeWorkersTo(target)
	}
}

func (w *worker) run() {
	defer w.pool.wg.Done()
	defer func() {
		w.process.Close()
		w.pool.executors.Put(w.executor)
	}()

	for {
		select {
		case <-w.pool.done:
			w.drain()
			return
		case <-w.stop:
			w.drain()
			return
		case req := <-w.pool.tasks:
			w.execute(req)
		}
	}
}

func (w *worker) drain() {
	for {
		select {
		case req := <-w.pool.tasks:
			w.execute(req)
		default:
			return
		}
	}
}

func (w *worker) execute(req *pool.Request) {
	w.pool.busyWorkers.Add(1)

	pid, _ := runtime.GetFramePID(req.Ctx)
	w.pool.active.Store(pid.UniqID, w.executor)

	result := w.executor.Run(req.Ctx, w.process, req.Method, req.Input)

	w.pool.active.Delete(pid.UniqID)
	w.executor.Reset()
	w.pool.busyWorkers.Add(-1)
	w.pool.completedOps.Add(1)

	req.ResultCh <- result
}
