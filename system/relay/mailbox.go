// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

// mailboxConfig holds internal configuration for a Mailbox.
type mailboxConfig struct {
	logger      *zap.Logger
	bufferSize  int
	workerCount int
}

// MailboxOption configures a Mailbox.
type MailboxOption func(*mailboxConfig)

// WithBufferSize sets the internal job channel buffer size.
func WithBufferSize(size int) MailboxOption {
	return func(c *mailboxConfig) {
		c.bufferSize = size
	}
}

// WithWorkerCount sets the number of concurrent worker goroutines.
func WithWorkerCount(count int) MailboxOption {
	return func(c *mailboxConfig) {
		c.workerCount = count
	}
}

// WithLogger sets the logger for operational events.
func WithLogger(logger *zap.Logger) MailboxOption {
	return func(c *mailboxConfig) {
		c.logger = logger
	}
}

// Mailbox implements a local message relay with asynchronous delivery.
// It routes packages to attached receivers via worker goroutines.
type Mailbox struct {
	config    mailboxConfig
	ctx       context.Context
	receivers sync.Map
	jobQueues []chan *api.Package
	lifecycle sync.RWMutex
	delivery  sync.RWMutex
	closed    bool
}

// NewMailbox creates a new Mailbox instance with the provided options.
// The supplied context will cancel all workers when done.
func NewMailbox(ctx context.Context, opts ...MailboxOption) *Mailbox {
	if ctx == nil {
		ctx = context.Background()
	}

	config := mailboxConfig{
		workerCount: 1,
		logger:      zap.NewNop(),
	}

	for _, opt := range opts {
		opt(&config)
	}

	if config.workerCount < 1 {
		config.workerCount = 1
	}

	jobQueues := make([]chan *api.Package, config.workerCount)
	for i := 0; i < config.workerCount; i++ {
		jobQueues[i] = make(chan *api.Package, config.bufferSize)
	}

	m := &Mailbox{
		jobQueues: jobQueues,
		ctx:       ctx,
		config:    config,
	}

	for i := 0; i < config.workerCount; i++ {
		go m.worker(i)
	}

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
	m.lifecycle.RLock()
	defer m.lifecycle.RUnlock()
	key := p.String()
	_, loaded := m.receivers.LoadOrStore(key, ch)
	if loaded {
		m.config.logger.Warn("attempt to attach an already existing package receiver",
			zap.String("pid", key),
			zap.String("host", p.Host),
			zap.String("uniq_id", p.UniqID))
		return nil, NewAlreadyAttachedError(p)
	}

	return func() { m.Detach(p) }, nil
}

// Detach removes a receiver channel from a pid.
func (m *Mailbox) Detach(p pid.PID) {
	key := p.String()
	m.lifecycle.Lock()
	if rec, ok := m.receivers.LoadAndDelete(key); ok {
		if ch, ok := rec.(chan *api.Package); ok {
			drainReceiver(ch)
		}
	}
	m.lifecycle.Unlock()
	m.config.logger.Debug("receiver detached", zap.String("pid", key))
}

// Send enqueues a package for delivery. Messages from the same source
// are routed to the same worker to preserve per-sender FIFO ordering.
func (m *Mailbox) Send(pkg *api.Package) error {
	return m.SendContext(context.Background(), pkg)
}

// SendContext enqueues a package until either the mailbox or caller context
// is canceled. The caller context is owned by the delivery operation; the
// mailbox context remains the lifecycle boundary for its workers.
func (m *Mailbox) SendContext(ctx context.Context, pkg *api.Package) error {
	if pkg == nil {
		return NewNilPackageError()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := m.ctx.Err(); err != nil {
		m.config.logger.Warn("send after mailbox shutdown", zap.String("pid", pkg.Target.String()))
		return err
	}

	// Hash by Source.UniqID to preserve per-sender ordering
	workerIndex := int(hashString(pkg.Source.UniqID)) % m.config.workerCount

	// The lifecycle read lock linearizes queue admission with worker shutdown.
	// A worker drains queued packages only after acquiring the write lock, so a
	// successful send always has a live owner and a rejected send remains owned
	// by its caller.
	m.lifecycle.RLock()
	if m.closed {
		m.lifecycle.RUnlock()
		return m.ctx.Err()
	}
	select {
	case m.jobQueues[workerIndex] <- pkg:
		m.lifecycle.RUnlock()
		return nil
	case <-ctx.Done():
		m.lifecycle.RUnlock()
		return ctx.Err()
	case <-m.ctx.Done():
		m.lifecycle.RUnlock()
		m.config.logger.Warn("send after mailbox shutdown", zap.String("pid", pkg.Target.String()))
		return m.ctx.Err()
	}
}

// worker processes packages from its dedicated queue until the context is
// canceled. The queues are never closed (a send racing a close would panic),
// so the worker exits on ctx.Done rather than ranging over a closed channel.
func (m *Mailbox) worker(queueIndex int) {
	queue := m.jobQueues[queueIndex]

	for {
		select {
		case pkg := <-queue:
			m.deliver(pkg)
		case <-m.ctx.Done():
			m.shutdown()
			return
		}
	}
}

// shutdown closes admission and releases packages still waiting in every
// worker queue. The queues remain open because SendContext may be racing the
// context cancellation; the lifecycle lock makes that race deterministic.
func (m *Mailbox) shutdown() {
	m.lifecycle.Lock()
	if m.closed {
		m.lifecycle.Unlock()
		return
	}
	m.closed = true
	for _, queue := range m.jobQueues {
	drain:
		for {
			select {
			case pkg := <-queue:
				api.ReleasePackage(pkg)
			default:
				break drain
			}
		}
	}
	m.lifecycle.Unlock()

	// Wait for in-flight deliveries to observe the canceled mailbox context
	// before draining attached channels. This lock is separate from lifecycle:
	// Detach must remain able to remove a blocked receiver without waiting for a
	// potentially slow consumer.
	m.delivery.Lock()
	m.receivers.Range(func(_, value any) bool {
		if ch, ok := value.(chan *api.Package); ok {
			drainReceiver(ch)
		}
		return true
	})
	m.delivery.Unlock()
}

// deliver sends the package to the target's receiver channel.
func (m *Mailbox) deliver(pkg *api.Package) {
	if pkg == nil {
		return
	}
	targetKey := pkg.Target.String()
	rec, ok := m.receivers.Load(targetKey)
	if !ok {
		var topic string
		if len(pkg.Messages) > 0 {
			topic = pkg.Messages[0].Topic
		}
		m.config.logger.Debug("no receiver found for target PID",
			zap.String("target", targetKey),
			zap.String("source", pkg.Source.String()),
			zap.String("topic", topic))
		api.ReleasePackage(pkg)
		return
	}

	ch, ok := rec.(chan *api.Package)
	if !ok {
		m.config.logger.Error("receiver has invalid type",
			zap.String("target", targetKey))
		api.ReleasePackage(pkg)
		return
	}

	m.deliverTo(ch, pkg, targetKey)
}

// deliverTo performs the receiver-channel send. The channel is owned by the
// attached process, which may close it concurrently with this send (Detach only
// removes the map entry). A send on a closed channel panics, which must never
// take down the worker, so it is recovered: a closed receiver means the process
// is gone and the package is dropped.
func (m *Mailbox) deliverTo(ch chan *api.Package, pkg *api.Package, targetKey string) {
	m.delivery.RLock()
	defer m.delivery.RUnlock()

	delivered := false
	defer func() {
		if r := recover(); r != nil {
			m.config.logger.Debug("dropped delivery to closed receiver",
				zap.String("target", targetKey))
		}
		if !delivered {
			api.ReleasePackage(pkg)
		}
	}()

	if err := m.ctx.Err(); err != nil {
		m.config.logger.Debug("delivery canceled",
			zap.String("target", targetKey), zap.Error(err))
		return
	}

	select {
	case ch <- pkg:
		delivered = true
	case <-m.ctx.Done():
		m.config.logger.Debug("delivery canceled",
			zap.String("target", targetKey))
	}
}

// drainReceiver releases packages that were already accepted by an attached
// channel after its owner detaches. Detach serializes with deliverTo, so no
// package can become unreachable between the final send and this drain.
func drainReceiver(ch chan *api.Package) {
	for {
		select {
		case pkg, ok := <-ch:
			if !ok {
				return
			}
			api.ReleasePackage(pkg)
		default:
			return
		}
	}
}
