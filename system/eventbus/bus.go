package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/internal/wildcard"
)

type actionType int

const (
	subscribe actionType = iota
	unsubscribe
	send
	stop
)

type action struct {
	actionType  actionType
	subscribe   sub
	unsubscribe unsub
	event       sendEvent
}

type sendEvent struct {
	event events.Event
	ctx   context.Context
}

type sub struct {
	subID   events.SubscriberID
	system  *wildcard.Wildcard
	kind    *wildcard.Wildcard
	eventCh chan<- events.Event
	doneCh  chan bool
}

type unsub struct {
	subID  events.SubscriberID
	doneCh chan bool
}

// Bus is an event bus that handles pub/sub message distribution with support for
// system and kind filtering using wildcards. It provides thread-safe operations
// for subscribing, unsubscribing, and sending events.
type Bus struct {
	subscribers       map[events.SubscriberID]sub
	actions           chan action
	wg                sync.WaitGroup
	subscriberCounter uint64
	closed            chan any
}

// NewBus creates a new event bus instance with the provided logger.
// It initializes internal channels and starts the event handling goroutine.
func NewBus() *Bus {
	b := &Bus{
		subscribers: make(map[events.SubscriberID]sub),
		actions:     make(chan action, 100), // Buffered channel for all actions
		closed:      make(chan any),
	}

	b.wg.Add(1)
	go b.handleActions()

	return b
}

// Subscribe creates a new subscription for events from the specified system.
// It returns a unique subscriber Alias that can be used to unsubscribe later.
func (b *Bus) Subscribe(
	ctx context.Context,
	system events.System,
	ch chan<- events.Event,
) (events.SubscriberID, error) {
	return b.SubscribeP(ctx, system, "", ch)
}

// SubscribeP creates a new subscription for events matching both system and kind filters.
// It supports wildcard patterns in both system and kind parameters.
func (b *Bus) SubscribeP(
	ctx context.Context,
	system events.System,
	kind events.Kind,
	ch chan<- events.Event,
) (events.SubscriberID, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if ch == nil {
		return "", errors.New("nil channel provided")
	}
	subID := b.generateSubscriberID()
	var w *wildcard.Wildcard
	if kind != "" {
		w = wildcard.NewWildcard(string(kind))
	}

	var sw *wildcard.Wildcard
	if system != "" {
		sw = wildcard.NewWildcard(string(system))
	}

	sub := sub{
		subID:   subID,
		system:  sw,
		kind:    w,
		eventCh: ch,
		doneCh:  make(chan bool),
	}

	select {
	case b.actions <- action{actionType: subscribe, subscribe: sub}:
	case <-b.closed:
		return "", errors.New("bus is closed")
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-b.closed:
		return "", errors.New("bus is closed")
	case <-sub.doneCh:
		return subID, nil
	}
}

// Unsubscribe removes the subscription identified by the given subscriber Alias.
// It closes the associated event channel.
func (b *Bus) Unsubscribe(ctx context.Context, subID events.SubscriberID) {
	if ctx.Err() != nil {
		return
	}

	unsub := unsub{
		subID:  subID,
		doneCh: make(chan bool),
	}

	select {
	case b.actions <- action{actionType: unsubscribe, unsubscribe: unsub}:
	case <-b.closed:
	case <-ctx.Done():
	}

	select {
	case <-ctx.Done():
	case <-b.closed:
	case <-unsub.doneCh:
	}
}

// Send publishes an event to all matching subscribers based on their system and kind filters.
// The event delivery is skipped if the context is canceled.
func (b *Bus) Send(ctx context.Context, event events.Event) {
	select {
	case b.actions <- action{actionType: send, event: sendEvent{event: event, ctx: ctx}}:
	case <-b.closed:
	case <-ctx.Done():
	}
}

// todo: add send and block for critical events

// Stop gracefully shuts down the event bus by closing all subscriber channels
// and stopping the event handling goroutine.
func (b *Bus) Stop() {
	select {
	case b.actions <- action{actionType: stop}:
	case <-b.closed:
	}

	b.wg.Wait()
}

func (b *Bus) handleActions() {
	defer b.wg.Done()

	for a := range b.actions {
		select {
		case <-b.closed:
			return
		default:
		}

		switch a.actionType {
		case subscribe:
			b.subscribers[a.subscribe.subID] = a.subscribe
			a.subscribe.doneCh <- true
		case unsubscribe:
			b.handleUnsubscribe(a.unsubscribe.subID)
			a.unsubscribe.doneCh <- true
		case send:
			if a.event.ctx.Err() != nil {
				continue
			}

			for _, s := range b.subscribers {
				if s.system != nil && !s.system.Match(string(a.event.event.System)) {
					continue
				}

				if s.kind != nil && !s.kind.Match(string(a.event.event.Kind)) {
					continue
				}

				select {
				case <-a.event.ctx.Done():
					continue
				case <-b.closed:
					continue
				case s.eventCh <- a.event.event:
					continue
				}
			}

		case stop:
			// todo: possibly have more strategies
			for id := range b.subscribers {
				b.handleUnsubscribe(id)
			}
			close(b.closed)
			return
		}
	}
}

func (b *Bus) handleUnsubscribe(subID events.SubscriberID) {
	delete(b.subscribers, subID)
}

func (b *Bus) generateSubscriberID() events.SubscriberID {
	id := atomic.AddUint64(&b.subscriberCounter, 1)
	return events.SubscriberID(fmt.Sprintf("sub.%d", id))
}
