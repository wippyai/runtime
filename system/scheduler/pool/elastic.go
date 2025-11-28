package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/runtime"
)

// elasticWorker owns one process with idle tracking.
type elasticWorker struct {
	id         int
	process    process2.Process
	executor   *Executor
	pool       *Elastic
	lastActive time.Time
}

// Elastic grows and shrinks based on demand.
// Starts with minimal workers and scales up under load.
// Idle workers are gradually removed to save memory.
//
// Use cases:
//   - Spiking workloads with quiet periods
//   - Functions that may be called rarely but need fast response
//   - Memory-constrained environments
type Elastic struct {
	factory    Factory
	dispatcher Dispatcher
	executor   *Executor

	mu          sync.Mutex
	workers     []*elasticWorker
	idle        []*elasticWorker
	nextID      int
	minWorkers  int
	maxWorkers  int
	idleTimeout time.Duration

	tasks  chan *request
	done   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool

	reaper     *time.Ticker
	reaperDone chan struct{}
}

// ElasticConfig configures the elastic pool.
type ElasticConfig struct {
	MinWorkers  int
	MaxWorkers  int
	QueueSize   int
	IdleTimeout time.Duration
}

// NewElastic creates an elastic pool.
func NewElastic(factory Factory, dispatcher Dispatcher, cfg ElasticConfig) (*Elastic, error) {
	if cfg.MinWorkers <= 0 {
		cfg.MinWorkers = 1
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 16
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		cfg.MaxWorkers = cfg.MinWorkers
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = cfg.MaxWorkers * 64
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}

	e := &Elastic{
		factory:     factory,
		dispatcher:  dispatcher,
		executor:    NewExecutor(dispatcher),
		workers:     make([]*elasticWorker, 0, cfg.MaxWorkers),
		idle:        make([]*elasticWorker, 0, cfg.MaxWorkers),
		minWorkers:  cfg.MinWorkers,
		maxWorkers:  cfg.MaxWorkers,
		idleTimeout: cfg.IdleTimeout,
		tasks:       make(chan *request, cfg.QueueSize),
		done:        make(chan struct{}),
		reaperDone:  make(chan struct{}),
	}

	return e, nil
}

// Start initializes minimum workers and begins accepting calls.
func (e *Elastic) Start() {
	e.mu.Lock()
	for i := 0; i < e.minWorkers; i++ {
		if err := e.spawnWorkerLocked(); err != nil {
			break
		}
	}
	e.mu.Unlock()

	e.reaper = time.NewTicker(e.idleTimeout / 2)
	go e.runReaper()
}

// Stop gracefully shuts down the pool.
func (e *Elastic) Stop() {
	if e.closed.Swap(true) {
		return
	}
	close(e.done)

	if e.reaper != nil {
		e.reaper.Stop()
		close(e.reaperDone)
	}

	e.wg.Wait()

	e.mu.Lock()
	for _, w := range e.workers {
		w.process.Close()
	}
	for _, w := range e.idle {
		w.process.Close()
	}
	e.workers = nil
	e.idle = nil
	e.mu.Unlock()
}

// Call executes a function, spawning workers if needed.
func (e *Elastic) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if e.closed.Load() {
		return nil, fmt.Errorf("pool is closed")
	}

	req := &request{
		ctx:      ctx,
		method:   method,
		input:    input,
		resultCh: make(chan *runtime.Result, 1),
	}

	// Try to queue
	select {
	case e.tasks <- req:
		e.maybeSpawn()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.done:
		return nil, fmt.Errorf("pool is closed")
	}

	select {
	case result := <-req.resultCh:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// maybeSpawn adds a worker if queue is backing up.
func (e *Elastic) maybeSpawn() {
	qlen := len(e.tasks)
	if qlen == 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Try to use idle worker first
	if len(e.idle) > 0 {
		w := e.idle[len(e.idle)-1]
		e.idle = e.idle[:len(e.idle)-1]
		e.workers = append(e.workers, w)
		e.wg.Add(1)
		go e.runWorker(w)
		return
	}

	// Spawn new if under limit
	totalWorkers := len(e.workers) + len(e.idle)
	if totalWorkers < e.maxWorkers && qlen > len(e.workers) {
		_ = e.spawnWorkerLocked()
	}
}

// spawnWorkerLocked creates and starts a new worker. Caller holds mu.
func (e *Elastic) spawnWorkerLocked() error {
	proc, err := e.factory()
	if err != nil {
		return err
	}

	w := &elasticWorker{
		id:         e.nextID,
		process:    proc,
		executor:   e.executor,
		pool:       e,
		lastActive: time.Now(),
	}
	e.nextID++
	e.workers = append(e.workers, w)
	e.wg.Add(1)
	go e.runWorker(w)
	return nil
}

// runWorker processes tasks until idle or shutdown.
func (e *Elastic) runWorker(w *elasticWorker) {
	defer e.wg.Done()

	idleTimer := time.NewTimer(e.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-e.done:
			return

		case req := <-e.tasks:
			w.lastActive = time.Now()
			result := w.executor.Run(req.ctx, w.process, req.method, req.input)
			select {
			case req.resultCh <- result:
			default:
			}
			idleTimer.Reset(e.idleTimeout)

		case <-idleTimer.C:
			if e.tryPark(w) {
				return
			}
			idleTimer.Reset(e.idleTimeout)
		}
	}
}

// tryPark attempts to park a worker as idle. Returns true if parked.
func (e *Elastic) tryPark(w *elasticWorker) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Keep minimum workers active
	if len(e.workers) <= e.minWorkers {
		return false
	}

	// Remove from active
	for i, worker := range e.workers {
		if worker.id == w.id {
			e.workers = append(e.workers[:i], e.workers[i+1:]...)
			e.idle = append(e.idle, w)
			return true
		}
	}
	return false
}

// runReaper periodically removes idle workers over the limit.
func (e *Elastic) runReaper() {
	for {
		select {
		case <-e.reaperDone:
			return
		case <-e.reaper.C:
			e.reapIdle()
		}
	}
}

// reapIdle closes workers that have been idle too long.
func (e *Elastic) reapIdle() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	kept := e.idle[:0]

	for _, w := range e.idle {
		if now.Sub(w.lastActive) > e.idleTimeout*2 {
			w.process.Close()
		} else {
			kept = append(kept, w)
		}
	}
	e.idle = kept
}
