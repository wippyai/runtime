package pool

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// request holds a pending function call.
type request struct {
	ctx      context.Context
	method   string
	input    payload.Payloads
	resultCh chan *runtime.Result
}

// requestPool pools request structs to reduce allocations.
var requestPool = sync.Pool{
	New: func() any {
		return &request{
			resultCh: make(chan *runtime.Result, 1),
		}
	},
}

func acquireRequest(ctx context.Context, method string, input payload.Payloads) *request {
	req := requestPool.Get().(*request)
	req.ctx = ctx
	req.method = method
	req.input = input
	return req
}

func releaseRequest(req *request) {
	req.ctx = nil
	req.method = ""
	req.input = nil
	select {
	case <-req.resultCh:
	default:
	}
	requestPool.Put(req)
}

// staticWorker owns one process and pulls from shared queue.
type staticWorker struct {
	process  process.Process
	executor *Executor
	tasks    <-chan *request
	done     <-chan struct{}
	wg       *sync.WaitGroup
}

// Static is a fixed-size pool using a channel-based work queue.
// Workers block on the shared channel - Go runtime handles scheduling.
//
// Use cases:
//   - HTTP handlers with steady high load
//   - Functions called at predictable rates
//   - When work-stealing overhead is not needed
type Static struct {
	workers    []*staticWorker
	tasks      chan *request
	dispatcher Dispatcher
	done       chan struct{}
	wg         sync.WaitGroup
	closed     atomic.Bool
}

// NewStatic creates a static pool with the given configuration.
func NewStatic(factory Factory, dispatcher Dispatcher, cfg Config, hooks ...ExecutionHooks) (*Static, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = cfg.Workers * 256
	}

	s := &Static{
		workers:    make([]*staticWorker, cfg.Workers),
		tasks:      make(chan *request, cfg.QueueSize),
		dispatcher: dispatcher,
		done:       make(chan struct{}),
	}

	executor := NewExecutor(dispatcher)
	if len(hooks) > 0 {
		executor = executor.WithExecutionHooks(hooks[0])
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

// Call executes a function call using an available worker.
func (s *Static) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if s.closed.Load() {
		return nil, ErrPoolClosed
	}

	req := acquireRequest(ctx, method, input)

	select {
	case s.tasks <- req:
	case <-ctx.Done():
		releaseRequest(req)
		return nil, ctx.Err()
	case <-s.done:
		releaseRequest(req)
		return nil, ErrPoolClosed
	}

	select {
	case result := <-req.resultCh:
		releaseRequest(req)
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
	result := w.executor.Run(req.ctx, w.process, req.method, req.input)
	select {
	case req.resultCh <- result:
	default:
	}
}
