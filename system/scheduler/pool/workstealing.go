package pool

import (
	"context"
	"fmt"
	"math/rand"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/runtime"
)

// wsRequest holds a pending call for work-stealing scheduler.
type wsRequest struct {
	data     atomic.Pointer[wsCallData]
	resultCh chan *runtime.Result
	canceled atomic.Int32
	state    atomic.Int32
}

type wsCallData struct {
	ctx    context.Context
	method string
	input  payload.Payloads
}

const (
	wsStateFree      int32 = 0
	wsStateAcquired  int32 = 1
	wsStateExecuting int32 = 2
	wsStateCompleted int32 = 3
)

var wsRequestPool = sync.Pool{
	New: func() any {
		return &wsRequest{
			resultCh: make(chan *runtime.Result, 1),
		}
	},
}

func acquireWSRequest(ctx context.Context, method string, input payload.Payloads) *wsRequest {
	req := wsRequestPool.Get().(*wsRequest)
	data := &wsCallData{ctx: ctx, method: method, input: input}
	req.data.Store(data)
	req.canceled.Store(0)
	req.state.Store(wsStateAcquired)
	return req
}

func releaseWSRequest(req *wsRequest) {
	for {
		state := req.state.Load()
		if state == wsStateCompleted || state == wsStateFree {
			break
		}
		goruntime.Gosched()
	}
	req.data.Store(nil)
	select {
	case <-req.resultCh:
	default:
	}
	req.state.Store(wsStateFree)
	wsRequestPool.Put(req)
}

// wsWorker owns one process and participates in work-stealing.
type wsWorker struct {
	id       int
	process  process2.Process
	pool     *WorkStealing
	executor *Executor
	local    *wsDeque
	lifoSlot atomic.Pointer[wsRequest]
	batchBuf [32]*wsRequest
	rng      *rand.Rand
	executed atomic.Uint64
	stolen   atomic.Uint64
	spins    int
}

// WorkStealing implements work-stealing scheduling.
// Workers have local deques and steal from each other when idle.
//
// Use cases:
//   - Long-running tasks with varying execution times
//   - When work distribution is uneven
//   - CPU-bound workloads that benefit from cache locality
type WorkStealing struct {
	workers    []*wsWorker
	global     *wsQueue
	dispatcher Dispatcher
	stopping   atomic.Bool
	wg         sync.WaitGroup

	wakeMu    sync.Mutex
	wakeCond  *sync.Cond
	nspinning atomic.Int32
	nparked   atomic.Int32

	localQueueSize int
}

// WorkStealingConfig configures work-stealing pool.
type WorkStealingConfig struct {
	Workers        int
	QueueSize      int
	LocalQueueSize int
}

const (
	wsSpinLimit  = 4
	wsYieldLimit = 16
)

// NewWorkStealing creates a work-stealing pool.
func NewWorkStealing(factory Factory, dispatcher Dispatcher, cfg WorkStealingConfig, hooks ...ExecutionHooks) (*WorkStealing, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = goruntime.GOMAXPROCS(0)
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.LocalQueueSize <= 0 {
		cfg.LocalQueueSize = 64
	}

	ws := &WorkStealing{
		workers:        make([]*wsWorker, cfg.Workers),
		global:         newWSQueue(cfg.QueueSize),
		dispatcher:     dispatcher,
		localQueueSize: cfg.LocalQueueSize,
	}
	ws.wakeCond = sync.NewCond(&ws.wakeMu)

	executor := NewExecutor(dispatcher)
	if len(hooks) > 0 {
		executor = executor.WithExecutionHooks(hooks[0])
	}

	for i := 0; i < cfg.Workers; i++ {
		proc, err := factory()
		if err != nil {
			for j := 0; j < i; j++ {
				ws.workers[j].process.Close()
			}
			return nil, err
		}

		ws.workers[i] = &wsWorker{
			id:       i,
			process:  proc,
			pool:     ws,
			executor: executor,
			local:    newWSDeque(cfg.LocalQueueSize),
			rng:      rand.New(rand.NewSource(time.Now().UnixNano() + int64(i))),
		}
	}

	return ws, nil
}

// Start launches all worker goroutines.
func (ws *WorkStealing) Start() {
	for _, w := range ws.workers {
		ws.wg.Add(1)
		go w.run()
	}
}

// Stop signals workers to stop and waits for completion.
func (ws *WorkStealing) Stop() {
	ws.stopping.Store(true)
	ws.wakeMu.Lock()
	ws.wakeCond.Broadcast()
	ws.wakeMu.Unlock()
	ws.wg.Wait()
	for _, w := range ws.workers {
		w.process.Close()
	}
}

// wakeWorker signals idle workers.
func (ws *WorkStealing) wakeWorker() {
	ws.wakeMu.Lock()
	nparked := ws.nparked.Load()
	if nparked > 0 {
		qlen := ws.global.len()
		if qlen == 0 {
			qlen = 1
		}
		toWake := qlen
		if toWake > int(nparked) {
			toWake = int(nparked)
		}
		for i := 0; i < toWake; i++ {
			ws.wakeCond.Signal()
		}
	} else if ws.nspinning.Load() == 0 {
		ws.wakeCond.Signal()
	}
	ws.wakeMu.Unlock()
}

// Call executes a function call.
func (ws *WorkStealing) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	req := acquireWSRequest(ctx, method, input)
	ws.global.push(req)
	ws.wakeWorker()

	select {
	case result := <-req.resultCh:
		releaseWSRequest(req)
		return result, nil
	case <-ctx.Done():
		req.canceled.Store(1)
		return nil, ctx.Err()
	}
}

// run is the worker's main loop.
func (w *wsWorker) run() {
	defer w.pool.wg.Done()
	spinning := false

	for {
		req := w.findWork()
		if req != nil {
			if spinning {
				spinning = false
				if w.pool.nspinning.Add(-1) == 0 {
					w.pool.wakeWorker()
				}
			}
			w.spins = 0
			w.executeRequest(req)
			continue
		}

		if w.pool.stopping.Load() {
			if spinning {
				w.pool.nspinning.Add(-1)
			}
			return
		}

		if !spinning {
			spinning = true
			w.pool.nspinning.Add(1)
		}

		w.spins++
		if w.spins < wsSpinLimit {
			continue
		}
		if w.spins < wsYieldLimit {
			goruntime.Gosched()
			continue
		}

		spinning = false
		w.pool.nspinning.Add(-1)

		if req := w.findWork(); req != nil {
			w.spins = 0
			w.executeRequest(req)
			continue
		}

		w.pool.wakeMu.Lock()
		w.pool.nparked.Add(1)

		for {
			if w.pool.stopping.Load() {
				w.pool.nparked.Add(-1)
				w.pool.wakeMu.Unlock()
				return
			}

			if req := w.findWork(); req != nil {
				w.pool.nparked.Add(-1)
				w.pool.wakeMu.Unlock()
				w.spins = 0
				w.executeRequest(req)
				break
			}

			w.pool.wakeCond.Wait()
		}
	}
}

// executeRequest runs a request.
func (w *wsWorker) executeRequest(req *wsRequest) {
	if req.canceled.Load() == 0 {
		req.state.Store(wsStateExecuting)
		result := w.execute(req)
		req.state.Store(wsStateCompleted)
		req.resultCh <- result
		w.executed.Add(1)
	} else {
		req.state.Store(wsStateCompleted)
		releaseWSRequest(req)
	}
}

// findWork searches for work in priority order.
func (w *wsWorker) findWork() *wsRequest {
	if req := w.lifoSlot.Swap(nil); req != nil {
		return req
	}
	if req := w.local.pop(); req != nil {
		return req
	}
	if req := w.pool.global.pop(); req != nil {
		n := w.pool.global.popN(w.batchBuf[:16])
		for i := 0; i < n; i++ {
			if w.batchBuf[i] != nil {
				w.local.push(w.batchBuf[i])
				w.batchBuf[i] = nil
			}
		}
		return req
	}
	return w.steal()
}

// steal takes work from another worker.
func (w *wsWorker) steal() *wsRequest {
	workers := w.pool.workers
	n := len(workers)
	if n <= 1 {
		return nil
	}

	start := w.rng.Intn(n)
	for i := 0; i < n; i++ {
		victim := (start + i) % n
		if victim == w.id {
			continue
		}

		count := workers[victim].local.stealHalfInto(w.batchBuf[:])
		if count > 0 {
			w.stolen.Add(uint64(count))
			for j := 1; j < count; j++ {
				w.local.push(w.batchBuf[j])
				w.batchBuf[j] = nil
			}
			first := w.batchBuf[0]
			w.batchBuf[0] = nil
			return first
		}
	}
	return nil
}

// execute runs a call to completion.
func (w *wsWorker) execute(req *wsRequest) *runtime.Result {
	data := req.data.Load()
	if data == nil {
		return &runtime.Result{Error: fmt.Errorf("nil request data")}
	}
	return w.executor.Run(data.ctx, w.process, data.method, data.input)
}

// Simple lock-free queue for global work.
type wsQueue struct {
	items []*wsRequest
	mu    sync.Mutex
}

func newWSQueue(size int) *wsQueue {
	return &wsQueue{items: make([]*wsRequest, 0, size)}
}

func (q *wsQueue) push(req *wsRequest) {
	q.mu.Lock()
	q.items = append(q.items, req)
	q.mu.Unlock()
}

func (q *wsQueue) pop() *wsRequest {
	q.mu.Lock()
	if len(q.items) == 0 {
		q.mu.Unlock()
		return nil
	}
	req := q.items[0]
	q.items = q.items[1:]
	q.mu.Unlock()
	return req
}

func (q *wsQueue) popN(buf []*wsRequest) int {
	q.mu.Lock()
	n := len(q.items)
	if n > len(buf) {
		n = len(buf)
	}
	copy(buf[:n], q.items[:n])
	q.items = q.items[n:]
	q.mu.Unlock()
	return n
}

func (q *wsQueue) len() int {
	q.mu.Lock()
	n := len(q.items)
	q.mu.Unlock()
	return n
}

// Work-stealing deque (simplified version).
type wsDeque struct {
	items []*wsRequest
	mu    sync.Mutex
}

func newWSDeque(size int) *wsDeque {
	return &wsDeque{items: make([]*wsRequest, 0, size)}
}

func (d *wsDeque) push(req *wsRequest) {
	d.mu.Lock()
	d.items = append(d.items, req)
	d.mu.Unlock()
}

func (d *wsDeque) pop() *wsRequest {
	d.mu.Lock()
	if len(d.items) == 0 {
		d.mu.Unlock()
		return nil
	}
	n := len(d.items) - 1
	req := d.items[n]
	d.items = d.items[:n]
	d.mu.Unlock()
	return req
}

func (d *wsDeque) stealHalfInto(buf []*wsRequest) int {
	d.mu.Lock()
	n := len(d.items) / 2
	if n == 0 && len(d.items) > 0 {
		n = 1
	}
	if n > len(buf) {
		n = len(buf)
	}
	copy(buf[:n], d.items[:n])
	d.items = d.items[n:]
	d.mu.Unlock()
	return n
}
