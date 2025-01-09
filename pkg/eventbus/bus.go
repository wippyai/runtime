package eventbus

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/internal/wildcard"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
)

type actionType int

const (
	subscribe actionType = iota
	unsubscribe
	send
	stop
)

type action struct {
	actionType   actionType
	subscribe    sub
	unsubscribe  unsub
	event        sendEvent
	stopDoneChan chan struct{} // opChan to signal stop completion
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

type Bus struct {
	subscribers       map[events.SubscriberID]sub
	logger            *zap.Logger
	actions           chan action
	wg                sync.WaitGroup
	subscriberCounter uint64
}

func NewBus(logger *zap.Logger) *Bus {
	b := &Bus{
		subscribers: make(map[events.SubscriberID]sub),
		logger:      logger,
		actions:     make(chan action, 100), // Buffered channel for all actions
	}

	b.wg.Add(1)
	go b.handleActions()

	return b
}

func (b *Bus) Subscribe(
	ctx context.Context,
	system events.System,
	ch chan<- events.Event,
) (events.SubscriberID, error) {
	return b.SubscribeP(ctx, system, "", ch)
}

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
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-sub.doneCh:
		return subID, nil
	}

}

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
	case <-ctx.Done():
		return
	}

	select {
	case <-ctx.Done():
	case <-unsub.doneCh:
	}
}

func (b *Bus) Send(ctx context.Context, event events.Event) {
	select {
	case b.actions <- action{actionType: send, event: sendEvent{event: event, ctx: ctx}}:
		if b.logger != nil {
			b.logger.Debug(
				"sending event",
				zap.String("system", string(event.System)),
				zap.String("kind", string(event.Kind)),
				zap.String("path", string(event.Path)),
				zap.Any("payload", event.Data),
			)
		}
	case <-ctx.Done():
		return
	}
}

func (b *Bus) Stop() {
	done := make(chan struct{})
	b.actions <- action{actionType: stop, stopDoneChan: done}
	<-done // Wait for stop to complete
	b.wg.Wait()
}

func (b *Bus) handleActions() {
	defer b.wg.Done()

	for a := range b.actions {
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
					b.logger.Warn("context cancelled", zap.String("subscriber", string(s.subID)))
					continue
				case s.eventCh <- a.event.event:
					continue
				}
			}

		case stop:
			for _, s := range b.subscribers {
				close(s.eventCh)
			}
			close(b.actions)
			a.stopDoneChan <- struct{}{} // Signal stop completion
			return
		}
	}
}

func (b *Bus) handleUnsubscribe(subID events.SubscriberID) {
	if s, exists := b.subscribers[subID]; exists {
		close(s.eventCh)
		delete(b.subscribers, subID)
	}
}

func (b *Bus) generateSubscriberID() events.SubscriberID {
	id := atomic.AddUint64(&b.subscriberCounter, 1)
	return events.SubscriberID(fmt.Sprintf("sub.%d", id))
}
