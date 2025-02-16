package pubsub

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"sync"
	"time"
)

// HostConfig holds configuration for a Host.
type HostConfig struct {
	BufferSize  int           // Internal job channel buffer size.
	WorkerCount int           // Number of concurrent worker goroutines.
	SendTimeout time.Duration // Timeout for enqueuing jobs and for receiver.Send calls.
}

// sendJob represents an asynchronous send operation.
type sendJob struct {
	pid  pubsub.PID
	msgs []*pubsub.Message
}

// Host implements a local pubsub for a single host with asynchronous sending.
type Host struct {
	receivers sync.Map // key: pid.String() -> pubsub.Downstream
	jobCh     chan sendJob
	config    HostConfig
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewHost creates a new Host instance with the provided configuration and context.
// The supplied context will cancel all workers when done.
func NewHost(ctx context.Context, config HostConfig) *Host {
	ctx, cancel := context.WithCancel(ctx)
	h := &Host{
		config: config,
		jobCh:  make(chan sendJob, config.BufferSize),
		ctx:    ctx,
		cancel: cancel,
	}
	// Spawn worker goroutines.
	for i := 0; i < config.WorkerCount; i++ {
		go h.worker()
	}
	return h
}

// Attach attaches a receiver to a PID.
// If a receiver is already attached, it returns pubsub.ErrAlreadyAttached.
func (h *Host) Attach(pid pubsub.PID, receiver pubsub.Downstream) (error, context.CancelFunc) {
	key := pid.String()
	_, loaded := h.receivers.LoadOrStore(key, receiver)
	if loaded {
		return pubsub.ErrAlreadyAttached, nil
	}
	cancel := func() {
		h.receivers.Delete(key)
	}
	return nil, cancel
}

// Send enqueues a send job for the given PID and messages.
// It returns an error only if the provided context expires or the job
// cannot be enqueued within the SendTimeout.
func (h *Host) Send(ctx context.Context, pid pubsub.PID, msgs ...*pubsub.Message) error {
	job := sendJob{
		pid:  pid,
		msgs: msgs,
	}
	select {
	case h.jobCh <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(h.config.SendTimeout):
		return fmt.Errorf("send timeout exceeded")
	}
}

// worker processes send jobs from the internal channel.
func (h *Host) worker() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case job := <-h.jobCh:
			// Retrieve the receiver for the PID.
			rec, ok := h.receivers.Load(job.pid.String())
			if !ok {
				// Optionally log that no receiver was found for this PID.
				continue
			}
			receiver, ok := rec.(pubsub.Downstream)
			if !ok {
				// Optionally log an error about invalid receiver type.
				continue
			}
			// Dispatch the send operation with timeout.
			doneCh := make(chan error, 1)
			go func() {
				doneCh <- receiver.Send(job.msgs...)
			}()
			select {
			case err := <-doneCh:
				if err != nil {
					// Optionally log the error from receiver.Send.
				}
			case <-h.ctx.Done():
				return
			case <-time.After(h.config.SendTimeout):
				// Optionally log that receiver.Send timed out.
			}
		}
	}
}
