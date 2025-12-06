package process

import (
	"sync"
	"sync/atomic"
)

const defaultQueueCap = 16

// EventQueue is a thread-safe MPSC queue for events.
// Multiple producers (handlers via CompleteYield, message senders), single consumer (scheduler).
// Scheduler owns this, not the process.
//
// Generation counter ensures stale senders from previous executions
// cannot push to a reused queue.
type EventQueue struct {
	mu         sync.Mutex
	events     []Event
	drainBuf   []Event
	signal     chan struct{}
	closed     atomic.Bool
	generation atomic.Uint64
}

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

	// Non-blocking signal
	select {
	case q.signal <- struct{}{}:
	default:
	}
	return true
}

// PushDirect adds an event without generation check (for scheduler's own use).
func (q *EventQueue) PushDirect(e Event) {
	q.mu.Lock()
	q.events = append(q.events, e)
	q.mu.Unlock()

	select {
	case q.signal <- struct{}{}:
	default:
	}
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
	q.events = q.events[:0]
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
	q.events = q.events[:0]
	q.drainBuf = q.drainBuf[:0]
	q.mu.Unlock()

	// Drain signal channel
	select {
	case <-q.signal:
	default:
	}
}

// MessageSender sends messages to the queue.
// Bound to a specific generation for safety.
type MessageSender struct {
	queue *EventQueue
	gen   uint64
}

// NewMessageSender creates a sender bound to current queue generation.
func (q *EventQueue) NewMessageSender() *MessageSender {
	return &MessageSender{
		queue: q,
		gen:   q.generation.Load(),
	}
}

// Send pushes a message event to queue.
// Returns false if queue was reset or closed.
func (s *MessageSender) Send(data any) bool {
	return s.queue.Push(Event{
		Type: EventMessage,
		Data: data,
	}, s.gen)
}
