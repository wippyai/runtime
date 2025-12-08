package pool

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// request holds a pending function call.
type request struct {
	ctx      context.Context
	method   string
	input    payload.Payloads
	resultCh chan *runtime.Result
}

// staticWorker owns one process and pulls from shared queue.
type staticWorker struct {
	pool     *Static
	process  process.Process
	executor *Executor
	tasks    <-chan *request
	done     <-chan struct{}
	wg       *sync.WaitGroup
}

// Static is a fixed-size pool with pre-allocated workers.
// Each worker owns one process and pulls from a shared channel queue.
type Static struct {
	workers    []*staticWorker
	tasks      chan *request
	dispatcher Dispatcher
	factory    Factory
	hooks      ExecutionHooks
	done       chan struct{}
	wg         sync.WaitGroup
	closed     atomic.Bool

	// Active executions indexed by PID.UniqID for message routing
	active sync.Map // map[string]*Executor
}

// NewStatic creates a static pool with the given configuration.
func NewStatic(factory Factory, dispatcher Dispatcher, cfg Config, hooks ...ExecutionHooks) (*Static, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = cfg.Workers * 256
	}

	var hooksCfg ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
	}

	s := &Static{
		workers:    make([]*staticWorker, cfg.Workers),
		tasks:      make(chan *request, cfg.QueueSize),
		dispatcher: dispatcher,
		factory:    factory,
		hooks:      hooksCfg,
		done:       make(chan struct{}),
	}

	for i := 0; i < cfg.Workers; i++ {
		proc, err := factory()
		if err != nil {
			for j := 0; j < i; j++ {
				s.workers[j].process.Close()
			}
			return nil, err
		}

		// Each worker needs its own Executor to avoid races on multiCtx
		executor := NewExecutor(dispatcher).WithExecutionHooks(hooksCfg)

		s.workers[i] = &staticWorker{
			pool:     s,
			process:  proc,
			executor: executor,
			tasks:    s.tasks,
			done:     s.done,
			wg:       &s.wg,
		}
	}

	return s, nil
}

// Start launches all worker goroutines.
func (s *Static) Start() {
	for _, w := range s.workers {
		s.wg.Add(1)
		go w.run()
	}
}

// Stop signals workers to stop and waits for completion.
func (s *Static) Stop() {
	if s.closed.Swap(true) {
		return
	}
	close(s.done)
	s.wg.Wait()
	for _, w := range s.workers {
		w.process.Close()
	}
}

// Send implements relay.Receiver. Routes package to target execution.
func (s *Static) Send(pkg *relay.Package) error {
	v, ok := s.active.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
	}
	return v.(*Executor).Send(pkg)
}

// Call executes a function call using an available worker.
func (s *Static) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if s.closed.Load() {
		return nil, ErrPoolClosed
	}

	req := &request{
		ctx:      ctx,
		method:   method,
		input:    input,
		resultCh: make(chan *runtime.Result, 1),
	}

	// Fast path: try non-blocking send first (avoids select overhead when queue has room)
	select {
	case s.tasks <- req:
	default:
		// Queue full - fall back to blocking with cancellation
		select {
		case s.tasks <- req:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.done:
			return nil, ErrPoolClosed
		}
	}

	// Wait for result - worker always sends result even during shutdown
	// Worker drains tasks during shutdown, so result will arrive
	return <-req.resultCh, nil
}

// run is the worker's main loop.
func (w *staticWorker) run() {
	defer w.wg.Done()

	for {
		select {
		case <-w.done:
			w.drain()
			return
		case req := <-w.tasks:
			w.execute(req)
		}
	}
}

// drain processes remaining tasks during shutdown.
func (w *staticWorker) drain() {
	for {
		select {
		case req := <-w.tasks:
			w.execute(req)
		default:
			return
		}
	}
}

// execute runs a single request.
func (w *staticWorker) execute(req *request) {
	pid, _ := runtime.GetFramePID(req.ctx)
	w.pool.active.Store(pid.UniqID, w.executor)

	result := w.executor.Run(req.ctx, w.process, req.method, req.input)

	w.pool.active.Delete(pid.UniqID)
	req.resultCh <- result
}
