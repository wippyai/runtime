// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/wildcard"
)

type actKind int

const (
	actSubscribe actKind = iota
	actUnsubscribe
	actSend
	actStop
)

const defaultQueueCap = 64
const subscriberChanBuffer = 16

// DefaultMaxSubscribers caps the total active subscriber map size.
// Observed peak under chaos before the cap was ~600; 4096 leaves 6×
// headroom and is far below the 50k+ that would matter for OOM. A
// caller hitting this cap is almost certainly leaking subscriptions
// (forgetting Unsubscribe / Close), and the cap is meant to surface
// that bug fast rather than silently blow the heap.
const DefaultMaxSubscribers = 4096

type action struct {
	ctx         context.Context
	subscribe   *subscribeRequest
	unsubscribe *unsubscribeRequest
	event       event.Event
	kind        actKind
}

type subscribeRequest struct {
	doneCh chan error
	sub    sub
}

type unsubscribeRequest struct {
	doneCh chan struct{}
	subID  event.SubscriberID
}

type sub struct {
	ctx     context.Context
	system  *wildcard.Wildcard
	kind    *wildcard.Wildcard
	eventCh chan<- event.Event
	subID   event.SubscriberID
}

// Bus is an event bus that handles pub/sub message distribution with support for
// system and kind filtering using wildcards. It provides thread-safe operations
// for subscribing, unsubscribing, and sending events.
type Bus struct {
	subscribers       map[event.SubscriberID]sub
	actionReady       chan struct{}
	collector         atomic.Pointer[metrics.Collector]
	actionQueue       []action
	spareQueue        []action
	wg                sync.WaitGroup
	subscriberCounter uint64
	maxSubscribers    int
	actionMu          sync.Mutex
	closed            atomic.Bool
}

// NewBus creates a new event bus instance.
func NewBus() *Bus {
	b := &Bus{
		subscribers:    make(map[event.SubscriberID]sub),
		actionQueue:    make([]action, 0, defaultQueueCap),
		spareQueue:     make([]action, 0, defaultQueueCap),
		actionReady:    make(chan struct{}, 1), // Buffered so signal never blocks
		maxSubscribers: DefaultMaxSubscribers,
	}

	b.wg.Add(1)
	go b.dispatcher()

	return b
}

// SetCollector binds a metrics collector for telemetry. Called by the
// metrics boot component after the collector becomes available; until
// then telemetry is a no-op. Safe to call concurrently with bus
// operations; reads are atomic loads in the dispatcher hot path.
//
// Bootstraps the active-count gauge and rejection counter at zero so
// the F5 expected-series gate sees them on a fresh boot, even if no
// subscribe has happened yet. The dispatcher will overwrite the gauge
// on the next subscribe/unsubscribe.
func (b *Bus) SetCollector(c metrics.Collector) {
	if c == nil {
		b.collector.Store(nil)
		return
	}
	b.collector.Store(&c)
	c.GaugeSet("eventbus_subscribers_active", 0, nil)
	c.CounterAdd("eventbus_subscribers_rejected_total", 0, metrics.Labels{"reason": "cap"})
}

// recordSubscribers writes the current active count as a gauge.
func (b *Bus) recordSubscribers() {
	cp := b.collector.Load()
	if cp == nil {
		return
	}
	(*cp).GaugeSet("eventbus_subscribers_active", float64(len(b.subscribers)), nil)
}

// recordRejection bumps the rejection counter when a subscribe is
// dropped because the cap was exceeded. Soak gates on this >0/s.
func (b *Bus) recordRejection(reason string) {
	cp := b.collector.Load()
	if cp == nil {
		return
	}
	(*cp).CounterInc("eventbus_subscribers_rejected_total", metrics.Labels{"reason": reason})
}

// Subscribe creates a new subscription for events from the specified system.
func (b *Bus) Subscribe(
	ctx context.Context,
	system event.System,
	ch chan<- event.Event,
) (event.SubscriberID, error) {
	return b.SubscribeP(ctx, system, "", ch)
}

// SubscribeP creates a new subscription for events matching both system and kind filters.
func (b *Bus) SubscribeP(
	ctx context.Context,
	system event.System,
	kind event.Kind,
	ch chan<- event.Event,
) (event.SubscriberID, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if ch == nil {
		return "", ErrNilChannel
	}

	subID := b.generateSubscriberID()
	var w *wildcard.Wildcard
	if kind != "" {
		w = wildcard.NewWildcard(kind)
	}

	var sw *wildcard.Wildcard
	if system != "" {
		sw = wildcard.NewWildcard(system)
	}

	sub := sub{
		subID:   subID,
		ctx:     ctx,
		system:  sw,
		kind:    w,
		eventCh: ch,
	}

	req := &subscribeRequest{
		sub:    sub,
		doneCh: make(chan error, 1),
	}

	// Enqueue subscribe request
	if err := b.enqueueAction(action{
		kind:      actSubscribe,
		subscribe: req,
	}); err != nil {
		return "", err
	}

	// Once enqueued, wait for the dispatcher to decide ownership. Returning on
	// cancellation before that decision could leave an installed subscription
	// whose ID was never returned to the caller.
	if err := <-req.doneCh; err != nil {
		return "", err
	}
	return subID, nil
}

// Unsubscribe removes the subscription identified by the given subscriber ID.
func (b *Bus) Unsubscribe(_ context.Context, subID event.SubscriberID) {
	req := &unsubscribeRequest{
		subID:  subID,
		doneCh: make(chan struct{}, 1),
	}

	// Enqueue unsubscribe request (ignore error, already closed)
	_ = b.enqueueAction(action{
		kind:        actUnsubscribe,
		unsubscribe: req,
	})

	// Unsubscribe is an ownership barrier. Returning early on cancellation
	// would let the caller release the channel while the dispatcher can still
	// hold an in-flight send reference.
	<-req.doneCh
}

// Send publishes an event to all matching subscribers without blocking the
// caller on delivery. Events are delivered in order while the publisher and
// subscriber contexts remain active; cancellation may abort queued or
// in-progress delivery. Calls made after Stop are ignored.
func (b *Bus) Send(ctx context.Context, e event.Event) {
	if ctx.Err() != nil {
		return
	}

	// Enqueue send event (ignore error if closed)
	_ = b.enqueueAction(action{
		kind:  actSend,
		event: e,
		ctx:   ctx,
	})
}

// Stop gracefully shuts down the event bus.
func (b *Bus) Stop() {
	// Atomically set closed and enqueue stop action
	b.actionMu.Lock()
	if b.closed.Swap(true) {
		b.actionMu.Unlock()
		// A concurrent Stop may still be draining the dispatcher. Stop is a
		// terminal barrier, not merely an idempotent state flip.
		b.wg.Wait()
		return
	}
	b.actionQueue = append(b.actionQueue, action{
		kind: actStop,
	})
	b.actionMu.Unlock()

	// Signal dispatcher
	select {
	case b.actionReady <- struct{}{}:
	default:
	}

	b.wg.Wait()
}

// enqueueAction adds an action to the queue and signals the dispatcher.
// Returns error if bus is closed.
func (b *Bus) enqueueAction(a action) error {
	b.actionMu.Lock()

	if b.closed.Load() {
		b.actionMu.Unlock()
		// Resolve control operations according to the terminal barrier.
		switch a.kind {
		case actSubscribe:
			a.subscribe.doneCh <- ErrBusClosed
		case actUnsubscribe:
			// Stop may have marked the bus closed while the dispatcher is
			// still delivering a previously drained send batch.  An
			// unsubscribe acknowledgement is also permission for helpers to
			// release their delivery channel, so do not acknowledge it until
			// the sole sender has exited.
			b.wg.Wait()
			a.unsubscribe.doneCh <- struct{}{}
		case actSend:
			// Silently drop send operations when closed
		case actStop:
			// Should not happen, but handle gracefully
		}
		return ErrBusClosed
	}

	b.actionQueue = append(b.actionQueue, a)
	b.actionMu.Unlock()

	// Signal dispatcher (non-blocking due to buffered channel)
	select {
	case b.actionReady <- struct{}{}:
	default:
		// Signal already pending, dispatcher will process all queued actions
	}

	return nil
}

// dispatcher is the main event loop that processes all operations
func (b *Bus) dispatcher() {
	defer b.wg.Done()

	for {
		<-b.actionReady

		if !b.processActions() {
			return // Stop requested
		}
	}
}

// processActions drains the action queue and processes all actions
// Returns false if stop was requested
func (b *Bus) processActions() bool {
	// Swap queues atomically - reuse spare to avoid allocation
	b.actionMu.Lock()
	if len(b.actionQueue) == 0 {
		b.actionMu.Unlock()
		return true
	}
	actions := b.actionQueue
	b.actionQueue = b.spareQueue[:0] // reuse spare capacity
	b.spareQueue = nil               // will be set after processing
	b.actionMu.Unlock()

	// Process all actions
	for i := range actions {
		a := actions[i]

		switch a.kind {
		case actSubscribe:
			// Cancellation before the serialized ownership decision means the
			// bus never takes ownership of the caller's channel.
			if err := a.subscribe.sub.ctx.Err(); err != nil {
				a.subscribe.doneCh <- err
				continue
			}
			if b.maxSubscribers > 0 && len(b.subscribers) >= b.maxSubscribers {
				// Cap reached. The metric+counter let the soak gate
				// catch a runaway leak; the typed error gives the
				// caller something to retry/back off on.
				b.recordRejection("cap")
				a.subscribe.doneCh <- ErrSubscribersCapReached
				continue
			}
			b.subscribers[a.subscribe.sub.subID] = a.subscribe.sub
			b.recordSubscribers()
			a.subscribe.doneCh <- nil

		case actUnsubscribe:
			if _, ok := b.subscribers[a.unsubscribe.subID]; ok {
				delete(b.subscribers, a.unsubscribe.subID)
				b.recordSubscribers()
			}
			a.unsubscribe.doneCh <- struct{}{}

		case actSend:
			if a.ctx.Err() != nil {
				continue
			}

			var expiredSubs []event.SubscriberID

			for id, s := range b.subscribers {
				// Check filters
				if s.system != nil && !s.system.Match(a.event.System) {
					continue
				}
				if s.kind != nil && !s.kind.Match(a.event.Kind) {
					continue
				}

				// Check contexts and deliver
				select {
				case <-a.ctx.Done():
					goto cleanup
				case <-s.ctx.Done():
					expiredSubs = append(expiredSubs, id)
					continue
				case s.eventCh <- a.event:
				}
			}

		cleanup:
			// Clean up expired subscribers
			for _, id := range expiredSubs {
				delete(b.subscribers, id)
			}
			if len(expiredSubs) > 0 {
				b.recordSubscribers()
			}

		case actStop:
			// Clean up all subscribers
			b.subscribers = make(map[event.SubscriberID]sub)

			// Clear references to prevent memory leaks
			clear(actions)

			// Drain remaining actions and reject any control requests
			b.drainQueue()

			return false // Signal to exit dispatcher
		}
	}

	// Clear references to prevent memory leaks, then recycle slice
	clear(actions)
	b.actionMu.Lock()
	b.spareQueue = actions[:0]
	b.actionMu.Unlock()

	return true
}

// drainQueue processes remaining actions after stop, rejecting control operations
func (b *Bus) drainQueue() {
	b.actionMu.Lock()
	remaining := b.actionQueue
	b.actionQueue = nil
	b.spareQueue = nil
	b.actionMu.Unlock()

	// Process remaining actions
	for i := range remaining {
		a := remaining[i]

		switch a.kind {
		case actSubscribe:
			// Reject with error
			a.subscribe.doneCh <- ErrBusClosed
		case actUnsubscribe:
			// Acknowledge unsubscribe (no-op since we're stopping)
			a.unsubscribe.doneCh <- struct{}{}
		case actSend:
			// Drop send events during shutdown
		case actStop:
			// Ignore additional stop actions
		}
	}
}

func (b *Bus) generateSubscriberID() event.SubscriberID {
	return "sub." + strconv.FormatUint(atomic.AddUint64(&b.subscriberCounter, 1), 10)
}
