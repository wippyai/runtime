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
	BufferSize   int           // Internal job channel buffer size.
	WorkerCount  int           // Number of concurrent worker goroutines.
	Logger       *zap.Logger   // Logger for operational events
	RetryTimeout time.Duration // Timeout for retry attempt on send (default: 100ms)
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
func (h *Host) Attach(pid api.PID, ch chan *api.Batch) (error, context.CancelFunc) {
	_, loaded := h.receivers.LoadOrStore(pid, ch)
	if loaded {
		h.logger.Warn("attempt to attach already existing receiver",
			zap.String("node", pid.Node),
			zap.String("host", pid.Host),
			zap.String("id", pid.ID.String()),
			zap.String("uniq_id", pid.UniqID),
		)
		return api.ErrAlreadyAttached, nil
	}

	h.logger.Debug("receiver attached",
		zap.String("node", pid.Node),
		zap.String("host", pid.Host),
		zap.String("id", pid.ID.String()),
		zap.String("uniq_id", pid.UniqID),
	)

	cancel := func() {
		h.receivers.Delete(pid)
		h.logger.Debug("receiver detached",
			zap.String("node", pid.Node),
			zap.String("host", pid.Host),
			zap.String("id", pid.ID.String()),
			zap.String("uniq_id", pid.UniqID),
		)
	}
	return nil, cancel
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

	// First attempt: immediate send
	select {
	case h.jobCh <- job:
		return nil
	case <-ctx.Done():
		h.logger.Warn("send cancelled by context",
			zap.String("node", pid.Node),
			zap.String("host", pid.Host),
			zap.String("id", pid.ID.String()),
			zap.String("uniq_id", pid.UniqID),
			zap.Error(ctx.Err()),
		)
		return ctx.Err()
	default:
		// Channel is full, try again with timeout
		timer := time.NewTimer(h.config.RetryTimeout)
		defer timer.Stop()

		select {
		case h.jobCh <- job:
			return nil
		case <-ctx.Done():
			h.logger.Warn("send cancelled by context during retry",
				zap.String("node", pid.Node),
				zap.String("host", pid.Host),
				zap.String("id", pid.ID.String()),
				zap.String("uniq_id", pid.UniqID),
				zap.Error(ctx.Err()),
			)
			return ctx.Err()
		case <-timer.C:
			h.logger.Warn("send failed after retry timeout",
				zap.String("node", pid.Node),
				zap.String("host", pid.Host),
				zap.String("id", pid.ID.String()),
				zap.String("uniq_id", pid.UniqID),
				zap.Duration("timeout", h.config.RetryTimeout),
			)
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
			// Check if the job's context is still valid
			if job.ctx.Err() != nil {
				h.logger.Warn("job context expired before processing",
					zap.String("node", job.pid.Node),
					zap.String("host", job.pid.Host),
					zap.String("id", job.pid.ID.String()),
					zap.String("uniq_id", job.pid.UniqID),
					zap.Error(job.ctx.Err()),
				)
				continue
			}

			// Retrieve the receiver channel for the PID
			rec, ok := h.receivers.Load(job.pid)
			if !ok {
				h.logger.Warn("no receiver found for PID",
					zap.String("node", job.pid.Node),
					zap.String("host", job.pid.Host),
					zap.String("id", job.pid.ID.String()),
					zap.String("uniq_id", job.pid.UniqID),
				)
				continue
			}

			ch, ok := rec.(chan *api.Batch)
			if !ok {
				h.logger.Error("invalid receiver type",
					zap.String("node", job.pid.Node),
					zap.String("host", job.pid.Host),
					zap.String("id", job.pid.ID.String()),
					zap.String("uniq_id", job.pid.UniqID),
					zap.String("type", fmt.Sprintf("%T", rec)),
				)
				continue
			}

			// Send the batch with context cancellation
			select {
			case ch <- job.batch:
				// Successfully sent
			case <-job.ctx.Done():
				h.logger.Warn("send cancelled while delivering to receiver",
					zap.String("node", job.pid.Node),
					zap.String("host", job.pid.Host),
					zap.String("id", job.pid.ID.String()),
					zap.String("uniq_id", job.pid.UniqID),
					zap.Error(job.ctx.Err()),
				)
			case <-h.ctx.Done():
				h.logger.Info("worker shutting down, dropping message",
					zap.String("node", job.pid.Node),
					zap.String("host", job.pid.Host),
					zap.String("id", job.pid.ID.String()),
					zap.String("uniq_id", job.pid.UniqID),
				)
				return
			}
		}
	}
}
