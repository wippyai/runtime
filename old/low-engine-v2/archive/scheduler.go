package engine

import (
	"context"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Pools for reducing allocations in hot path
var (
	processorPool = sync.Pool{
		New: func() any { return &Processor{} },
	}
	yieldResultPool = sync.Pool{
		New: func() any { return &YieldResults{} },
	}
)

// ProcessState tracks a process through the scheduler.
type ProcessState int

const (
	StateReady    ProcessState = iota // in run queue, waiting to execute
	StateRunning                      // currently executing on a worker
	StateBlocked                      // waiting for yield result
	StateIdle                         // waiting for Send (external event)
	StateComplete                     // finished
)

// Processor wraps a Process with scheduler metadata.
// This is the unit that flows through queues - Process is DATA, Processor is scheduler state.
type Processor struct {
	ID       uint64
	Process  Process
	State    ProcessState
	Priority int

	// Execution context
	ctx    context.Context
	cancel context.CancelFunc

	// Embedded yield results to avoid allocation
	yieldResult    YieldResults
	hasYieldResult bool

	// Back-reference for zero-alloc completion callback
	scheduler *Scheduler

	// Metrics (no time.Time to avoid allocs)
	StepCount uint64
}

// Complete is called by handlers when async work finishes.
// Zero allocation - no closure needed.
func (p *Processor) Complete(data any, err error) {
	p.yieldResult.Data = data
	p.yieldResult.Error = err
	p.hasYieldResult = true
	p.State = StateReady
	p.scheduler.global.Push(p)
}

// acquireProcessor gets a Processor from pool
func acquireProcessor() *Processor {
	return processorPool.Get().(*Processor)
}

// releaseProcessor returns a Processor to pool after clearing
func releaseProcessor(p *Processor) {
	p.ID = 0
	p.Process = nil
	p.State = 0
	p.Priority = 0
	p.ctx = nil
	p.cancel = nil
	p.yieldResult.Data = nil
	p.yieldResult.Error = nil
	p.hasYieldResult = false
	p.scheduler = nil
	p.StepCount = 0
	processorPool.Put(p)
}

// Handler executes a command.
// Handlers are provided by subsystems (time, http, db, etc).
type Handler interface {
	// Handle executes the command.
	// For sync ops: call proc.Complete(data, err) before returning.
	// For async ops: spawn goroutine, call proc.Complete(data, err) when done.
	Handle(cmd Command, proc *Processor)
}

// HandlerFunc adapts a function to Handler interface.
type HandlerFunc func(cmd Command, proc *Processor)

func (f HandlerFunc) Handle(cmd Command, proc *Processor) {
	f(cmd, proc)
}

// Registry maps CommandID -> Handler with O(1) lookup.
type Registry struct {
	handlers [256]Handler
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(id CommandID, h Handler) {
	if r.handlers[id] != nil {
		panic("handler already registered")
	}
	r.handlers[id] = h
}

func (r *Registry) Get(id CommandID) Handler {
	return r.handlers[id]
}

// Deque is a Chase-Lev work-stealing deque.
// Owner: push/pop from bottom (LIFO, cache-friendly)
// Thieves: steal from top (FIFO, gets older/bigger tasks)
type Deque struct {
	buffer atomic.Pointer[dequeBuffer]
	top    atomic.Int64
	bottom atomic.Int64
}

type dequeBuffer struct {
	items []*Processor
}

func NewDeque(capacity int) *Deque {
	d := &Deque{}
	buf := &dequeBuffer{items: make([]*Processor, capacity)}
	d.buffer.Store(buf)
	return d
}

// Push adds to bottom (owner only, no CAS needed).
func (d *Deque) Push(p *Processor) {
	bottom := d.bottom.Load()
	top := d.top.Load()
	buf := d.buffer.Load()

	size := bottom - top
	if size >= int64(len(buf.items)-1) {
		buf = d.grow(buf, top, bottom)
	}

	buf.items[bottom%int64(len(buf.items))] = p
	d.bottom.Store(bottom + 1)
}

// Pop removes from bottom (owner only).
func (d *Deque) Pop() *Processor {
	bottom := d.bottom.Load() - 1
	d.bottom.Store(bottom)

	top := d.top.Load()

	if bottom > top {
		buf := d.buffer.Load()
		return buf.items[bottom%int64(len(buf.items))]
	}

	if bottom == top {
		buf := d.buffer.Load()
		p := buf.items[bottom%int64(len(buf.items))]
		if !d.top.CompareAndSwap(top, top+1) {
			d.bottom.Store(top + 1)
			return nil
		}
		d.bottom.Store(top + 1)
		return p
	}

	d.bottom.Store(top)
	return nil
}

// Steal takes from top (thieves, uses CAS).
func (d *Deque) Steal() *Processor {
	top := d.top.Load()
	bottom := d.bottom.Load()

	if top >= bottom {
		return nil
	}

	buf := d.buffer.Load()
	p := buf.items[top%int64(len(buf.items))]

	if !d.top.CompareAndSwap(top, top+1) {
		return nil
	}

	return p
}

// StealHalf takes half the queue into provided buffer.
// Returns number of items stolen.
func (d *Deque) StealHalfInto(dst []*Processor) int {
	top := d.top.Load()
	bottom := d.bottom.Load()

	size := bottom - top
	if size <= 0 {
		return 0
	}

	n := size / 2
	if n < 1 {
		n = 1
	}
	if n > int64(len(dst)) {
		n = int64(len(dst))
	}

	if !d.top.CompareAndSwap(top, top+n) {
		return 0
	}

	buf := d.buffer.Load()
	for i := int64(0); i < n; i++ {
		dst[i] = buf.items[(top+i)%int64(len(buf.items))]
	}

	return int(n)
}

func (d *Deque) Len() int {
	return int(d.bottom.Load() - d.top.Load())
}

func (d *Deque) grow(old *dequeBuffer, top, bottom int64) *dequeBuffer {
	newBuf := &dequeBuffer{items: make([]*Processor, len(old.items)*2)}
	for i := top; i < bottom; i++ {
		newBuf.items[i%int64(len(newBuf.items))] = old.items[i%int64(len(old.items))]
	}
	d.buffer.Store(newBuf)
	return newBuf
}

// ConcurrentQueue is a thread-safe ring buffer for the global work queue.
// Zero allocation after init - uses fixed-size ring buffer.
type ConcurrentQueue struct {
	items []*Processor
	head  int
	tail  int
	count int
	mu    sync.Mutex
}

func NewConcurrentQueue(capacity int) *ConcurrentQueue {
	return &ConcurrentQueue{
		items: make([]*Processor, capacity),
	}
}

func (q *ConcurrentQueue) Push(p *Processor) {
	q.mu.Lock()
	if q.count == len(q.items) {
		// Grow - rare, only at startup
		q.grow()
	}
	q.items[q.tail] = p
	q.tail = (q.tail + 1) % len(q.items)
	q.count++
	q.mu.Unlock()
}

func (q *ConcurrentQueue) Pop() *Processor {
	q.mu.Lock()
	if q.count == 0 {
		q.mu.Unlock()
		return nil
	}
	p := q.items[q.head]
	q.items[q.head] = nil // clear reference
	q.head = (q.head + 1) % len(q.items)
	q.count--
	q.mu.Unlock()
	return p
}

func (q *ConcurrentQueue) Len() int {
	q.mu.Lock()
	n := q.count
	q.mu.Unlock()
	return n
}

func (q *ConcurrentQueue) grow() {
	newItems := make([]*Processor, len(q.items)*2)
	for i := 0; i < q.count; i++ {
		newItems[i] = q.items[(q.head+i)%len(q.items)]
	}
	q.items = newItems
	q.head = 0
	q.tail = q.count
}

// Worker is a goroutine that processes Processors from queues.
type Worker struct {
	id        int
	local     *Deque
	scheduler *Scheduler
	rng       *rand.Rand

	// Pre-allocated buffer for stealing to avoid allocation
	stealBuf [64]*Processor

	executed atomic.Uint64
	stolen   atomic.Uint64
}

func newWorker(id int, s *Scheduler) *Worker {
	return &Worker{
		id:        id,
		local:     NewDeque(256),
		scheduler: s,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano() + int64(id))),
	}
}

func (w *Worker) run(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
		}

		proc := w.findWork()
		if proc == nil {
			runtime.Gosched()
			continue
		}

		w.executeOne(proc)
		w.executed.Add(1)
	}
}

func (w *Worker) findWork() *Processor {
	// 1. Local queue (fast, cache-friendly)
	if p := w.local.Pop(); p != nil {
		return p
	}

	// 2. Global queue (thread-safe)
	if p := w.scheduler.global.Pop(); p != nil {
		return p
	}

	// 3. Steal from random victim
	return w.steal()
}

func (w *Worker) steal() *Processor {
	workers := w.scheduler.workers
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

		count := workers[victim].local.StealHalfInto(w.stealBuf[:])
		if count > 0 {
			w.stolen.Add(uint64(count))
			for j := 1; j < count; j++ {
				w.local.Push(w.stealBuf[j])
				w.stealBuf[j] = nil // clear reference
			}
			first := w.stealBuf[0]
			w.stealBuf[0] = nil
			return first
		}
	}

	return nil
}

func (w *Worker) executeOne(proc *Processor) {
	proc.State = StateRunning
	// Removed time.Now() - too expensive for hot path
	// Use proc.StepCount for activity tracking instead

	// Get previous yield results (if any)
	var yieldResults *YieldResults
	if proc.hasYieldResult {
		yieldResults = &proc.yieldResult
		proc.hasYieldResult = false
	}

	// Step the process
	result, err := proc.Process.Step(yieldResults)
	proc.StepCount++

	// Clear yield result after use
	if yieldResults != nil {
		proc.yieldResult.Data = nil
		proc.yieldResult.Error = nil
	}

	if err != nil {
		proc.State = StateComplete
		w.scheduler.completeProcessor(proc, nil, err)
		return
	}

	yields := result.GetYields()

	switch result.Status {
	case StepDone:
		proc.State = StateComplete
		w.scheduler.completeProcessor(proc, yields, nil)

	case StepIdle:
		proc.State = StateIdle
		w.scheduler.parkIdle(proc)

	case StepContinue:
		if len(yields) == 0 {
			// No yields but continue - just re-queue
			proc.State = StateReady
			w.local.Push(proc)
			return
		}

		// Execute first yield (for POC, synchronous dispatch)
		cmd := yields[0]
		handler := w.scheduler.registry.Get(cmd.CmdID())
		if handler == nil {
			proc.State = StateComplete
			w.scheduler.completeProcessor(proc, nil, &UnknownCommandError{ID: cmd.CmdID()})
			return
		}

		proc.State = StateBlocked
		handler.Handle(cmd, proc) // handler calls proc.Complete() when done
	}
}

// UnknownCommandError is returned when no handler is registered for a command.
type UnknownCommandError struct {
	ID CommandID
}

func (e *UnknownCommandError) Error() string {
	return "unknown command"
}

// Scheduler coordinates workers and manages processor lifecycle.
type Scheduler struct {
	registry *Registry
	workers  []*Worker
	global   *ConcurrentQueue

	nextID atomic.Uint64
	done   chan struct{}
	wg     sync.WaitGroup

	// Callbacks
	onComplete func(proc *Processor, result any, err error)

	// Idle processors waiting for Send
	idle sync.Map
}

func NewScheduler(registry *Registry, numWorkers int) *Scheduler {
	if numWorkers <= 0 {
		numWorkers = runtime.GOMAXPROCS(0)
	}

	s := &Scheduler{
		registry: registry,
		workers:  make([]*Worker, numWorkers),
		global:   NewConcurrentQueue(1024),
		done:     make(chan struct{}),
	}

	for i := 0; i < numWorkers; i++ {
		s.workers[i] = newWorker(i, s)
	}

	return s
}

func (s *Scheduler) OnComplete(fn func(proc *Processor, result any, err error)) {
	s.onComplete = fn
}

func (s *Scheduler) Start() {
	for _, w := range s.workers {
		s.wg.Add(1)
		go func(worker *Worker) {
			defer s.wg.Done()
			worker.run(s.done)
		}(w)
	}
}

func (s *Scheduler) Stop() {
	close(s.done)
	s.wg.Wait()
}

// Submit adds a new process to the scheduler.
func (s *Scheduler) Submit(ctx context.Context, p Process, input any) (*Processor, error) {
	procCtx, cancel := context.WithCancel(ctx)

	if err := p.Start(procCtx, input); err != nil {
		cancel()
		return nil, err
	}

	proc := acquireProcessor()
	proc.ID = s.nextID.Add(1)
	proc.Process = p
	proc.State = StateReady
	proc.ctx = procCtx
	proc.cancel = cancel
	proc.scheduler = s

	s.global.Push(proc)
	return proc, nil
}

// SubmitFast adds a process without context cancellation (for max perf).
// Caller responsible for cleanup.
func (s *Scheduler) SubmitFast(p Process, input any) (*Processor, error) {
	if err := p.Start(context.Background(), input); err != nil {
		return nil, err
	}

	proc := acquireProcessor()
	proc.ID = s.nextID.Add(1)
	proc.Process = p
	proc.State = StateReady
	proc.scheduler = s

	s.global.Push(proc)
	return proc, nil
}

// SendTo delivers a message to an idle process.
func (s *Scheduler) SendTo(procID uint64, pkg *Package) error {
	val, ok := s.idle.LoadAndDelete(procID)
	if !ok {
		return &ProcessNotIdleError{ID: procID}
	}

	proc := val.(*Processor)
	if err := proc.Process.Send(pkg); err != nil {
		return err
	}

	proc.State = StateReady
	s.global.Push(proc)
	return nil
}

// Cancel terminates a process.
func (s *Scheduler) Cancel(procID uint64) {
	// Cancel context - will propagate to any blocked operations
	s.idle.Range(func(key, val any) bool {
		if key.(uint64) == procID {
			proc := val.(*Processor)
			proc.cancel()
			s.idle.Delete(key)
		}
		return true
	})
}

func (s *Scheduler) completeProcessor(proc *Processor, yields []Command, err error) {
	proc.cancel()
	if s.onComplete != nil {
		var result any
		if len(yields) > 0 {
			result = yields[0] // typically Complete{Result: ...}
		}
		s.onComplete(proc, result, err)
	}
	releaseProcessor(proc)
}

func (s *Scheduler) parkIdle(proc *Processor) {
	s.idle.Store(proc.ID, proc)
}

// ProcessNotIdleError is returned when SendTo is called for a non-idle process.
type ProcessNotIdleError struct {
	ID uint64
}

func (e *ProcessNotIdleError) Error() string {
	return "process not idle"
}

// Stats returns scheduler metrics.
func (s *Scheduler) Stats() map[string]uint64 {
	stats := make(map[string]uint64)

	var executed, stolen uint64
	for _, w := range s.workers {
		executed += w.executed.Load()
		stolen += w.stolen.Load()
	}

	stats["executed"] = executed
	stats["stolen"] = stolen
	stats["global_queue"] = uint64(s.global.Len())

	return stats
}
