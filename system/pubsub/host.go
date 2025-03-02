package pubsub

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/pubsub"
	"go.uber.org/zap"
	"sync"
)

// HostConfig holds configuration for a Host.
type HostConfig struct {
	BufferSize  int         // Internal job channel buffer size.
	WorkerCount int         // Number of concurrent worker goroutines.
	Logger      *zap.Logger // Logger for operational events.
}

// sendJob represents an asynchronous send operation.
type sendJob struct {
	pkg *api.Package
}

// Host implements a local pubsub for a single host with asynchronous sending.
type Host struct {
	ctx       context.Context
	receivers sync.Map // key: api.pid -> chan *api.Messages
	jobCh     chan sendJob
	config    HostConfig
	logger    *zap.Logger
}

// NewHost creates a new Host instance with the provided configuration and context.
// The supplied context will cancel all workers when done.
func NewHost(ctx context.Context, config HostConfig) *Host {
	// If no logger provided, use noop logger
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	h := &Host{
		config: config,
		jobCh:  make(chan sendJob, config.BufferSize),
		ctx:    ctx,
		logger: config.Logger,
	}

	// todo: we have a current bug where we have to also provide a consistent order
	// for each source stream, we can not split messages from the same source
	// todo: address using consistent hashing
	config.WorkerCount = 1

	// Spawn worker goroutines.
	for i := 0; i < config.WorkerCount; i++ {
		go h.worker()
	}
	return h
}

// Attach attaches a receiver channel for Package messages.
// This method is intended for consumers that need both the sender's pid and the pkg payload.
// It registers the channel to receive messages where each message wraps the pid along with the pkg.
// Note: Only one Package receiver may be attached per pid; if one already exists, an error is returned.
func (h *Host) Attach(pid api.PID, ch chan *api.Package) (context.CancelFunc, error) {
	_, loaded := h.receivers.LoadOrStore(pid, ch)
	if loaded {
		h.logger.Warn("attempt to attach an already existing Package receiver", zap.String("pid", pid.String()))
		return nil, api.ErrAlreadyAttached
	}

	h.logger.Debug("Package receiver attached", zap.String("pid", pid.String()))

	cancel := func() {
		h.receivers.Delete(pid)
		h.logger.Debug("Package receiver detached", zap.String("pid", pid.String()))
	}
	return cancel, nil
}

// Detach removes a receiver channel from a pid.
func (h *Host) Detach(pid api.PID) {
	h.receivers.Delete(pid)
	h.logger.Debug("receiver detached", zap.String("pid", pid.String()))
}

// Send enqueues a send job for the given pid and pkg.
// It first attempts to send immediately, then falls back to a retry with timeout
// if the channel is full. Returns error if both attempts fail or context expires.
func (h *Host) Send(pkg *api.Package) error {
	if err := h.ctx.Err(); err != nil {
		h.logger.Warn("send after host shutdown", zap.String("pid", pkg.PID.String()))
		return err
	}

	// quick hash pid to num workers

	job := sendJob{
		pkg: pkg,
	}

	// First attempt: immediate send into the job channel.
	select {
	case h.jobCh <- job:
		return nil
	case <-h.ctx.Done():
		h.logger.Warn("send cancelled by host shutdown", zap.String("pid", pkg.PID.String()))
		return h.ctx.Err()
	}
}

// worker processes send jobs from the internal channel.
func (h *Host) worker() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case job := <-h.jobCh:
			rec, ok := h.receivers.Load(job.pkg.PID)
			if !ok {
				continue
			}

			// Handle both types of channels
			switch ch := rec.(type) {
			case chan *api.Package:
				h.deliverPackage(job, ch)
			default:
				h.logger.Error("invalid receiver type",
					zap.String("pid", job.pkg.PID.String()),
					zap.String("type", fmt.Sprintf("%T", rec)))
			}
		}
	}
}

// deliverPackage handles delivery to Package channels
func (h *Host) deliverPackage(job sendJob, ch chan *api.Package) {
	select {
	case ch <- job.pkg:
		// Successfully sent immediately
		return
	case <-h.ctx.Done():
		h.logger.Info("worker shutting down, dropping Package message",
			zap.String("pid", job.pkg.PID.String()))
	}
}
