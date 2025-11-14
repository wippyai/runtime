package relay

import (
	"context"
	"fmt"
	"sync"

	api "github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

// HostConfig holds configuration for a Host.
type HostConfig struct {
	BufferSize  int         // Internal job channel buffer size.
	WorkerCount int         // Number of concurrent worker goroutines.
	Logger      *zap.Logger // Logger for operational events.
}

// Host implements a local relay for a single host with asynchronous sending.
type Host struct {
	ctx       context.Context
	receivers sync.Map            // key: api.pid -> chan *api.Messages
	jobQueues []chan *api.Package // One queue per worker
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

	// Ensure at least one worker
	if config.WorkerCount < 1 {
		config.WorkerCount = 1
	}

	// Create one job queue per worker
	jobQueues := make([]chan *api.Package, config.WorkerCount)
	for i := 0; i < config.WorkerCount; i++ {
		jobQueues[i] = make(chan *api.Package, config.BufferSize)
	}

	h := &Host{
		config:    config,
		jobQueues: jobQueues,
		ctx:       ctx,
		logger:    config.Logger,
	}

	// Spawn worker goroutines, each with its own queue
	for i := 0; i < config.WorkerCount; i++ {
		go h.worker(i)
	}
	return h
}

// fnv1a32 is a very fast hash function for string inputs
// It's simple and provides good distribution
func fnv1a32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
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
// Uses hash of Target to route to consistent worker queue.
func (h *Host) Send(pkg *api.Package) error {
	if err := h.ctx.Err(); err != nil {
		h.logger.Warn("send after host shutdown", zap.String("pid", pkg.Target.String()))
		return err
	}

	// Use UniqID for hashing as it's the most specific part of Source
	hash := fnv1a32(pkg.Source.UniqID)
	workerIndex := int(hash) % len(h.jobQueues)

	// Send to the determined worker queue
	select {
	case h.jobQueues[workerIndex] <- pkg:
		return nil
	case <-h.ctx.Done():
		h.logger.Warn("send canceled by host shutdown", zap.String("pid", pkg.Target.String()))
		return h.ctx.Err()
	}
}

// worker processes send jobs from a specific queue
func (h *Host) worker(queueIndex int) {
	queue := h.jobQueues[queueIndex]

	for {
		select {
		case <-h.ctx.Done():
			return
		case job := <-queue:
			rec, ok := h.receivers.Load(job.Target)
			if !ok {
				h.logger.Warn("No receiver found for target PID",
					zap.String("target", job.Target.String()),
					zap.String("source", job.Source.String()))
				continue
			}

			// Handle both types of channels
			switch ch := rec.(type) {
			case chan *api.Package:
				h.deliverPackage(job, ch)
			default:
				h.logger.Error("invalid receiver type",
					zap.String("pid", job.Target.String()),
					zap.String("type", fmt.Sprintf("%T", rec)))
			}
		}
	}
}

// deliverPackage handles delivery to Package channels
func (h *Host) deliverPackage(job *api.Package, ch chan *api.Package) {
	select {
	case ch <- job:
		// Successfully sent immediately
		return
	case <-h.ctx.Done():
		h.logger.Info("worker shutting down, dropping Package message",
			zap.String("pid", job.Target.String()))
	}
}
