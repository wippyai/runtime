package actor

import (
	"context"
	goruntime "runtime"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// OnStart is called after a process starts.
type OnStart func(ctx context.Context, pid relay.PID, proc Process)

// OnComplete is called when a process completes.
type OnComplete func(ctx context.Context, pid relay.PID, result *runtime.Result)

// Pool for result channels (blocking Execute)
var resultChPool = sync.Pool{
	New: func() any { return make(chan *runtime.Result, 1) },
}

// Option configures the scheduler.
type Option func(*Scheduler)

// WithWorkers sets the number of worker goroutines.
func WithWorkers(n int) Option {
	return func(s *Scheduler) {
		if n > 0 {
			s.numWorkers = n
		}
	}
}

// WithOnStart sets the start callback.
func WithOnStart(fn OnStart) Option {
	return func(s *Scheduler) {
		s.onStart = fn
	}
}

// WithOnComplete sets the completion callback.
func WithOnComplete(fn OnComplete) Option {
	return func(s *Scheduler) {
		s.onComplete = fn
	}
}

// WithQueueSize sets the initial global queue capacity.
func WithQueueSize(size int) Option {
	return func(s *Scheduler) {
		if size > 0 {
			s.queueSize = size
		}
	}
}

// WithLocalQueueSize sets the worker local deque capacity.
func WithLocalQueueSize(size int) Option {
	return func(s *Scheduler) {
		if size > 0 {
			s.localQueueSize = size
		}
	}
}

// Dispatcher routes commands to handlers. Default uses Registry.
type Dispatcher interface {
	Dispatch(cmd dispatcher.Command) dispatcher.Handler
}

// WithDispatcher sets a custom command dispatcher.
func WithDispatcher(d Dispatcher) Option {
	return func(s *Scheduler) {
		s.dispatcher = d
	}
}

// Scheduler coordinates workers and manages processor lifecycle.
//
// Architecture:
//   - Global queue: receives new submissions and async completions
//   - Worker local deques: per-worker queues for cache locality
//   - Work stealing: idle workers steal from busy workers
//
// Work distribution:
//  1. Submit() pushes to global queue
//  2. Workers pop from local deque first (fast path)
//  3. If empty, pop from global queue
//  4. If still empty, steal from random victim
type Scheduler struct {
	registry *Registry
	workers  []*Worker
	global   *Queue

	nextID atomic.Uint64
	wg     sync.WaitGroup

	// Worker stop signaling
	stopping atomic.Bool

	// Event-based worker waking (zero idle CPU)
	// Uses Go runtime's spinning pattern to avoid thundering herd
	wakeMu    sync.Mutex
	wakeCond  *sync.Cond
	nspinning atomic.Int32 // Number of spinning workers (looking for work)
	nparked   atomic.Int32 // Number of parked workers (in Cond.Wait)

	// Configuration (set via options before Start)
	numWorkers     int
	queueSize      int
	localQueueSize int
	dispatcher     Dispatcher

	// Lifecycle callbacks
	onStart    OnStart
	onComplete OnComplete

	// Process lookup (for Send() routing)
	byPID sync.Map // map[relay.PID]uint64 (PID -> internal ID)
	byID  sync.Map // map[uint64]Process (internal ID -> Process for Send)

	// Idle process storage (waiting for Send)
	idle sync.Map // map[uint64]*Processor
}

// NewScheduler creates a scheduler with the given registry and options.
func NewScheduler(registry *Registry, opts ...Option) *Scheduler {
	s := &Scheduler{
		registry:       registry,
		numWorkers:     goruntime.GOMAXPROCS(0),
		queueSize:      1024,
		localQueueSize: 256,
	}

	for _, opt := range opts {
		opt(s)
	}

	s.global = NewQueue(s.queueSize)
	s.wakeCond = sync.NewCond(&s.wakeMu)
	s.workers = make([]*Worker, s.numWorkers)
	for i := 0; i < s.numWorkers; i++ {
		s.workers[i] = newWorker(i, s)
	}

	return s
}

// getHandler returns the handler for a command, using dispatcher if set.
func (s *Scheduler) getHandler(cmd dispatcher.Command) dispatcher.Handler {
	if s.dispatcher != nil {
		return s.dispatcher.Dispatch(cmd)
	}
	return s.registry.Get(cmd.CmdID())
}

// Start launches all worker goroutines.
func (s *Scheduler) Start() {
	for _, w := range s.workers {
		s.wg.Add(1)
		go func(worker *Worker) {
			defer s.wg.Done()
			worker.run()
		}(w)
	}
}

// Stop signals workers to stop and waits for them to finish.
func (s *Scheduler) Stop() {
	s.stopping.Store(true)
	s.wakeAllWorkers()
	s.wg.Wait()
}

// wakeWorker signals an idle worker to check for work.
// Only wakes if there are no spinning workers (Go runtime pattern).
func (s *Scheduler) wakeWorker() {
	// If there are spinning workers, they will find the work
	if s.nspinning.Load() > 0 {
		return
	}
	// Must hold mutex while signaling to prevent race with Wait()
	// Worker: Lock -> check work -> Wait (atomic unlock+wait)
	// Producer: Lock -> Signal -> Unlock
	// This ensures signal is never lost between check and wait
	s.wakeMu.Lock()
	s.wakeCond.Signal()
	s.wakeMu.Unlock()
}

// wakeAllWorkers wakes all idle workers (used for shutdown).
func (s *Scheduler) wakeAllWorkers() {
	s.wakeMu.Lock()
	s.wakeCond.Broadcast()
	s.wakeMu.Unlock()
}

// Submit adds a new process to the scheduler (fire-and-forget).
// The process is initialized with Execute() and queued for execution.
func (s *Scheduler) Submit(ctx context.Context, pid relay.PID, p Process, method string, input payload.Payloads) (*Processor, error) {
	if err := p.Execute(ctx, method, input); err != nil {
		return nil, err
	}

	proc := acquireProcessor()
	proc.ID = s.nextID.Add(1)
	proc.PID = pid
	proc.Process = p
	proc.State = StateReady
	proc.ctx = ctx
	proc.scheduler = s
	proc.WakeNano = nanotime()

	s.byID.Store(proc.ID, p)
	s.byPID.Store(pid, proc.ID)

	// Push to global queue and wake a worker
	s.global.Push(proc)
	s.wakeWorker()

	if s.onStart != nil {
		s.onStart(ctx, pid, p)
	}

	return proc, nil
}

// Execute runs a process and blocks until completion.
// Returns the final result. Uses pooled channels.
func (s *Scheduler) Execute(ctx context.Context, pid relay.PID, p Process, method string, input payload.Payloads) (*runtime.Result, error) {
	if err := p.Execute(ctx, method, input); err != nil {
		return nil, err
	}

	proc := acquireProcessor()
	proc.ID = s.nextID.Add(1)
	proc.PID = pid
	proc.Process = p
	proc.State = StateReady
	proc.ctx = ctx
	proc.scheduler = s
	proc.WakeNano = nanotime()

	resultCh := resultChPool.Get().(chan *runtime.Result)
	proc.resultCh = resultCh

	s.byID.Store(proc.ID, p)
	s.byPID.Store(pid, proc.ID)

	// Push to global queue and wake a worker
	s.global.Push(proc)
	s.wakeWorker()

	if s.onStart != nil {
		s.onStart(ctx, pid, p)
	}

	result := <-resultCh
	resultChPool.Put(resultCh)

	return result, nil
}

// SendTo delivers a message to an idle process.
// Returns error if the process is not in idle state.
func (s *Scheduler) SendTo(procID uint64, pkg *relay.Package) error {
	val, ok := s.idle.LoadAndDelete(procID)
	if !ok {
		return &ProcessNotIdleError{ID: procID}
	}

	proc := val.(*Processor)
	if err := proc.Process.Send(pkg); err != nil {
		return err
	}

	proc.State = StateReady
	proc.WakeNano = nanotime()
	s.global.Push(proc)
	s.wakeWorker()
	return nil
}

// Cancel removes an idle process from the scheduler.
func (s *Scheduler) Cancel(procID uint64) {
	s.idle.Delete(procID)
}

// completeProcessor handles process completion.
func (s *Scheduler) completeProcessor(proc *Processor, result *StepResult, err error) {
	res := &runtime.Result{Error: err}

	// Use explicit Result field from StepResult
	if result != nil && result.Result != nil {
		res.Value = result.Result
	}

	// Notify blocking Execute()/Requeue() caller
	if proc.resultCh != nil {
		proc.resultCh <- res
	}

	// Invoke completion callback
	if s.onComplete != nil {
		s.onComplete(proc.ctx, proc.PID, res)
	}

	// Pooled processors are managed externally - don't release or unregister
	if proc.pooled {
		return
	}

	// Close the process to release resources (e.g., Lua state)
	proc.Process.Close()

	// Unregister from lookups and release processor
	s.byID.Delete(proc.ID)
	s.byPID.Delete(proc.PID)
	s.idle.Delete(proc.ID)
	releaseProcessor(proc)
}

// Requeue executes an existing processor and blocks until completion.
// Unlike Execute, this reuses an existing Processor without registration.
// Used by funcpool for pooled process reuse where Processor is persistent.
//
// The caller must:
// 1. Call SetupCall() on the process before Requeue
// 2. Ensure the process state is ready for execution
func (s *Scheduler) Requeue(ctx context.Context, proc *Processor) (*runtime.Result, error) {
	proc.ctx = ctx
	proc.State = StateReady
	proc.WakeNano = nanotime()

	// Reuse pooled processor's result channel if available
	resultCh := proc.resultCh
	if resultCh == nil {
		resultCh = resultChPool.Get().(chan *runtime.Result)
		proc.resultCh = resultCh
	}

	// Push to global queue and wake a worker
	s.global.Push(proc)
	s.wakeWorker()

	result := <-resultCh
	// Don't return channel to pool - keep for reuse

	return result, nil
}

// CreateProcessor creates a persistent Processor for a process.
// Used by funcpool to pre-create Processors at warmup time.
// The returned Processor can be reused via Requeue().
func (s *Scheduler) CreateProcessor(pid relay.PID, p Process) *Processor {
	proc := acquireProcessor()
	proc.ID = s.nextID.Add(1)
	proc.PID = pid
	proc.Process = p
	proc.State = StateReady
	proc.scheduler = s
	proc.pooled = true
	proc.resultCh = make(chan *runtime.Result, 1)

	// Register for Send() routing
	s.byID.Store(proc.ID, p)
	s.byPID.Store(pid, proc.ID)

	return proc
}

// ReleaseProcessor removes a persistent Processor from the scheduler.
// Called when a pooled process is destroyed.
// Note: Pooled processors are NOT returned to the processor pool to avoid
// races with completeProcessor which may still be reading proc.pooled.
// The Processor struct will be GC'd when no longer referenced.
func (s *Scheduler) ReleaseProcessor(proc *Processor) {
	s.byID.Delete(proc.ID)
	s.byPID.Delete(proc.PID)
}

// Send routes a package to a process by PID.
// The package is delivered to the process via Process.Send().
// Returns error if process not found or not in a state to receive.
func (s *Scheduler) Send(pid relay.PID, pkg *relay.Package) error {
	// Lookup PID -> internal ID
	idVal, ok := s.byPID.Load(pid)
	if !ok {
		return &ProcessNotFoundError{PID: pid}
	}
	id := idVal.(uint64)

	// Lookup ID -> Process
	procVal, ok := s.byID.Load(id)
	if !ok {
		return &ProcessNotFoundError{PID: pid}
	}

	return procVal.(Process).Send(pkg)
}

// parkIdle stores a process waiting for Send().
func (s *Scheduler) parkIdle(proc *Processor) {
	s.idle.Store(proc.ID, proc)
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
	stats["workers"] = uint64(len(s.workers))

	return stats
}

// WorkerStats returns per-worker metrics.
func (s *Scheduler) WorkerStats() []map[string]uint64 {
	result := make([]map[string]uint64, len(s.workers))
	for i, w := range s.workers {
		result[i] = map[string]uint64{
			"executed":    w.executed.Load(),
			"stolen":      w.stolen.Load(),
			"local_queue": uint64(w.local.Len()),
		}
	}
	return result
}
