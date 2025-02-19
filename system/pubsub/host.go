package pubsub

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/pubsub"
	"go.uber.org/zap"
	"sync"
	"time"
)

// HostConfig holds configuration for a Host.
type HostConfig struct {
	BufferSize      int           // Internal job channel buffer size.
	WorkerCount     int           // Number of concurrent worker goroutines.
	Logger          *zap.Logger   // Logger for operational events.
	RetryTimeout    time.Duration // Timeout for retry attempt on send (default: 100ms)
	DeliveryTimeout time.Duration // Timeout for delivery to receiver (default: 30s)
}

// sendJob represents an asynchronous send operation.
type sendJob struct {
	pid   api.PID
	batch *api.Batch
	ctx   context.Context
}

// Host implements a local pubsub for a single host with asynchronous sending.
type Host struct {
	receivers sync.Map // key: api.PID -> chan *api.Batch
	jobCh     chan sendJob
	config    HostConfig
	ctx       context.Context
	logger    *zap.Logger
}

// NewHost creates a new Host instance with the provided configuration and context.
// The supplied context will cancel all workers when done.
func NewHost(ctx context.Context, config HostConfig) *Host {
	// If no logger provided, use noop logger
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	// Set default retry timeout if not provided
	if config.RetryTimeout == 0 {
		config.RetryTimeout = 100 * time.Millisecond
	}
	// Set default delivery timeout if not provided
	if config.DeliveryTimeout == 0 {
		config.DeliveryTimeout = 30 * time.Second
	}

	h := &Host{
		config: config,
		jobCh:  make(chan sendJob, config.BufferSize),
		ctx:    ctx,
		logger: config.Logger,
	}

	// Spawn worker goroutines.
	for i := 0; i < config.WorkerCount; i++ {
		go h.worker()
	}
	return h
}

// Attach attaches a receiver channel to a PID.
// If a receiver is already attached, it returns ErrAlreadyAttached.
func (h *Host) Attach(pid api.PID, ch chan *api.Batch) (context.CancelFunc, error) {
	_, loaded := h.receivers.LoadOrStore(pid, ch)
	if loaded {
		h.logger.Warn("attempt to attach already existing receiver", zap.String("pid", pid.String()))
		return nil, api.ErrAlreadyAttached
	}

	h.logger.Debug("receiver attached", zap.String("pid", pid.String()))

	cancel := func() {
		h.receivers.Delete(pid)
		h.logger.Debug("receiver detached", zap.String("pid", pid.String()))
	}
	return cancel, nil
}

// AttachWithPID attaches a receiver channel for PIDBatch messages.
// This method is intended for consumers that need both the sender's PID and the batch payload.
// It registers the channel to receive messages where each message wraps the PID along with the batch.
// Note: Only one PIDBatch receiver may be attached per PID; if one already exists, an error is returned.
func (h *Host) AttachWithPID(pid api.PID, ch chan *api.PIDBatch) (context.CancelFunc, error) {
	_, loaded := h.receivers.LoadOrStore(pid, ch)
	if loaded {
		h.logger.Warn("attempt to attach an already existing PIDBatch receiver", zap.String("pid", pid.String()))
		return nil, api.ErrAlreadyAttached
	}

	h.logger.Debug("PIDBatch receiver attached", zap.String("pid", pid.String()))

	cancel := func() {
		h.receivers.Delete(pid)
		h.logger.Debug("PIDBatch receiver detached", zap.String("pid", pid.String()))
	}
	return cancel, nil
}

// Detach removes a receiver channel from a PID.
func (h *Host) Detach(pid api.PID) {
	h.receivers.Delete(pid) // todo: we can add more validation here
	h.logger.Debug("receiver detached", zap.String("pid", pid.String()))
}

// Send enqueues a send job for the given PID and batch.
// It first attempts to send immediately, then falls back to a retry with timeout
// if the channel is full. Returns error if both attempts fail or context expires.
func (h *Host) Send(ctx context.Context, pid api.PID, batch *api.Batch) error {
	job := sendJob{
		pid:   pid,
		batch: batch,
		ctx:   ctx,
	}

	// First attempt: immediate send into the job channel.
	select {
	case h.jobCh <- job:
		return nil
	case <-ctx.Done():
		h.logger.Warn("send cancelled by context", zap.String("pid", pid.String()), zap.Error(ctx.Err()))
		return ctx.Err()
	case <-h.ctx.Done():
		h.logger.Warn("send cancelled by host shutdown", zap.String("pid", pid.String()))
		return h.ctx.Err()
	default:
		// Channel is full, try again with a retry timeout.
		timer := time.NewTimer(h.config.RetryTimeout)
		defer timer.Stop()

		select {
		case h.jobCh <- job:
			return nil
		case <-ctx.Done():
			h.logger.Warn("send cancelled by context during retry", zap.String("pid", pid.String()), zap.Error(ctx.Err()))
			return ctx.Err()
		case <-h.ctx.Done():
			h.logger.Warn("send cancelled by host shutdown", zap.String("pid", pid.String()))
			return h.ctx.Err()
		case <-timer.C:
			h.logger.Warn("send failed after retry timeout", zap.String("pid", pid.String()), zap.Duration("timeout", h.config.RetryTimeout))
			return fmt.Errorf("send timeout exceeded after retry")
		}
	}
}

// worker processes send jobs from the internal channel.
func (h *Host) worker() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case job := <-h.jobCh:
			if job.ctx.Err() != nil {
				h.logger.Warn("job context expired before processing", zap.String("pid", job.pid.String()), zap.Error(job.ctx.Err()))
				continue
			}

			rec, ok := h.receivers.Load(job.pid)
			if !ok {
				continue
			}

			// Handle both types of channels
			switch ch := rec.(type) {
			case chan *api.Batch:
				h.deliverBatch(job, ch)
			case chan *api.PIDBatch:
				h.deliverPIDBatch(job, ch)
			default:
				h.logger.Error("invalid receiver type",
					zap.String("pid", job.pid.String()),
					zap.String("type", fmt.Sprintf("%T", rec)))
			}
		}
	}
}

// deliverBatch handles delivery to regular Batch channels
func (h *Host) deliverBatch(job sendJob, ch chan *api.Batch) {
	select {
	case ch <- job.batch:
		// Successfully sent immediately
		return
	default:
		// Use timeout-based delivery
		select {
		case ch <- job.batch:
			// Successfully sent after waiting
		case <-job.ctx.Done():
			h.logger.Warn("send cancelled while delivering to receiver",
				zap.String("pid", job.pid.String()),
				zap.Error(job.ctx.Err()))
		case <-time.After(h.config.DeliveryTimeout):
			h.logger.Warn("delivery timeout exceeded; dropping message",
				zap.String("pid", job.pid.String()),
				zap.Duration("delivery_timeout", h.config.DeliveryTimeout))
		case <-h.ctx.Done():
			h.logger.Info("worker shutting down, dropping message",
				zap.String("pid", job.pid.String()))
		}
	}
}

// deliverPIDBatch handles delivery to PIDBatch channels
func (h *Host) deliverPIDBatch(job sendJob, ch chan *api.PIDBatch) {
	pidBatch := &api.PIDBatch{
		PID:   job.pid,
		Batch: job.batch,
	}

	select {
	case ch <- pidBatch:
		// Successfully sent immediately
		return
	default:
		// Use timeout-based delivery
		select {
		case ch <- pidBatch:
			// Successfully sent after waiting
		case <-job.ctx.Done():
			h.logger.Warn("send cancelled while delivering to PIDBatch receiver",
				zap.String("pid", job.pid.String()),
				zap.Error(job.ctx.Err()))
		case <-time.After(h.config.DeliveryTimeout):
			h.logger.Warn("delivery timeout exceeded; dropping PIDBatch message",
				zap.String("pid", job.pid.String()),
				zap.Duration("delivery_timeout", h.config.DeliveryTimeout))
		case <-h.ctx.Done():
			h.logger.Info("worker shutting down, dropping PIDBatch message",
				zap.String("pid", job.pid.String()))
		}
	}
}
