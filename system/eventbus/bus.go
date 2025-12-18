package eventbus

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/event"
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

type action struct {
	kind        actKind
	subscribe   *subscribeRequest
	unsubscribe *unsubscribeRequest
	// Inline send event fields to avoid allocation
	event event.Event
	ctx   context.Context
}

type subscribeRequest struct {
	sub    sub
	doneCh chan error
}

type unsubscribeRequest struct {
	subID  event.SubscriberID
	doneCh chan struct{}
}

type sub struct {
	subID   event.SubscriberID
	ctx     context.Context
	system  *wildcard.Wildcard
	kind    *wildcard.Wildcard
	eventCh chan<- event.Event
}

// Bus is an event bus that handles pub/sub message distribution with support for
// system and kind filtering using wildcards. It provides thread-safe operations
// for subscribing, unsubscribing, and sending events.
type Bus struct {
	subscribers       map[event.SubscriberID]sub
	subscriberCounter uint64

	// Slice-based queue with swap for zero-alloc steady state
	actionQueue []action
	spareQueue  []action // reused after processing
	actionMu    sync.Mutex
	actionReady chan struct{} // Signal that actions are available

	wg     sync.WaitGroup
	closed atomic.Bool
}

// NewBus creates a new event bus instance.
func NewBus() *Bus {
	b := &Bus{
		subscribers: make(map[event.SubscriberID]sub),
		actionQueue: make([]action, 0, defaultQueueCap),
		spareQueue:  make([]action, 0, defaultQueueCap),
		actionReady: make(chan struct{}, 1), // Buffered so signal never blocks
	}

	b.wg.Add(1)
	go b.dispatcher()

	return b
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
		return "", errors.New("nil channel provided")
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

	// Wait for response
	select {
	case err := <-req.doneCh:
		return subID, err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Unsubscribe removes the subscription identified by the given subscriber ID.
func (b *Bus) Unsubscribe(ctx context.Context, subID event.SubscriberID) {
	if ctx.Err() != nil {
		return
	}

	req := &unsubscribeRequest{
		subID:  subID,
		doneCh: make(chan struct{}, 1),
	}

	// Enqueue unsubscribe request (ignore error, already closed)
	_ = b.enqueueAction(action{
		kind:        actUnsubscribe,
		unsubscribe: req,
	})

	// Wait for response
	select {
	case <-req.doneCh:
	case <-ctx.Done():
	}
}

// Send publishes an event to all matching subscribers.
// This is guaranteed to never block and never lose messages.
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
		return // Already closed
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
		// Respond to control operations immediately
		switch a.kind {
		case actSubscribe:
			a.subscribe.doneCh <- errors.New("bus is closed")
		case actUnsubscribe:
			a.unsubscribe.doneCh <- struct{}{}
		case actSend:
			// Silently drop send operations when closed
		case actStop:
			// Should not happen, but handle gracefully
		}
		return errors.New("bus is closed")
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
			b.subscribers[a.subscribe.sub.subID] = a.subscribe.sub
			a.subscribe.doneCh <- nil

		case actUnsubscribe:
			delete(b.subscribers, a.unsubscribe.subID)
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
			a.subscribe.doneCh <- errors.New("bus is closed")
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
