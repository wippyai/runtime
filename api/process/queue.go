// SPDX-License-Identifier: MPL-2.0

package process

import (
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

// todo: move from api
const defaultQueueCap = 16

// EventQueue is a thread-safe MPSC queue for events.
// Multiple producers (handlers via CompleteYield, message senders), single consumer (scheduler).
// Scheduler owns this, not the process.
//
// Generation counter ensures stale senders from previous executions
// cannot push to a reused queue.
type EventQueue struct {
	signal chan struct{}
	// Message accounting is opt-in. Ordinary event traffic keeps the
	// historical unbounded queue semantics; CDC messages carry MaxItems and/or
	// MaxBytes and are admitted through PushMessage.
	messageTopics map[string]*messageTopicState
	events        []Event
	drainBuf      []Event
	generation    atomic.Uint64
	mu            sync.Mutex
	closed        atomic.Bool
}

// messageTopicState is the accounting identity for one bounded topic
// incarnation. It must not be reused after a terminal is drained: a process
// may hold a message reservation past that point while a new stream with the
// same topic is admitted. Per-message leases retain this state until the
// consumer releases the message, so old traffic cannot debit a replacement
// stream's counters.
type messageTopicState struct {
	items      atomic.Int64
	bytes      atomic.Int64
	maxItems   int64
	maxBytes   int64
	overflowed bool
}

// messageRetentionLease transfers one EventQueue reservation to the process
// mailbox. Release is idempotent because either the process or relay's pooled
// package cleanup may be the first owner to finish the handoff.
type messageRetentionLease struct {
	state *messageTopicState
	items int64
	bytes int64
	once  sync.Once
}

func (l *messageRetentionLease) Release() {
	if l == nil || l.state == nil {
		return
	}
	l.once.Do(func() {
		if l.items > 0 {
			l.state.items.Add(-l.items)
		}
		if l.bytes > 0 {
			l.state.bytes.Add(-l.bytes)
		}
	})
}

// MessageAdmission describes ownership after PushMessage.
//
// Accepted means the queue owns the package. Dropped means the queue emitted
// its overflow terminal but retained no part of the supplied package, so the
// caller must release it. Rejected means the queue did not admit the package
// (closed or stale generation), and the caller must release it as well.
type MessageAdmission uint8

const (
	MessageRejected MessageAdmission = iota
	MessageDropped
	MessageAccepted
)

// NewEventQueue creates a queue with default capacity.
func NewEventQueue() *EventQueue {
	q := &EventQueue{
		events:   make([]Event, 0, defaultQueueCap),
		drainBuf: make([]Event, 0, defaultQueueCap),
		signal:   make(chan struct{}, 1),
	}
	q.generation.Store(1)
	return q
}

// Generation returns current generation for sender binding.
func (q *EventQueue) Generation() uint64 {
	return q.generation.Load()
}

// Push adds an event if queue is open and generation matches.
// Returns false if queue is closed or generation mismatch (stale sender).
func (q *EventQueue) Push(e Event, gen uint64) bool {
	// Fast path: check generation and closed without lock
	if q.generation.Load() != gen {
		return false
	}
	if q.closed.Load() {
		return false
	}

	q.mu.Lock()
	// Recheck under lock
	if q.generation.Load() != gen || q.closed.Load() {
		q.mu.Unlock()
		return false
	}
	q.events = append(q.events, e)
	q.mu.Unlock()

	q.signalPush()
	return true
}

func (q *EventQueue) signalPush() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// PushMessage admits a relay package while enforcing the per-topic limits
// carried by its messages. A package can contain messages for more than one
// topic; each message is admitted independently and the package is compacted
// before ownership transfers to the queue. On the first overflow for a topic,
// one synthetic error+terminal message is appended in its position. Later
// traffic for that topic is discarded until Reset.
//
// The queue owns an accepted package and the scheduler releases it after
// processing. The caller owns rejected or fully dropped packages.
func (q *EventQueue) PushMessage(e Event, gen uint64) MessageAdmission {
	if e.Type != EventMessage {
		if q.Push(e, gen) {
			return MessageAccepted
		}
		return MessageRejected
	}
	pkg, ok := e.Data.(*relay.Package)
	if !ok || pkg == nil {
		if q.Push(e, gen) {
			return MessageAccepted
		}
		return MessageRejected
	}

	if q.generation.Load() != gen || q.closed.Load() {
		return MessageRejected
	}

	q.mu.Lock()
	if q.generation.Load() != gen || q.closed.Load() {
		q.mu.Unlock()
		return MessageRejected
	}
	accepted := q.admitPackageLocked(pkg)
	if !accepted {
		q.mu.Unlock()
		return MessageDropped
	}
	q.events = append(q.events, e)
	q.mu.Unlock()

	q.signalPush()
	return MessageAccepted
}

func (q *EventQueue) admitPackageLocked(pkg *relay.Package) bool {
	original := pkg.Messages
	if len(original) == 0 {
		return true
	}

	accepted := make([]*relay.Message, 0, len(original)+1)
	for _, msg := range original {
		if msg == nil {
			accepted = append(accepted, nil)
			continue
		}

		topic := msg.Topic
		maxItems := msg.MaxItems
		maxBytes := msg.MaxBytes
		state := q.messageTopics[topic]
		if state != nil {
			if maxItems <= 0 {
				maxItems = int(state.maxItems)
			} else if state.maxItems > 0 && state.maxItems < int64(maxItems) {
				maxItems = int(state.maxItems)
			}
			if maxBytes <= 0 {
				maxBytes = state.maxBytes
			} else if state.maxBytes > 0 && state.maxBytes < maxBytes {
				maxBytes = state.maxBytes
			}
		}
		if maxItems > 0 || maxBytes > 0 {
			if state == nil {
				if q.messageTopics == nil {
					q.messageTopics = make(map[string]*messageTopicState)
				}
				state = &messageTopicState{}
				q.messageTopics[topic] = state
			}
			if state.maxItems <= 0 || (maxItems > 0 && int64(maxItems) < state.maxItems) {
				state.maxItems = int64(maxItems)
			}
			if state.maxBytes <= 0 || (maxBytes > 0 && maxBytes < state.maxBytes) {
				state.maxBytes = maxBytes
			}
			maxItems = int(state.maxItems)
			maxBytes = state.maxBytes
		}

		if state != nil && state.overflowed {
			relay.ReleaseMessage(msg)
			continue
		}

		// A terminal never consumes backlog capacity. This also makes the
		// synthetic overflow terminal admissible after a full backlog.
		if !messageHasData(msg) {
			if maxItems > 0 {
				msg.MaxItems = maxItems
			}
			if maxBytes > 0 {
				msg.MaxBytes = maxBytes
			}
			accepted = append(accepted, msg)
			continue
		}

		payloadBytes := msg.PayloadBytes
		if maxBytes > 0 && payloadBytes <= 0 {
			// Missing size metadata must not bypass a byte budget.
			payloadBytes = maxBytes
		}
		var items, bytes int64
		if state != nil {
			items = state.items.Load()
			bytes = state.bytes.Load()
		}
		if (maxItems > 0 && items >= int64(maxItems)) ||
			(maxBytes > 0 && (payloadBytes > maxBytes || bytes > maxBytes-payloadBytes)) {
			accepted = q.messageOverflowedLocked(state, topic, accepted, maxItems, maxBytes)
			relay.ReleaseMessage(msg)
			continue
		}

		msg.MaxItems = maxItems
		msg.MaxBytes = maxBytes
		msg.PayloadBytes = payloadBytes
		if state != nil {
			if maxItems > 0 {
				state.items.Add(1)
			}
			if maxBytes > 0 && payloadBytes > 0 {
				state.bytes.Add(payloadBytes)
			}
			reservationBytes := int64(0)
			if maxBytes > 0 {
				reservationBytes = payloadBytes
			}
			msg.SetRetentionLease(&messageRetentionLease{
				state: state,
				items: boolInt64(maxItems > 0),
				bytes: reservationBytes,
			})
		}
		accepted = append(accepted, msg)
	}

	pkg.Messages = accepted
	return len(accepted) > 0
}

func boolInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func (q *EventQueue) messageOverflowedLocked(state *messageTopicState, topic string, accepted []*relay.Message, maxItems int, maxBytes int64) []*relay.Message {
	if state == nil {
		if q.messageTopics == nil {
			q.messageTopics = make(map[string]*messageTopicState)
		}
		state = &messageTopicState{maxItems: int64(maxItems), maxBytes: maxBytes}
		q.messageTopics[topic] = state
	}
	if state.overflowed {
		return accepted
	}
	state.overflowed = true
	msg := relay.AcquireMessage()
	msg.Topic = topic
	msg.Payloads = payload.Payloads{payload.NewError(ErrMessageQueueOverflow), payload.NewTerminal()}
	msg.MaxItems = maxItems
	msg.MaxBytes = maxBytes
	msg.PayloadBytes = 0
	return append(accepted, msg)
}

func messageHasTerminal(msg *relay.Message) bool {
	if msg == nil {
		return false
	}
	for _, pl := range msg.Payloads {
		if pl != nil && payload.IsTerminal(pl) {
			return true
		}
	}
	return false
}

func messageHasData(msg *relay.Message) bool {
	if msg == nil {
		return false
	}
	for _, pl := range msg.Payloads {
		if pl == nil || payload.IsTerminal(pl) || pl.Format() == payload.GoError {
			continue
		}
		return true
	}
	return false
}

// PushDirect adds an event without generation check (for scheduler's own use).
// It returns false when the queue is closed. A rejected message package is
// released here because PushDirect is an ownership-taking scheduler path; a
// successful message remains owned by the queue/process as usual.
func (q *EventQueue) PushDirect(e Event) bool {
	q.mu.Lock()
	if q.closed.Load() {
		q.mu.Unlock()
		if e.Type == EventMessage {
			if pkg, ok := e.Data.(*relay.Package); ok {
				relay.ReleasePackage(pkg)
			}
		}
		return false
	}
	q.events = append(q.events, e)
	q.mu.Unlock()

	q.signalPush()
	return true
}

// Drain returns all pending events and clears the queue.
// Returns the same slice on each call (reused buffer) - caller must not retain.
// Single consumer only (scheduler).
func (q *EventQueue) Drain() []Event {
	q.mu.Lock()
	// The previous drain result is caller-owned. The single-consumer contract
	// means the caller has finished with it before asking for the next batch;
	// clear it now so the queue does not retain arbitrary Data values.
	for i := range q.drainBuf {
		q.drainBuf[i] = Event{}
	}
	q.drainBuf = q.drainBuf[:0]
	if len(q.events) == 0 {
		q.mu.Unlock()
		return nil
	}
	for _, event := range q.events {
		q.retireEventTopicsLocked(event)
	}

	// Swap buffers to avoid allocation
	q.drainBuf, q.events = q.events, q.drainBuf[:0]
	result := q.drainBuf
	q.mu.Unlock()

	return result
}

// HasEvents returns true if there are pending events.
func (q *EventQueue) HasEvents() bool {
	q.mu.Lock()
	n := len(q.events)
	q.mu.Unlock()
	return n > 0
}

// Signal returns channel for select. Signaled when events arrive.
func (q *EventQueue) Signal() <-chan struct{} {
	return q.signal
}

// Close marks queue as closed. Push will return false after this.
func (q *EventQueue) Close() {
	q.mu.Lock()
	q.closed.Store(true)
	for i, event := range q.events {
		q.retireEventTopicsLocked(event)
		q.releaseEventPackageLocked(event)
		q.events[i] = Event{}
	}
	q.events = q.events[:0]
	q.clearMessageAccountingLocked()
	// A drained batch belongs to the scheduler. Drop the queue's reference
	// without mutating the caller's slice, which may still be in flight while
	// Close is called by a supervisor.
	q.drainBuf = nil
	q.mu.Unlock()

	// Wake any waiters
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// Reset clears queue for reuse. Bumps generation to invalidate stale senders.
func (q *EventQueue) Reset() {
	q.mu.Lock()
	q.generation.Add(1) // Invalidate all existing senders
	q.closed.Store(false)
	for i, event := range q.events {
		q.retireEventTopicsLocked(event)
		q.releaseEventPackageLocked(event)
		q.events[i] = Event{}
	}
	q.events = q.events[:0]
	// See Close: the previous Drain result is consumer-owned. Detach it
	// rather than touching a potentially concurrent scheduler slice.
	q.drainBuf = nil
	q.clearMessageAccountingLocked()
	q.mu.Unlock()

	// Drain signal channel
	select {
	case <-q.signal:
	default:
	}
}

func (q *EventQueue) retireEventTopicsLocked(event Event) {
	if event.Type != EventMessage {
		return
	}
	pkg, ok := event.Data.(*relay.Package)
	if !ok || pkg == nil {
		return
	}
	for _, msg := range pkg.Messages {
		if !messageHasTerminal(msg) {
			continue
		}
		topic := msg.Topic
		delete(q.messageTopics, topic)
	}
}

func (q *EventQueue) releaseEventPackageLocked(event Event) {
	if event.Type != EventMessage {
		return
	}
	if pkg, ok := event.Data.(*relay.Package); ok {
		relay.ReleasePackage(pkg)
	}
}

func (q *EventQueue) clearMessageAccountingLocked() {
	clear(q.messageTopics)
}

// YieldScheduler is the subset of Scheduler needed for waking.
type YieldScheduler interface {
	WakeProcessor(q *EventQueue, gen uint64)
}

// YieldCompleter delivers yield completion results to a queue.
// Bound to a specific generation - stale completers silently no-op.
// This breaks the direct reference to Processor, preventing races
// when processor is released to pool while handler goroutines are still running.
type YieldCompleter struct {
	scheduler YieldScheduler
	queue     *EventQueue
	gen       uint64
}

// NewYieldCompleter creates a completer bound to current queue generation.
func (q *EventQueue) NewYieldCompleter(sched YieldScheduler) *YieldCompleter {
	return &YieldCompleter{
		queue:     q,
		gen:       q.generation.Load(),
		scheduler: sched,
	}
}

// CompleteYield implements dispatcher.ResultReceiver.
// Safe to call from any goroutine - uses generation to detect staleness.
func (c *YieldCompleter) CompleteYield(tag uint64, data any, err error) {
	if !c.queue.Push(Event{
		Type:  EventYieldComplete,
		Tag:   tag,
		Data:  data,
		Error: err,
	}, c.gen) {
		return
	}

	// Wake processor if waiting
	if c.scheduler != nil {
		c.scheduler.WakeProcessor(c.queue, c.gen)
	}
}
