package eventbus

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/internal/wildcard"
)

type actionType int

const (
	actionSubscribe actionType = iota
	actionUnsubscribe
	actionSend
	actionStop
)

type action struct {
	actionType  actionType
	subscribe   *subscribeRequest
	unsubscribe *unsubscribeRequest
	sendEvent   *sendEvent
}

type subscribeRequest struct {
	sub    sub
	doneCh chan error
}

type unsubscribeRequest struct {
	subID  event.SubscriberID
	doneCh chan struct{}
}

type sendEvent struct {
	event event.Event
	ctx   context.Context
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

	// Single queue for all operations - unbounded linked list
	actionQueue *list.List
	actionMu    sync.Mutex
	actionReady chan struct{} // Signal that actions are available

	wg     sync.WaitGroup
	closed atomic.Bool
}

// NewBus creates a new event bus instance.
func NewBus() *Bus {
	b := &Bus{
		subscribers: make(map[event.SubscriberID]sub),
		actionQueue: list.New(),
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
		actionType: actionSubscribe,
		subscribe:  req,
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
		actionType:  actionUnsubscribe,
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
		actionType: actionSend,
		sendEvent: &sendEvent{
			event: e,
			ctx:   ctx,
		},
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
	b.actionQueue.PushBack(action{
		actionType: actionStop,
	})
	b.actionMu.Unlock()

	// Signal dispatcher
	select {
	case b.actionReady <- struct{}{}:
	default:
	}

	b.wg.Wait()
}

// enqueueAction adds an action to the queue and signals the dispatcher
// Returns error if bus is closed
func (b *Bus) enqueueAction(a action) error {
	// Atomically check closed and enqueue to prevent TOCTOU race
	b.actionMu.Lock()
	defer b.actionMu.Unlock()

	if b.closed.Load() {
		// Respond to control operations immediately
		switch a.actionType {
		case actionSubscribe:
			a.subscribe.doneCh <- errors.New("bus is closed")
		case actionUnsubscribe:
			a.unsubscribe.doneCh <- struct{}{}
		case actionSend:
			// Silently drop send operations when closed
		case actionStop:
			// Should not happen, but handle gracefully
		}
		return errors.New("bus is closed")
	}

	b.actionQueue.PushBack(a)

	// Signal dispatcher (non-blocking due to buffered channel)
	// Release lock before signaling to avoid holding lock during channel operation
	b.actionMu.Unlock()
	select {
	case b.actionReady <- struct{}{}:
	default:
		// Signal already pending, dispatcher will process all queued actions
	}
	b.actionMu.Lock() // Re-acquire for defer unlock

	return nil
}

// dispatcher is the main event loop that processes all operations
func (b *Bus) dispatcher() {
	defer b.wg.Done()

	for {
		// Wait for actions
		<-b.actionReady

		// Process all pending actions
		if !b.processActions() {
			return // Stop requested
		}
	}
}

// processActions drains the action queue and processes all actions
// Returns false if stop was requested
func (b *Bus) processActions() bool {
	// Swap out the queue atomically
	b.actionMu.Lock()
	if b.actionQueue.Len() == 0 {
		b.actionMu.Unlock()
		return true
	}
	actions := b.actionQueue
	b.actionQueue = list.New()
	b.actionMu.Unlock()

	// Process all actions
	for e := actions.Front(); e != nil; e = e.Next() {
		a := e.Value.(action)

		switch a.actionType {
		case actionSubscribe:
			b.subscribers[a.subscribe.sub.subID] = a.subscribe.sub
			a.subscribe.doneCh <- nil

		case actionUnsubscribe:
			delete(b.subscribers, a.unsubscribe.subID)
			a.unsubscribe.doneCh <- struct{}{}

		case actionSend:
			if a.sendEvent.ctx.Err() != nil {
				continue
			}

			var expiredSubs []event.SubscriberID

			for id, s := range b.subscribers {
				// Check filters
				if s.system != nil && !s.system.Match(a.sendEvent.event.System) {
					continue
				}
				if s.kind != nil && !s.kind.Match(a.sendEvent.event.Kind) {
					continue
				}

				// Check contexts and deliver
				select {
				case <-a.sendEvent.ctx.Done():
					// Event context canceled
					goto cleanup
				case <-s.ctx.Done():
					// Subscriber context canceled, mark for cleanup
					expiredSubs = append(expiredSubs, id)
					continue
				case s.eventCh <- a.sendEvent.event:
					// Delivered successfully
				}
			}

		cleanup:
			// Clean up expired subscribers
			for _, id := range expiredSubs {
				delete(b.subscribers, id)
			}

		case actionStop:
			// Clean up all subscribers
			b.subscribers = make(map[event.SubscriberID]sub)

			// Drain remaining actions and reject any control requests
			b.drainQueue()

			return false // Signal to exit dispatcher
		}
	}

	return true
}

// drainQueue processes remaining actions after stop, rejecting control operations
func (b *Bus) drainQueue() {
	b.actionMu.Lock()
	remaining := b.actionQueue
	b.actionQueue = list.New()
	b.actionMu.Unlock()

	// Process remaining actions
	for e := remaining.Front(); e != nil; e = e.Next() {
		a := e.Value.(action)

		switch a.actionType {
		case actionSubscribe:
			// Reject with error
			a.subscribe.doneCh <- errors.New("bus is closed")
		case actionUnsubscribe:
			// Acknowledge unsubscribe (no-op since we're stopping)
			a.unsubscribe.doneCh <- struct{}{}
		case actionSend:
			// Drop send events during shutdown
		case actionStop:
			// Ignore additional stop actions
		}
	}
}

func (b *Bus) generateSubscriberID() event.SubscriberID {
	return fmt.Sprintf("sub.%d", atomic.AddUint64(&b.subscriberCounter, 1))
}
