package actor

import (
	"context"
	goruntime "runtime"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

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

// WithLifecycle sets the lifecycle handler.
func WithLifecycle(l process.Lifecycle) Option {
	return func(s *Scheduler) {
		s.lifecycle = l
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

// WithLocalQueueSize is deprecated - kept for compatibility but ignored.
func WithLocalQueueSize(_ int) Option {
	return func(_ *Scheduler) {}
}

// Dispatcher routes commands to handlers.
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
// Simplified design for async dispatchers - global queue only, no work-stealing.
type Scheduler struct {
	registry dispatcher.Registry
	workers  []*Worker
	global   *Queue

	nextID atomic.Uint64
	wg     sync.WaitGroup

	// Worker lifecycle
	stopping atomic.Bool
	wakeMu   sync.Mutex
	wakeCond *sync.Cond

	// Configuration
	numWorkers int
	queueSize  int
	dispatcher Dispatcher
	lifecycle  process.Lifecycle

	// Process lookup (for Send() routing)
	byPID sync.Map // map[relay.PID]Process

	// Idle process storage (waiting for Send)
	idle sync.Map // map[uint64]*Processor
}

// NewScheduler creates a scheduler with the given registry and options.
func NewScheduler(registry dispatcher.Registry, opts ...Option) *Scheduler {
	s := &Scheduler{
		registry:   registry,
		numWorkers: goruntime.GOMAXPROCS(0),
		queueSize:  1024,
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

// getHandler returns the handler for a command.
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
	s.wakeMu.Lock()
	s.wakeCond.Broadcast()
	s.wakeMu.Unlock()
	s.wg.Wait()
}

// wakeWorker signals a parked worker to check for work.
func (s *Scheduler) wakeWorker() {
	s.wakeMu.Lock()
	s.wakeCond.Signal()
	s.wakeMu.Unlock()
}

// Submit adds a new process to the scheduler (fire-and-forget).
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

	s.byPID.Store(pid, p)

	s.global.Push(proc)
	s.wakeWorker()

	if s.lifecycle != nil {
		s.lifecycle.OnStart(ctx, pid, p)
	}

	return proc, nil
}

// Execute runs a process and blocks until completion.
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

	s.byPID.Store(pid, p)

	s.global.Push(proc)
	s.wakeWorker()

	if s.lifecycle != nil {
		s.lifecycle.OnStart(ctx, pid, p)
	}

	result := <-resultCh
	resultChPool.Put(resultCh)

	return result, nil
}

// SendTo delivers a message to an idle process.
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

	if result != nil && result.Result != nil {
		res.Value = result.Result
	}

	if proc.resultCh != nil {
		proc.resultCh <- res
	}

	if s.lifecycle != nil {
		s.lifecycle.OnComplete(proc.ctx, proc.PID, res)
	}

	if proc.pooled {
		return
	}

	proc.Process.Close()

	s.byPID.Delete(proc.PID)
	s.idle.Delete(proc.ID)
	releaseProcessor(proc)
}

// Requeue executes an existing processor and blocks until completion.
func (s *Scheduler) Requeue(ctx context.Context, proc *Processor) (*runtime.Result, error) {
	proc.ctx = ctx
	proc.State = StateReady
	proc.WakeNano = nanotime()

	resultCh := proc.resultCh
	if resultCh == nil {
		resultCh = resultChPool.Get().(chan *runtime.Result)
		proc.resultCh = resultCh
	}

	s.global.Push(proc)
	s.wakeWorker()

	result := <-resultCh
	return result, nil
}

// CreateProcessor creates a persistent Processor for a process.
func (s *Scheduler) CreateProcessor(pid relay.PID, p Process) *Processor {
	proc := acquireProcessor()
	proc.ID = s.nextID.Add(1)
	proc.PID = pid
	proc.Process = p
	proc.State = StateReady
	proc.scheduler = s
	proc.pooled = true
	proc.resultCh = make(chan *runtime.Result, 1)

	s.byPID.Store(pid, p)

	return proc
}

// ReleaseProcessor removes a persistent Processor from the scheduler.
func (s *Scheduler) ReleaseProcessor(proc *Processor) {
	s.byPID.Delete(proc.PID)
}

// Send routes a package to a process by PID.
func (s *Scheduler) Send(pid relay.PID, pkg *relay.Package) error {
	procVal, ok := s.byPID.Load(pid)
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

	var executed uint64
	for _, w := range s.workers {
		executed += w.executed.Load()
	}

	var byPIDCount, idleCount uint64
	s.byPID.Range(func(_, _ any) bool { byPIDCount++; return true })
	s.idle.Range(func(_, _ any) bool { idleCount++; return true })

	stats["executed"] = executed
	stats["global_queue"] = uint64(s.global.Len()) //nolint:gosec // queue length is bounded
	stats["workers"] = uint64(len(s.workers))
	stats["by_pid"] = byPIDCount
	stats["idle"] = idleCount

	return stats
}

// WorkerStats returns per-worker metrics.
func (s *Scheduler) WorkerStats() []map[string]uint64 {
	result := make([]map[string]uint64, len(s.workers))
	for i, w := range s.workers {
		result[i] = map[string]uint64{
			"executed": w.executed.Load(),
		}
	}
	return result
}
