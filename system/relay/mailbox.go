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
	config     mailboxConfig
	ctx        context.Context
	receivers  sync.Map
	jobQueues  []chan mailboxJob
	lifecycle  sync.RWMutex
	admissions sync.WaitGroup
	closed     bool
}

type mailboxJob struct {
	pkg      *api.Package
	receiver *mailboxReceiver
}

// mailboxReceiver is one attachment incarnation. Deliveries hold an active
// reference while they are sending to the channel; Detach marks the
// incarnation closed, waits for those sends to finish, then drains anything
// they accepted. This makes a buffered channel safe to detach and reattach
// without letting an old delivery strand a package in the old channel.
type mailboxReceiver struct {
	ch       chan *api.Package
	done     chan struct{}
	active   sync.WaitGroup
	mu       sync.Mutex
	detached bool
}

func newMailboxReceiver(ch chan *api.Package) *mailboxReceiver {
	return &mailboxReceiver{ch: ch, done: make(chan struct{})}
}

func (r *mailboxReceiver) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.detached {
		return false
	}
	r.active.Add(1)
	return true
}

func (r *mailboxReceiver) end() {
	r.active.Done()
}

func (r *mailboxReceiver) stop() {
	r.mu.Lock()
	if !r.detached {
		r.detached = true
		close(r.done)
	}
	r.mu.Unlock()
	r.active.Wait()
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

	jobQueues := make([]chan mailboxJob, config.workerCount)
	for i := 0; i < config.workerCount; i++ {
		jobQueues[i] = make(chan mailboxJob, config.bufferSize)
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
	if m.closed {
		return nil, m.ctx.Err()
	}
	key := p.String()
	receiver := newMailboxReceiver(ch)
	_, loaded := m.receivers.LoadOrStore(key, receiver)
	if loaded {
		m.config.logger.Warn("attempt to attach an already existing package receiver",
			zap.String("pid", key),
			zap.String("host", p.Host),
			zap.String("uniq_id", p.UniqID))
		return nil, NewAlreadyAttachedError(p)
	}

	return func() { m.detach(key, receiver) }, nil
}

// Detach removes a receiver channel from a pid.
func (m *Mailbox) Detach(p pid.PID) {
	key := p.String()
	m.detach(key, nil)
	m.config.logger.Debug("receiver detached", zap.String("pid", key))
}

// detach removes one receiver incarnation and waits for all deliveries that
// loaded it before the removal. expected is used by Attach's cancellation
// callback so an old callback cannot detach a newer reattachment.
func (m *Mailbox) detach(key string, expected *mailboxReceiver) {
	m.lifecycle.Lock()
	var receiver *mailboxReceiver
	if value, ok := m.receivers.Load(key); ok {
		current, valid := value.(*mailboxReceiver)
		if valid && (expected == nil || current == expected) {
			m.receivers.Delete(key)
			receiver = current
		}
	}
	m.lifecycle.Unlock()
	if receiver == nil {
		return
	}
	receiver.stop()
	drainReceiver(receiver.ch)
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

	// The lifecycle read lock linearizes the receiver snapshot and admission
	// with worker shutdown. It is released before the queue select: a full queue
	// must not prevent Detach or shutdown from canceling the sender.
	m.lifecycle.RLock()
	if m.closed {
		m.lifecycle.RUnlock()
		return m.ctx.Err()
	}
	m.admissions.Add(1)
	targetKey := pkg.Target.String()

	// Capture the attachment incarnation under the same lock as admission. A
	// worker must never look up the target again after Detach/reattach, or a
	// package accepted for an old channel could be delivered to a new one.
	var receiver *mailboxReceiver
	if value, ok := m.receivers.Load(targetKey); ok {
		receiver, _ = value.(*mailboxReceiver)
	}
	job := mailboxJob{pkg: pkg, receiver: receiver}
	defer func() {
		m.lifecycle.Lock()
		m.admissions.Done()
		m.lifecycle.Unlock()
	}()
	m.lifecycle.RUnlock()
	select {
	case m.jobQueues[workerIndex] <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
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
		case job := <-queue:
			m.deliver(job)
		case <-m.ctx.Done():
			m.shutdown()
			return
		}
	}
}

// shutdown closes admission and releases packages still waiting in every
// worker queue. The queues remain open because SendContext may be racing the
// queue send; the admission count makes that race deterministic.
func (m *Mailbox) shutdown() {
	m.lifecycle.Lock()
	if m.closed {
		m.lifecycle.Unlock()
		return
	}
	m.closed = true
	var receivers []*mailboxReceiver
	m.receivers.Range(func(_, value any) bool {
		if receiver, ok := value.(*mailboxReceiver); ok {
			receivers = append(receivers, receiver)
		}
		return true
	})
	m.lifecycle.Unlock()

	// SendContext may have captured an attachment and be waiting for queue
	// capacity. Closed admission prevents new senders from entering; wait for
	// existing senders before draining so every accepted job has a clear owner.
	m.admissions.Wait()
	for _, queue := range m.jobQueues {
	drain:
		for {
			select {
			case job := <-queue:
				api.ReleasePackage(job.pkg)
			default:
				break drain
			}
		}
	}

	// Stop every attachment outside lifecycle so Detach/Attach do not hold the
	// global lock across a potentially blocked channel send. Each receiver's
	// own active counter makes this wait finite once the mailbox context is
	// canceled.
	for _, receiver := range receivers {
		receiver.stop()
		drainReceiver(receiver.ch)
	}
}

// deliver sends the package to the target's receiver channel.
func (m *Mailbox) deliver(job mailboxJob) {
	pkg := job.pkg
	if pkg == nil {
		return
	}
	targetKey := pkg.Target.String()
	receiver := job.receiver
	if receiver == nil {
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

	if !receiver.begin() {
		api.ReleasePackage(pkg)
		return
	}
	defer receiver.end()
	m.deliverTo(receiver, pkg, targetKey)
}

// deliverTo performs the receiver-channel send. The channel is owned by the
// attached process, which may close it concurrently with this send. Detach
// cancels the receiver-local delivery context; a send on a closed channel
// panics, which must never take down the worker, so it is recovered: a closed
// receiver means the process is gone and the package is dropped.
func (m *Mailbox) deliverTo(receiver *mailboxReceiver, pkg *api.Package, targetKey string) {
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
	case receiver.ch <- pkg:
		delivered = true
	case <-receiver.done:
		m.config.logger.Debug("delivery detached",
			zap.String("target", targetKey))
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
