package host

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"go.uber.org/zap"
)

// processEntry holds process state and execution lock
type processEntry struct {
	process process.Process
	running atomic.Bool // Atomic flag to prevent concurrent execution
}

// ProcessPool manages concurrent process execution
type ProcessPool struct {
	workers   int
	log       *zap.Logger
	processes sync.Map        // map[pubsub.PID]*processEntry
	workCh    chan pubsub.PID // Channel for scheduling work

	wg        sync.WaitGroup // Worker goroutines WaitGroup
	processWG sync.WaitGroup // Active processes WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewProcessPool(
	ctx context.Context,
	workers int,
	queueSize int,
	log *zap.Logger,
) *ProcessPool {
	ctx, cancel := context.WithCancel(ctx)

	return &ProcessPool{
		workers: workers,
		log:     log,
		workCh:  make(chan pubsub.PID, queueSize),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// AddProcess registers a new process with the pool
func (p *ProcessPool) AddProcess(pid pubsub.PID, proc process.Process) error {
	entry := &processEntry{
		process: proc,
	}

	if _, loaded := p.processes.LoadOrStore(pid, entry); loaded {
		return process.ErrHostBusy
	}

	p.processWG.Add(1)

	// Schedule initial execution
	return p.Schedule(pid)
}

// HasProcess checks if a process exists in the pool
func (p *ProcessPool) HasProcess(pid pubsub.PID) bool {
	_, exists := p.processes.Load(pid)
	return exists
}

// Schedule adds a process to the work queue
func (p *ProcessPool) Schedule(pid pubsub.PID) error {
	select {
	case p.workCh <- pid:
		return nil
	case <-p.ctx.Done():
		return context.Canceled
	}
}

// Start launches the worker goroutines
func (p *ProcessPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Close gracefully shuts down the worker pool
func (p *ProcessPool) Close() {
	p.cancel()
	p.wg.Wait()
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
			entryVal, exists := p.processes.Load(pid)
			if !exists {
				p.log.Warn("process not found or evicted", zap.String("pid", pid.String()))
				continue
			}

			entry := entryVal.(*processEntry)

			// Try to acquire execution lock
			if !entry.running.CompareAndSwap(false, true) {
				p.log.Debug("process already running", zap.String("pid", pid.String()))
				continue
			}

			// Execute process step
			reschedule, err := entry.process.Step()
			if err != nil {
				p.log.Debug("process step completed with error",
					zap.String("pid", pid.String()),
					zap.Error(err))

				// Process is done (with error)
				p.processes.Delete(pid)
				p.processWG.Done()
				continue
			}

			// Release execution lock
			entry.running.Store(false)

			// Reschedule only if process requested it
			if reschedule {
				select {
				case <-p.ctx.Done():
					return
				case p.workCh <- pid:
				}
			}
		}
	}
}

// RemoveProcess removes a process from the pool
func (p *ProcessPool) RemoveProcess(pid pubsub.PID) {
	if _, exists := p.processes.LoadAndDelete(pid); exists {
		p.processWG.Done()
	}
}

// CancelProcess sends a cancellation signal to a specific process
func (p *ProcessPool) CancelProcess(pid pubsub.PID) error {
	entryVal, exists := p.processes.Load(pid)
	if !exists {
		return process.ErrNoProcess
	}

	entry := entryVal.(*processEntry)

	// Create cancel message batch
	batch := pubsub.NewBatch(
		process.TopicEvents,
		payload.New(topology.CancelEvent{
			Event: topology.Event{
				At:   time.Now(),
				Kind: topology.KindCancel,
			},
		}),
	)

	// Send cancel message to process
	if err := entry.process.Send(batch); err != nil {
		p.log.Warn("failed to send cancel message to process",
			zap.String("pid", pid.String()),
			zap.Error(err))
	}

	// Let process handle cancellation in next schedule
	return p.Schedule(pid)
}

// CancelAll sends cancellation signals to all processes and waits for completion
func (p *ProcessPool) CancelAll(ctx context.Context) error {
	p.processes.Range(func(key, _ interface{}) bool {
		pid := key.(pubsub.PID)
		if err := p.CancelProcess(pid); err != nil {
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
