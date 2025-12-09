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
	byPID          sync.Map
	idleProcs      sync.Map
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
	s.workers = make([]*Worker, s.numWorkers)

	for i := range s.workers {
		s.workers[i] = newWorker(i, s)
	}

	return s
}

func (s *Scheduler) ProcessorCount() int64 { return s.processorCount.Load() }

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

func (s *Scheduler) Stop() {
	s.stopping.Store(true)
	s.wakeMu.Lock()
	s.wakeCond.Broadcast()
	s.wakeMu.Unlock()
	s.wg.Wait()
}

func (s *Scheduler) wake() {
	s.wakeMu.Lock()
	s.wakeCond.Signal()
	s.wakeMu.Unlock()
}

func (s *Scheduler) Submit(ctx context.Context, pid relay.PID, p Process, method string, input payload.Payloads) (*Processor, error) {
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
	proc.setState(StateReady)
	proc.ctx = procCtx
	proc.cancel = cancel
	proc.scheduler = s

	// Reset queue for this execution and cache generation
	proc.queue.Reset()
	proc.gen.Store(proc.queue.Generation())

	s.processorCount.Add(1)
	s.byPID.Store(pid, proc)

	if s.lifecycle != nil {
		s.lifecycle.OnStart(procCtx, pid, p)
	}

	s.global.Push(proc)
	s.wake()

	return proc, nil
}

// Terminate forcibly terminates a process by PID.
// Cancels the process context - worker will detect and evict on next step.
func (s *Scheduler) Terminate(pid relay.PID) error {
	v, ok := s.byPID.Load(pid)
	if !ok {
		return process.ErrProcessNotFound
	}
	proc := v.(*Processor)

	// Cancel context - worker checks ctx.Err() and evicts
	if proc.cancel != nil {
		proc.cancel()
	}

	// Close queue to reject new events
	proc.queue.Close()

	// Remove from idle tracking
	s.idleProcs.Delete(pid)

	// Try to transition to Ready and re-queue so worker can evict.
	// Works for Idle, Blocked, and already-Ready states.
	// Running state: worker will see ctx.Err() after current step.
	if proc.casState(StateIdle, StateReady) ||
		proc.casState(StateBlocked, StateReady) {
		s.global.Push(proc)
		s.wake()
	}

	return nil
}

func (s *Scheduler) complete(proc *Processor, result *StepOutput, err error) {
	res := &runtime.Result{Error: err}
	if result != nil && result.Result() != nil {
		res.Value = result.Result()
	}

	// Always remove from byPID - process is done regardless of pooling
	s.byPID.Delete(proc.pid)
	s.idleProcs.Delete(proc.pid)

	if !proc.pooled {
		s.processorCount.Add(-1)
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
	releaseProcessor(proc)
}

func (s *Scheduler) CreateProcessor(ctx context.Context, pid relay.PID, p Process) (*Processor, error) {
	if s.maxProcesses > 0 && s.processorCount.Load() >= s.maxProcesses {
		return nil, process.ErrMaxProcessesExceeded
	}

	// Wrap context with cancel for Terminate support
	procCtx, cancel := context.WithCancel(ctx)

	proc := acquireProcessor()
	proc.id = s.nextID.Add(1)
	proc.pid = pid
	proc.Process = p
	proc.setState(StateReady)
	proc.ctx = procCtx
	proc.cancel = cancel
	proc.scheduler = s
	proc.pooled = true
	proc.resultCh = make(chan *runtime.Result, 1)

	// Reset queue for this execution and cache generation
	proc.queue.Reset()
	proc.gen.Store(proc.queue.Generation())

	s.processorCount.Add(1)
	s.byPID.Store(pid, proc)

	return proc, nil
}

func (s *Scheduler) ReleaseProcessor(proc *Processor) {
	s.processorCount.Add(-1)
	s.byPID.Delete(proc.pid)
	s.idleProcs.Delete(proc.pid)
	if proc.cancel != nil {
		proc.cancel()
	}
	if proc.Process != nil {
		proc.Process.Close()
	}
	releaseProcessor(proc)
}

// Send implements relay.Receiver. Routes package to target process.
// Wakes the process if it's idle waiting for messages.
func (s *Scheduler) Send(pkg *relay.Package) error {
	target := pkg.Target // copy before push - pkg may be released after queue receives it

	v, ok := s.byPID.Load(target)
	if !ok {
		return process.ErrProcessNotFound
	}
	proc := v.(*Processor)

	// Try to wake idle process - O(1) lookup by PID
	// Must happen before push since receiver may release pkg immediately
	idleProc, wasIdle := s.idleProcs.LoadAndDelete(target)

	// Push message event to processor's queue with generation check
	if !proc.queue.Push(process.Event{
		Type: process.EventMessage,
		Data: pkg,
	}, proc.gen.Load()) {
		// Push failed - queue closed, process is terminating. Don't restore idle.
		return process.ErrProcessClosed
	}

	// Wake idle process if it was parked
	if wasIdle {
		idle := idleProc.(*Processor)
		idle.setState(StateReady)
		s.global.Push(idle)
		s.wake()
	}

	return nil
}

func (s *Scheduler) parkIdle(proc *Processor) {
	s.idleProcs.Store(proc.pid, proc)
}

func (s *Scheduler) Stats() map[string]uint64 {
	stats := make(map[string]uint64, 8)

	var executed, stolen uint64
	for _, w := range s.workers {
		executed += w.executed.Load()
		stolen += w.stolen.Load()
	}

	var byPIDCount, idleCount uint64
	s.byPID.Range(func(_, _ any) bool { byPIDCount++; return true })
	s.idleProcs.Range(func(_, _ any) bool { idleCount++; return true })

	stats["executed"] = executed
	stats["stolen"] = stolen
	stats["global_queue"] = uint64(max(0, s.global.Len())) //nolint:gosec // queue length is always non-negative and bounded
	stats["workers"] = uint64(len(s.workers))
	stats["by_pid"] = byPIDCount
	stats["idle"] = idleCount
	stats["processors"] = uint64(max(0, s.processorCount.Load())) //nolint:gosec // processor count is always non-negative

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

// StatsProvider can be implemented by Process to expose runtime statistics.
// This is an optional interface for debug/observability purposes.
type StatsProvider interface {
	Stats() any
}

// CollectProcessStats gathers statistics from all active processes.
// Each process can implement StatsProvider to expose custom stats.
func (s *Scheduler) CollectProcessStats() []any {
	var stats []any

	s.byPID.Range(func(_, value any) bool {
		proc := value.(*Processor)
		if sp, ok := proc.Process.(StatsProvider); ok {
			if s := sp.Stats(); s != nil {
				stats = append(stats, s)
			}
		}
		return true
	})

	return stats
}
