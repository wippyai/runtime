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
	pool     *Static
	process  process.Process
	executor *Executor
	tasks    <-chan *request
	done     <-chan struct{}
	wg       *sync.WaitGroup
}

// Static is a fixed-size pool using a channel-based work queue.
// Workers block on the shared channel - Go runtime handles scheduling.
// Implements relay.Receiver for message delivery to running processes.
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

	s := &Static{
		workers:    make([]*staticWorker, cfg.Workers),
		tasks:      make(chan *request, cfg.QueueSize),
		dispatcher: dispatcher,
		done:       make(chan struct{}),
	}

	var hooksCfg ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
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
		executor.proc = proc // Set once, immutable for this worker's lifetime

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
	println("[DEBUG] Static.Send: Target.Host=", pkg.Target.Host, "Target.UniqID=", pkg.Target.UniqID)
	v, ok := s.active.Load(pkg.Target.UniqID)
	if !ok {
		println("[DEBUG] Static.Send: NOT FOUND in active map")
		return ErrProcessNotFound
	}
	println("[DEBUG] Static.Send: found executor, forwarding")
	return v.(*Executor).Send(pkg)
}

// Call executes a function call using an available worker.
func (s *Static) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	if s.closed.Load() {
		return nil, ErrPoolClosed
	}

	req := acquireRequest(ctx, method, input)

	// Try to submit request to worker queue
	select {
	case s.tasks <- req:
	case <-ctx.Done():
		releaseRequest(req)
		return nil, ctx.Err()
	case <-s.done:
		releaseRequest(req)
		return nil, ErrPoolClosed
	}

	// Wait for worker to complete. We must always wait for the result
	// before releasing the request, even if context is cancelled or pool stops.
	// Worker checks ctx.Done() in yield handling and returns early.
	// We cannot return until worker is done to prevent races with ResponseWriter.
	var result *runtime.Result
	var poolStopped bool
	select {
	case result = <-req.resultCh:
	case <-s.done:
		poolStopped = true
		result = <-req.resultCh
	}

	releaseRequest(req)

	// Return appropriate error based on what happened
	if poolStopped {
		return nil, ErrPoolClosed
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, nil
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
	// Get PID from frame context (set by function registry)
	ctx := req.ctx
	pid, _ := runtime.GetFramePID(ctx)
	w.pool.active.Store(pid.UniqID, w.executor)

	result := w.executor.Run(ctx, w.process, req.method, req.input)

	// Unregister - any Send after this returns ErrProcessNotFound
	w.pool.active.Delete(pid.UniqID)

	select {
	case req.resultCh <- result:
	default:
	}
}
