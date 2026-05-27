// SPDX-License-Identifier: MPL-2.0

package static

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler/pool"
)

// Config contains static pool configuration.
type Config struct {
	// Workers is the number of worker goroutines/processes.
	Workers int

	// QueueSize is the capacity of the work queue.
	// Calls block when queue is full.
	QueueSize int
}

// worker owns one process and pulls from shared queue.
type worker struct {
	pool     *Pool
	process  process.Process
	executor *pool.Executor
}

// Pool is a fixed-size pool with pre-allocated workers.
// Each worker owns one process and pulls from a shared channel queue.
type Pool struct {
	reqPool    sync.Pool
	dispatcher dispatcher.Dispatcher
	hooks      pool.ExecutionHooks
	tasks      chan *pool.Request
	factory    process.FactoryFunc
	done       chan struct{}
	gate       *pool.AdmissionGate
	active     sync.Map
	workers    []*worker
	wg         sync.WaitGroup
	closed     atomic.Bool
}

// New creates a static pool with the given configuration.
func New(factory process.FactoryFunc, d dispatcher.Dispatcher, cfg Config, hooks ...pool.ExecutionHooks) (*Pool, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = cfg.Workers * 256
	}

	var hooksCfg pool.ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
	}

	s := &Pool{
		workers:    make([]*worker, cfg.Workers),
		tasks:      make(chan *pool.Request, cfg.QueueSize),
		dispatcher: d,
		factory:    factory,
		hooks:      hooksCfg,
		done:       make(chan struct{}),
		gate:       pool.NewAdmissionGate(),
	}

	s.reqPool.New = func() any {
		return &pool.Request{ResultCh: make(chan *runtime.Result, 1)}
	}

	for i := 0; i < cfg.Workers; i++ {
		proc, err := factory()
		if err != nil {
			for j := 0; j < i; j++ {
				s.workers[j].process.Close()
			}
			return nil, err
		}

		s.workers[i] = &worker{
			pool:     s,
			process:  proc,
			executor: pool.NewExecutor(d).WithExecutionHooks(hooksCfg),
		}
	}

	return s, nil
}

// Start launches all worker goroutines.
func (s *Pool) Start() {
	for _, w := range s.workers {
		s.wg.Add(1)
		go w.run()
	}
}

// Stop signals workers to stop and waits for completion.
func (s *Pool) Stop() {
	if s.closed.Swap(true) {
		return
	}

	s.gate.Stop()

	close(s.done)
	s.wg.Wait()
	for _, w := range s.workers {
		w.process.Close()
	}
}

// Send implements relay.Receiver for message routing.
func (s *Pool) Send(pkg *relay.Package) error {
	v, ok := s.active.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
	}
	return v.(*pool.Executor).Send(pkg)
}

// Call executes a function call using an available worker.
func (s *Pool) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if !s.gate.Begin() {
		return nil, pool.ErrPoolClosed
	}
	defer s.gate.End()

	req := s.reqPool.Get().(*pool.Request)
	req.Ctx = ctx
	req.Method = method
	req.Input = input

	select {
	case s.tasks <- req:
	default:
		select {
		case s.tasks <- req:
		case <-ctx.Done():
			s.reqPool.Put(req)
			return nil, ctx.Err()
		case <-s.done:
			s.reqPool.Put(req)
			return nil, pool.ErrPoolClosed
		}
	}

	result := <-req.ResultCh
	s.reqPool.Put(req)
	return result, nil
}

func (w *worker) run() {
	defer w.pool.wg.Done()

	for {
		select {
		case <-w.pool.done:
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
	pid, _ := runtime.GetFramePID(req.Ctx)
	w.pool.active.Store(pid.UniqID, w.executor)

	result := w.executor.Run(req.Ctx, w.process, req.Method, req.Input)

	w.pool.active.Delete(pid.UniqID)
	w.executor.Reset()
	req.ResultCh <- result
}
