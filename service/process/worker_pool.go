package process

import (
	"context"
	"sync"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"go.uber.org/zap"
)

// ProcessRunner defines an interface for running process steps
type ProcessRunner interface {
	Step() error
}

// WorkRequest represents a request to execute a process step
type WorkRequest struct {
	PID    pubsub.PID    // Process ID
	Runner ProcessRunner // Process to run
}

// WorkerPool manages a pool of workers for executing process steps
type WorkerPool struct {
	workers int
	log     *zap.Logger
	workCh  chan *WorkRequest

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(ctx context.Context, workers int, queueSize int, log *zap.Logger) *WorkerPool {
	ctx, cancel := context.WithCancel(ctx)

	return &WorkerPool{
		workers: workers,
		log:     log,
		workCh:  make(chan *WorkRequest, queueSize),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start launches the worker goroutines
func (p *WorkerPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Stop gracefully shuts down the worker pool
func (p *WorkerPool) Stop() {
	p.cancel()
	p.wg.Wait()
}

func (p *WorkerPool) Terminate(pid pubsub.PID) {
	// todo: to be updated
}

// Schedule adds a process step execution request to the work queue
func (p *WorkerPool) Schedule(req *WorkRequest) error {
	select {
	case p.workCh <- req:
		return nil
	case <-p.ctx.Done():
		return context.Canceled
	default:
		return process.ErrHostBusy
	}
}

// worker runs in its own goroutine and processChannels work requests
func (p *WorkerPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return

		case work := <-p.workCh:
			if err := work.Runner.Step(); err != nil {
				p.log.Debug("process step completed with error",
					zap.String("pid", work.PID.String()),
					zap.Error(err))
			}
			// todo: to be updated
			//log.Printf("process step completed: %s", work.PID)

			p.workCh <- work
		}
	}
}

// WorkChannel returns the channel for submitting work requests
func (p *WorkerPool) WorkChannel() chan<- *WorkRequest {
	return p.workCh
}
