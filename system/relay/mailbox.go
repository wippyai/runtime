package relay

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

// MailboxConfig holds configuration for a Mailbox.
type MailboxConfig struct {
	BufferSize  int         // Internal job channel buffer size.
	WorkerCount int         // Number of concurrent worker goroutines.
	Logger      *zap.Logger // Logger for operational events.
}

// Mailbox implements a local message relay with asynchronous delivery.
// It routes packages to attached receivers via worker goroutines.
type Mailbox struct {
	ctx       context.Context
	receivers sync.Map            // key: api.PID -> chan *api.Package
	jobQueues []chan *api.Package // One queue per worker
	config    MailboxConfig
}

// NewMailbox creates a new Mailbox instance with the provided configuration and context.
// The supplied context will cancel all workers when done.
func NewMailbox(ctx context.Context, config MailboxConfig) *Mailbox {
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

	m := &Mailbox{
		jobQueues: jobQueues,
		ctx:       ctx,
		config:    config,
	}

	for i := 0; i < config.WorkerCount; i++ {
		go m.worker(i)
	}

	// Close job queues when context is cancelled
	go func() {
		<-ctx.Done()
		for i := range jobQueues {
			close(jobQueues[i])
		}
	}()

	return m
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
func (m *Mailbox) Attach(p pid.PID, ch chan *api.Package) (context.CancelFunc, error) {
	_, loaded := m.receivers.LoadOrStore(p, ch)
	if loaded {
		m.config.Logger.Warn("attempt to attach an already existing package receiver",
			zap.String("pid", p.String()),
			zap.String("host", p.Host),
			zap.String("uniq_id", p.UniqID))
		return nil, api.NewAlreadyAttachedError(p)
	}

	return func() { m.receivers.Delete(p) }, nil
}

// Detach removes a receiver channel from a pid.
func (m *Mailbox) Detach(p pid.PID) {
	m.receivers.Delete(p)
	m.config.Logger.Debug("receiver detached", zap.String("pid", p.String()))
}

// Send enqueues a package for delivery. Messages from the same source
// are routed to the same worker to preserve per-sender FIFO ordering.
func (m *Mailbox) Send(pkg *api.Package) error {
	if err := m.ctx.Err(); err != nil {
		m.config.Logger.Warn("send after mailbox shutdown", zap.String("pid", pkg.Target.String()))
		return err
	}

	// Hash by Source.UniqID to preserve per-sender ordering
	workerIndex := int(hashString(pkg.Source.UniqID)) % m.config.WorkerCount

	m.jobQueues[workerIndex] <- pkg
	return nil
}

// worker processes packages from its dedicated queue.
func (m *Mailbox) worker(queueIndex int) {
	queue := m.jobQueues[queueIndex]

	for pkg := range queue {
		m.deliver(pkg)
	}
}

// deliver sends the package to the target's receiver channel.
func (m *Mailbox) deliver(pkg *api.Package) {
	rec, ok := m.receivers.Load(pkg.Target)
	if !ok {
		var topic string
		if len(pkg.Messages) > 0 {
			topic = pkg.Messages[0].Topic
		}
		m.config.Logger.Debug("no receiver found for target PID",
			zap.String("target", pkg.Target.String()),
			zap.String("source", pkg.Source.String()),
			zap.String("topic", topic))
		return
	}

	ch, ok := rec.(chan *api.Package)
	if !ok {
		m.config.Logger.Error("receiver has invalid type",
			zap.String("target", pkg.Target.String()))
		return
	}

	ch <- pkg
}
