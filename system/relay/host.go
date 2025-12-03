package relay

import (
	"context"
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
	receivers sync.Map            // key: api.PID -> chan *api.Package
	jobQueues []chan *api.Package // One queue per worker
	config    HostConfig
}

// NewHost creates a new Host instance with the provided configuration and context.
// The supplied context will cancel all workers when done.
func NewHost(ctx context.Context, config HostConfig) *Host {
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	if config.WorkerCount < 1 {
		config.WorkerCount = 1
	}

	jobQueues := make([]chan *api.Package, config.WorkerCount)
	for i := 0; i < config.WorkerCount; i++ {
		jobQueues[i] = make(chan *api.Package, config.BufferSize)
	}

	h := &Host{
		jobQueues: jobQueues,
		ctx:       ctx,
		config:    config,
	}

	for i := 0; i < config.WorkerCount; i++ {
		go h.worker(i)
	}

	// Close job queues when context is cancelled
	go func() {
		<-ctx.Done()
		for i := range jobQueues {
			close(jobQueues[i])
		}
	}()

	return h
}

// hashString computes a fast hash for worker distribution.
// Uses FNV-1a which is optimal for short strings like UniqIDs.
func hashString(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// Attach attaches a receiver channel for Package messages.
// Only one receiver may be attached per PID; if one already exists, an error is returned.
func (h *Host) Attach(pid api.PID, ch chan *api.Package) (context.CancelFunc, error) {
	_, loaded := h.receivers.LoadOrStore(pid, ch)
	if loaded {
		h.config.Logger.Warn("attempt to attach an already existing package receiver",
			zap.String("pid", pid.String()),
			zap.String("host", pid.Host),
			zap.String("uniq_id", pid.UniqID))
		return nil, api.NewAlreadyAttachedError(pid)
	}

	return func() { h.receivers.Delete(pid) }, nil
}

// Detach removes a receiver channel from a pid.
func (h *Host) Detach(pid api.PID) {
	h.receivers.Delete(pid)
	h.config.Logger.Debug("receiver detached", zap.String("pid", pid.String()))
}

// Send enqueues a package for delivery. Messages from the same source
// are routed to the same worker to preserve per-sender FIFO ordering.
func (h *Host) Send(pkg *api.Package) error {
	if err := h.ctx.Err(); err != nil {
		h.config.Logger.Warn("send after host shutdown", zap.String("pid", pkg.Target.String()))
		return err
	}

	// Hash by Source.UniqID to preserve per-sender ordering
	workerIndex := int(hashString(pkg.Source.UniqID)) % h.config.WorkerCount

	h.jobQueues[workerIndex] <- pkg
	return nil
}

// worker processes packages from its dedicated queue.
func (h *Host) worker(queueIndex int) {
	queue := h.jobQueues[queueIndex]

	for pkg := range queue {
		h.deliver(pkg)
	}
}

// deliver sends the package to the target's receiver channel.
func (h *Host) deliver(pkg *api.Package) {
	rec, ok := h.receivers.Load(pkg.Target)
	if !ok {
		var topic string
		if len(pkg.Messages) > 0 {
			topic = pkg.Messages[0].Topic
		}
		h.config.Logger.Debug("no receiver found for target PID",
			zap.String("target", pkg.Target.String()),
			zap.String("source", pkg.Source.String()),
			zap.String("topic", topic))
		return
	}

	rec.(chan *api.Package) <- pkg
}
