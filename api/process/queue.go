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
	messageItems      map[string]int
	messageBytes      map[string]int64
	messageItemLimits map[string]int
	messageByteLimits map[string]int64
	messageOverflowed map[string]struct{}
	events            []Event
	drainBuf          []Event
	generation        atomic.Uint64
	mu                sync.Mutex
	closed            atomic.Bool
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
		if maxItems <= 0 {
			maxItems = q.messageItemLimits[topic]
		}
		maxBytes := msg.MaxBytes
		if maxBytes <= 0 {
			maxBytes = q.messageByteLimits[topic]
		}
		if maxItems > 0 {
			if previous := q.messageItemLimits[topic]; previous > 0 && previous < maxItems {
				maxItems = previous
			}
			if q.messageItemLimits == nil {
				q.messageItemLimits = make(map[string]int)
			}
			q.messageItemLimits[topic] = maxItems
		}
		if maxBytes > 0 {
			if previous := q.messageByteLimits[topic]; previous > 0 && previous < maxBytes {
				maxBytes = previous
			}
			if q.messageByteLimits == nil {
				q.messageByteLimits = make(map[string]int64)
			}
			q.messageByteLimits[topic] = maxBytes
		}

		if _, overflowed := q.messageOverflowed[topic]; overflowed {
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
		items := q.messageItems[topic]
		bytes := q.messageBytes[topic]
		if (maxItems > 0 && items >= maxItems) ||
			(maxBytes > 0 && (payloadBytes > maxBytes || bytes > maxBytes-payloadBytes)) {
			accepted = q.messageOverflowedLocked(topic, accepted, maxItems, maxBytes)
			relay.ReleaseMessage(msg)
			continue
		}

		msg.MaxItems = maxItems
		msg.MaxBytes = maxBytes
		msg.PayloadBytes = payloadBytes
		if maxItems > 0 {
			if q.messageItems == nil {
				q.messageItems = make(map[string]int)
			}
			q.messageItems[topic] = items + 1
		}
		if maxBytes > 0 && payloadBytes > 0 {
			if q.messageBytes == nil {
				q.messageBytes = make(map[string]int64)
			}
			q.messageBytes[topic] = bytes + payloadBytes
		}
		accepted = append(accepted, msg)
	}

	pkg.Messages = accepted
	return len(accepted) > 0
}

func (q *EventQueue) messageOverflowedLocked(topic string, accepted []*relay.Message, maxItems int, maxBytes int64) []*relay.Message {
	if q.messageOverflowed == nil {
		q.messageOverflowed = make(map[string]struct{})
	}
	if _, exists := q.messageOverflowed[topic]; exists {
		return accepted
	}
	q.messageOverflowed[topic] = struct{}{}
	msg := relay.AcquireMessage()
	msg.Topic = topic
	msg.Payloads = payload.Payloads{payload.NewError(ErrMessageQueueOverflow), payload.NewTerminal()}
	msg.MaxItems = maxItems
	msg.MaxBytes = maxBytes
	msg.PayloadBytes = 0
	return append(accepted, msg)
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
func (q *EventQueue) PushDirect(e Event) {
	q.mu.Lock()
	q.events = append(q.events, e)
	q.mu.Unlock()

	q.signalPush()
}

// Drain returns all pending events and clears the queue.
// Returns the same slice on each call (reused buffer) - caller must not retain.
// Single consumer only (scheduler).
func (q *EventQueue) Drain() []Event {
	q.mu.Lock()
	if len(q.events) == 0 {
		q.mu.Unlock()
		return nil
	}
	for _, event := range q.events {
		q.releaseEventLocked(event)
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
	for _, event := range q.events {
		q.releaseEventLocked(event)
		q.releaseEventPackageLocked(event)
	}
	q.events = q.events[:0]
	q.clearMessageAccountingLocked()
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
	for _, event := range q.events {
		q.releaseEventPackageLocked(event)
	}
	q.events = q.events[:0]
	q.drainBuf = q.drainBuf[:0]
	q.clearMessageAccountingLocked()
	q.mu.Unlock()

	// Drain signal channel
	select {
	case <-q.signal:
	default:
	}
}

func (q *EventQueue) releaseEventLocked(event Event) {
	if event.Type != EventMessage {
		return
	}
	pkg, ok := event.Data.(*relay.Package)
	if !ok || pkg == nil {
		return
	}
	for _, msg := range pkg.Messages {
		if !messageHasData(msg) {
			continue
		}
		topic := msg.Topic
		if msg.MaxItems > 0 && q.messageItems != nil {
			if n := q.messageItems[topic] - 1; n > 0 {
				q.messageItems[topic] = n
			} else {
				delete(q.messageItems, topic)
			}
		}
		if msg.MaxBytes > 0 && msg.PayloadBytes > 0 && q.messageBytes != nil {
			if n := q.messageBytes[topic] - msg.PayloadBytes; n > 0 {
				q.messageBytes[topic] = n
			} else {
				delete(q.messageBytes, topic)
			}
		}
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
	clear(q.messageItems)
	clear(q.messageBytes)
	clear(q.messageItemLimits)
	clear(q.messageByteLimits)
	clear(q.messageOverflowed)
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
