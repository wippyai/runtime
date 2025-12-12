package pool

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
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
}

// Static is a fixed-size pool with pre-allocated workers.
// Each worker owns one process and pulls from a shared channel queue.
type Static struct {
	workers    []*staticWorker
	tasks      chan *request
	dispatcher dispatcher.Dispatcher
	factory    process.FactoryFunc
	hooks      ExecutionHooks
	done       chan struct{}
	wg         sync.WaitGroup
	closed     atomic.Bool
	reqPool    sync.Pool

	active sync.Map
}

// NewStatic creates a static pool with the given configuration.
func NewStatic(factory process.FactoryFunc, d dispatcher.Dispatcher, cfg Config, hooks ...ExecutionHooks) (*Static, error) {
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
		dispatcher: d,
		factory:    factory,
		hooks:      hooksCfg,
		done:       make(chan struct{}),
	}

	s.reqPool.New = func() any {
		return &request{resultCh: make(chan *runtime.Result, 1)}
	}

	for i := 0; i < cfg.Workers; i++ {
		proc, err := factory()
		if err != nil {
			for j := 0; j < i; j++ {
				s.workers[j].process.Close()
			}
			return nil, err
		}

		s.workers[i] = &staticWorker{
			pool:     s,
			process:  proc,
			executor: NewExecutor(d).WithExecutionHooks(hooksCfg),
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

// Send implements relay.Receiver for message routing.
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

	req := s.reqPool.Get().(*request)
	req.ctx = ctx
	req.method = method
	req.input = input

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
			return nil, ErrPoolClosed
		}
	}

	result := <-req.resultCh
	s.reqPool.Put(req)
	return result, nil
}

func (w *staticWorker) run() {
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

func (w *staticWorker) drain() {
	for {
		select {
		case req := <-w.pool.tasks:
			w.execute(req)
		default:
			return
		}
	}
}

func (w *staticWorker) execute(req *request) {
	pid, _ := runtime.GetFramePID(req.ctx)
	w.pool.active.Store(pid.UniqID, w.executor)

	result := w.executor.Run(req.ctx, w.process, req.method, req.input)

	w.pool.active.Delete(pid.UniqID)
	req.resultCh <- result
}
