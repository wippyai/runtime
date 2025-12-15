package actor

import (
	"context"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

type Option func(*Scheduler)

func WithWorkers(n int) Option {
	return func(s *Scheduler) {
		if n > 0 {
			s.numWorkers = n
		}
	}
}

func WithLifecycle(l process.Lifecycle) Option {
	return func(s *Scheduler) { s.lifecycle = l }
}

func WithQueueSize(size int) Option {
	return func(s *Scheduler) {
		if size > 0 {
			s.queueSize = size
		}
	}
}

func WithLocalQueueSize(size int) Option {
	return func(s *Scheduler) {
		if size > 0 {
			s.localQueueSize = size
		}
	}
}

func WithMaxProcesses(maxProcs int64) Option {
	return func(s *Scheduler) {
		if maxProcs > 0 {
			s.maxProcesses = maxProcs
		}
	}
}

type Scheduler struct {
	registry dispatcher.Registry
	workers  []*Worker
	global   *Queue

	nextID atomic.Uint64
	wg     sync.WaitGroup

	stopping atomic.Bool
	wakeMu   sync.Mutex
	wakeCond *sync.Cond

	numWorkers     int
	queueSize      int
	localQueueSize int
	maxProcesses   int64
	lifecycle      process.Lifecycle

	processorCount atomic.Int64
	drainCh        chan struct{} // closed when processorCount reaches 0 during shutdown
	byPID          sync.Map      // PID.String() -> *Processor
	byQueue        sync.Map      // *EventQueue -> *Processor for wake routing
}

func NewScheduler(registry dispatcher.Registry, opts ...Option) *Scheduler {
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
	s.drainCh = make(chan struct{}, 1)
	s.workers = make([]*Worker, s.numWorkers)

	for i := range s.workers {
		s.workers[i] = newWorker(i, s)
	}

	return s
}

func (s *Scheduler) getHandler(cmd dispatcher.Command) dispatcher.Handler {
	return s.registry.Get(cmd.CmdID())
}

func (s *Scheduler) Start() {
	for _, w := range s.workers {
		s.wg.Add(1)
		go func(worker *Worker) {
			defer s.wg.Done()
			worker.run()
		}(w)
	}
}

// Stop gracefully shuts down the scheduler.
// Sends cancel events and waits for processes to complete or context deadline.
func (s *Scheduler) Stop(ctx context.Context) {
	// Set stopping first - prevents new submissions and pool release
	s.stopping.Store(true)

	// Determine deadline for cancel events
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}

	// Push cancel event directly to each processor's queue.
	// Safe because stopping=true prevents pool release.
	// Wake idle/blocked processors so they process the cancel.
	s.byPID.Range(func(_, value any) bool {
		proc := value.(*Processor)
		pkg := topology.CancelPackage(pid.PID{}, proc.pid, deadline)
		proc.queue.PushDirect(process.Event{
			Type: process.EventMessage,
			Data: pkg,
		})
		// Wake if idle or blocked
		if proc.casState(StateIdle, StateReady) || proc.casState(StateBlocked, StateReady) {
			s.global.Push(proc)
		}
		return true
	})

	// Wake workers to process cancel events
	s.wakeAll()

	// If already empty, we're done
	if s.processorCount.Load() == 0 {
		select {
		case s.drainCh <- struct{}{}:
		default:
		}
	}

	// Wait for processes to complete or context timeout
	waitCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	select {
	case <-s.drainCh:
		// All processes completed gracefully
	case <-waitCtx.Done():
		// Timeout - cancel all process contexts to unblock stuck processes
		s.byPID.Range(func(_, value any) bool {
			proc := value.(*Processor)
			if proc.cancel != nil {
				proc.cancel()
			}
			proc.queue.Close()
			return true
		})
	}

	// Wake and wait for workers to exit
	s.wakeAll()
	s.wg.Wait()

	// Force complete any remaining processes (workers stopped)
	s.byPID.Range(func(_, value any) bool {
		proc := value.(*Processor)
		s.completeNoPool(proc, nil, context.Canceled)
		return true
	})
}

func (s *Scheduler) wake() {
	s.wakeMu.Lock()
	s.wakeCond.Signal()
	s.wakeMu.Unlock()
}

func (s *Scheduler) wakeAll() {
	s.wakeMu.Lock()
	s.wakeCond.Broadcast()
	s.wakeMu.Unlock()
}

// WakeProcessor implements process.YieldScheduler.
// Called by YieldCompleter after pushing an event to wake a blocked processor.
// Uses queue pointer to look up processor safely - avoids direct processor reference.
func (s *Scheduler) WakeProcessor(q *process.EventQueue, gen uint64) {
	v, ok := s.byQueue.Load(q)
	if !ok {
		return
	}
	proc := v.(*Processor)

	// Verify generation matches to avoid waking wrong processor
	if proc.gen.Load() != gen {
		return
	}

	// Same wake logic as processor.CompleteYield
	if proc.casState(StateBlocked, StateReady) {
		s.global.Push(proc)
		s.wake()
		return
	}
	proc.setWakeup(StateRunning)
}

func (s *Scheduler) Submit(ctx context.Context, pid pid.PID, p process.Process, method string, input payload.Payloads) (*Processor, error) {
	if s.stopping.Load() {
		return nil, process.ErrSchedulerStopping
	}
	if s.maxProcesses > 0 && s.processorCount.Load() >= s.maxProcesses {
		return nil, process.ErrMaxProcessesExceeded
	}

	// Create cancellable context first so Init receives the right context
	procCtx, cancel := context.WithCancel(ctx)

	if err := p.Init(procCtx, method, input); err != nil {
		cancel()
		return nil, err
	}

	proc := acquireProcessor()
	proc.id = s.nextID.Add(1)
	proc.pid = pid
	proc.Process = p
	proc.state.Store(int32(StateReady))
	proc.ctx = procCtx
	proc.cancel = cancel
	proc.scheduler = s

	// Reset queue for this execution and cache generation
	proc.queue.Reset()
	proc.gen.Store(proc.queue.Generation())

	s.processorCount.Add(1)
	s.byPID.Store(pid.String(), proc)
	s.byQueue.Store(proc.queue, proc)

	if s.lifecycle != nil {
		s.lifecycle.OnStart(procCtx, pid, p)
	}

	s.global.Push(proc)
	s.wake()

	return proc, nil
}

// Terminate forcibly terminates a process by PID.
// Cancels the process context - worker will detect and evict on next step.
func (s *Scheduler) Terminate(pid pid.PID) error {
	v, ok := s.byPID.Load(pid.String())
	if !ok {
		return process.ErrProcessNotFound
	}
	proc := v.(*Processor)

	// Cancel context - worker checks ctx.Err() and evicts
	if proc.cancel != nil {
		proc.cancel()
	}

	// Push termination event via PushDirect (bypasses generation check).
	// This ensures process wakes even if yields never complete.
	// Don't close queue yet - let the event be processed first.
	proc.queue.PushDirect(process.Event{Type: process.EventMessage})

	// Try to transition to Ready and re-queue so worker can evict.
	if proc.casState(StateIdle, StateReady) || proc.casState(StateBlocked, StateReady) {
		s.global.Push(proc)
		s.wake()
	}

	return nil
}

func (s *Scheduler) complete(proc *Processor, result *process.StepOutput, err error) {
	s.finishProcessor(proc, result, err, true)
}

func (s *Scheduler) completeNoPool(proc *Processor, result *process.StepOutput, err error) {
	s.finishProcessor(proc, result, err, false)
}

func (s *Scheduler) finishProcessor(proc *Processor, result *process.StepOutput, err error, allowPool bool) {
	res := &runtime.Result{Error: err}
	if result != nil && result.Result() != nil {
		res.Value = result.Result()
	}

	s.byPID.Delete(proc.pid.String())
	s.byQueue.Delete(proc.queue)

	stopping := s.stopping.Load()
	if !proc.pooled {
		if s.processorCount.Add(-1) == 0 && stopping {
			select {
			case s.drainCh <- struct{}{}:
			default:
			}
		}
	}

	if proc.resultCh != nil {
		proc.resultCh <- res
	}

	if s.lifecycle != nil {
		s.lifecycle.OnComplete(proc.ctx, proc.pid, res)
	}

	if proc.pooled {
		return
	}

	if proc.cancel != nil {
		proc.cancel()
	}
	if proc.Process != nil {
		proc.Process.Close()
	}

	if allowPool && !stopping {
		releaseProcessor(proc)
	}
}

func (s *Scheduler) CreateProcessor(ctx context.Context, pid pid.PID, p process.Process) (*Processor, error) {
	if s.stopping.Load() {
		return nil, process.ErrSchedulerStopping
	}
	if s.maxProcesses > 0 && s.processorCount.Load() >= s.maxProcesses {
		return nil, process.ErrMaxProcessesExceeded
	}

	// Wrap context with cancel for Terminate support
	procCtx, cancel := context.WithCancel(ctx)

	proc := acquireProcessor()
	proc.id = s.nextID.Add(1)
	proc.pid = pid
	proc.Process = p
	proc.state.Store(int32(StateReady))
	proc.ctx = procCtx
	proc.cancel = cancel
	proc.scheduler = s
	proc.pooled = true
	proc.resultCh = make(chan *runtime.Result, 1)

	// Reset queue for this execution and cache generation
	proc.queue.Reset()
	proc.gen.Store(proc.queue.Generation())

	s.processorCount.Add(1)
	s.byPID.Store(pid.String(), proc)
	s.byQueue.Store(proc.queue, proc)

	return proc, nil
}

func (s *Scheduler) ReleaseProcessor(proc *Processor) {
	s.processorCount.Add(-1)
	s.byPID.Delete(proc.pid.String())
	s.byQueue.Delete(proc.queue)
	if proc.cancel != nil {
		proc.cancel()
	}
	if proc.Process != nil {
		proc.Process.Close()
	}
}

// Send implements relay.Receiver. Routes package to target process.
// Wakes the process if it's idle or blocked waiting for messages.
func (s *Scheduler) Send(pkg *relay.Package) error {
	target := pkg.Target // copy before push - pkg may be released after queue receives it

	v, ok := s.byPID.Load(target.String())
	if !ok {
		return process.ErrProcessNotFound
	}
	proc := v.(*Processor)

	// Push message event to processor's queue with generation check
	if !proc.queue.Push(process.Event{
		Type: process.EventMessage,
		Data: pkg,
	}, proc.gen.Load()) {
		// Push failed - queue closed, process is terminating
		return process.ErrProcessClosed
	}

	// Wake process if waiting for messages.
	// CAS ensures exactly-once wake even with concurrent senders.
	// Try both Idle (waiting on select) and Blocked (waiting on yield completion).
	if proc.casState(StateIdle, StateReady) {
		s.global.Push(proc)
		s.wake()
	} else if proc.casState(StateBlocked, StateReady) {
		s.global.Push(proc)
		s.wake()
	}

	return nil
}

func (s *Scheduler) Stats() map[string]uint64 {
	stats := make(map[string]uint64, 8)

	var executed, stolen uint64
	for _, w := range s.workers {
		executed += w.executed.Load()
		stolen += w.stolen.Load()
	}

	var processCount uint64
	s.byPID.Range(func(_, _ any) bool { processCount++; return true })

	stats["executed"] = executed
	stats["stolen"] = stolen
	stats["global_queue"] = uint64(max(0, s.global.Len())) //nolint:gosec // queue length is always non-negative and bounded
	stats["workers"] = uint64(len(s.workers))
	stats["processes"] = processCount

	return stats
}

func (s *Scheduler) WorkerStats() []map[string]uint64 {
	result := make([]map[string]uint64, len(s.workers))
	for i, w := range s.workers {
		result[i] = map[string]uint64{
			"executed":    w.executed.Load(),
			"stolen":      w.stolen.Load(),
			"local_queue": uint64(max(0, w.local.Len())), //nolint:gosec // local queue length bounded
		}
	}
	return result
}

// CollectProcessStats gathers statistics from all active processes.
// Each process can implement process.StatsProvider to expose custom stats.
func (s *Scheduler) CollectProcessStats() []attrs.Attributes {
	var stats []attrs.Attributes

	s.byPID.Range(func(_, value any) bool {
		proc := value.(*Processor)
		if sp, ok := proc.Process.(process.StatsProvider); ok {
			if st := sp.Stats(); st != nil {
				stats = append(stats, st)
			}
		}
		return true
	})

	return stats
}
