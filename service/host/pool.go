package host

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/process"

	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"go.uber.org/zap"
)

// processEntry holds process state and execution lock
type processEntry struct {
	process process.Process
	running atomic.Bool
	awaken  atomic.Bool
}

// ProcessPool manages concurrent process execution
type ProcessPool struct {
	workers      int
	numProcesses atomic.Int32
	maxProcesses int
	log          *zap.Logger
	processes    sync.Map        // map[pubsub.Target]*processEntry
	workCh       chan pubsub.PID // Channel for scheduling work
	wg           sync.WaitGroup  // Worker goroutines WaitGroup
	processWG    sync.WaitGroup  // Active processes WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewProcessPool creates a new process pool
func NewProcessPool(
	ctx context.Context,
	workers int,
	maxProcesses int,
	log *zap.Logger,
) *ProcessPool {
	ctx, cancel := context.WithCancel(ctx)

	return &ProcessPool{
		workers:      workers,
		maxProcesses: maxProcesses,
		log:          log,
		workCh:       make(chan pubsub.PID, maxProcesses+1),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Add registers a new process with the pool
func (p *ProcessPool) Add(pid pubsub.PID, proc process.Process) error {
	if p.maxProcesses != 0 && p.numProcesses.Load() >= int32(p.maxProcesses) {
		p.log.Warn("max processes reached, cannot add new process", zap.String("pid", pid.String()))
		return process.ErrMaxProcesses
	}

	entry := &processEntry{
		process: proc,
	}

	if _, loaded := p.processes.LoadOrStore(pid.String(), entry); loaded {
		return process.ErrHostBusy
	}

	p.processWG.Add(1)
	p.numProcesses.Add(1)

	// Schedule initial execution
	return p.Schedule(pid)
}

// Cancel sends a cancellation signal to a specific process
func (p *ProcessPool) Cancel(pid pubsub.PID, deadline time.Time) error {
	entryVal, exists := p.processes.Load(pid.String())
	if !exists {
		return process.ErrNoProcess
	}

	entry := entryVal.(*processEntry)

	// send cancel message to process
	if err := entry.process.Send(topology.Cancel(pid, pid, deadline)); err != nil {
		p.log.Warn("failed to send cancel message to process",
			zap.String("pid", pid.String()),
			zap.Error(err))
	}

	// Let process handle cancellation in next Schedule
	return p.Schedule(pid)
}

// CancelAll sends cancellation signals to all processes and waits for completion
func (p *ProcessPool) CancelAll(ctx context.Context, deadline time.Time) error {
	p.processes.Range(func(key, _ interface{}) bool {
		pid, _ := pubsub.ParsePID(key.(string))
		if err := p.Cancel(pid, deadline); err != nil {
			p.log.Warn("failed to cancel process",
				zap.String("pid", pid.String()),
				zap.Error(err))
		}
		return true
	})

	// Wait for all processes to complete or context to cancel
	done := make(chan struct{})
	go func() {
		p.processWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close gracefully shuts down the worker pool
func (p *ProcessPool) Close() {
	p.cancel()
	p.wg.Wait()
}

// Has checks if a process exists in the pool
func (p *ProcessPool) Has(pid pubsub.PID) bool {
	_, exists := p.processes.Load(pid.String())
	return exists
}

// Remove removes a process from the pool
func (p *ProcessPool) Remove(pid pubsub.PID) {
	if _, exists := p.processes.LoadAndDelete(pid.String()); exists {
		p.processWG.Done()
		p.numProcesses.Add(^int32(0))
	}
}

// Schedule adds a process to the work queue
func (p *ProcessPool) Schedule(pid pubsub.PID) error {
	pr, exists := p.processes.Load(pid.String())
	if !exists {
		return process.ErrNoProcess
	}

	if pr.(*processEntry).awaken.CompareAndSwap(false, true) {
		select {
		case p.workCh <- pid:
			return nil
		case <-p.ctx.Done():
			return context.Canceled
		}
	}

	return nil
}

// Send sends a message to a specific process
func (p *ProcessPool) Send(pid pubsub.PID, pkg *pubsub.Package) error {
	entryVal, exists := p.processes.Load(pid.String())
	if !exists {
		return process.ErrNoProcess
	}

	return entryVal.(*processEntry).process.Send(pkg)
}

// Start launches the worker goroutines
func (p *ProcessPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Terminate notifies a process about termination
func (p *ProcessPool) Terminate(pid pubsub.PID) {
	entryVal, exists := p.processes.Load(pid.String())
	if !exists {
		return
	}

	entryVal.(*processEntry).process.Terminate()
}

// worker runs in its own goroutine and processes work requests
func (p *ProcessPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return

		case pid := <-p.workCh:
			// Get process entry
			entryVal, exists := p.processes.Load(pid.String())
			if !exists {
				continue // most likely async task stuck in the queue
			}

			entry := entryVal.(*processEntry)
			entry.awaken.Store(false) // we got it to work

			// Try to acquire execution lock
			if !entry.running.CompareAndSwap(false, true) {
				continue // handled by another goroutine
			}

			err := entry.process.Step()
			if err != nil {
				p.log.Debug("process step completed with error",
					zap.String("pid", pid.String()),
					zap.Error(err))

				// Process is done (with error)
				p.Remove(pid)
				continue
			}

			// Release execution lock
			entry.running.Store(false)

			// still have tasks in the queue
			if entry.process.Ready() > 0 && entry.awaken.CompareAndSwap(false, true) {
				select {
				case <-p.ctx.Done():
					return
				case p.workCh <- pid:
				}
			}
		}
	}
}
