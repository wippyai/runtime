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

type sendEvent struct {
	event events.Event
	ctx   context.Context
}

type sub struct {
	subID   events.SubscriberID
	system  events.System
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
	fout              chan sendEvent
	stop              chan struct{}
	sub               chan sub
	unsub             chan unsub
	wg                sync.WaitGroup
	subscriberCounter uint64
}

func NewBus(logger *zap.Logger) *Bus {
	b := &Bus{
		subscribers: make(map[events.SubscriberID]sub),
		logger:      logger,
		fout:        make(chan sendEvent, 100),
		stop:        make(chan struct{}),
		sub:         make(chan sub),
		unsub:       make(chan unsub),
	}

	b.wg.Add(1)
	go b.handleEvents()

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
	kind events.Kind, // todo: change to wildcard.Wildcard
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

	sub := sub{
		subID:   subID,
		system:  system,
		kind:    w,
		eventCh: ch,
		doneCh:  make(chan bool),
	}

	select {
	case b.sub <- sub:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-b.stop:
		return "", errors.New("bus stopped")
	case <-sub.doneCh:
	}

	return subID, nil
}

func (b *Bus) Unsubscribe(ctx context.Context, subID events.SubscriberID) {
	if ctx.Err() != nil || b.stop == nil {
		return
	}

	unsub := unsub{
		subID:  subID,
		doneCh: make(chan bool),
	}

	select {
	case b.unsub <- unsub:
	case <-ctx.Done():
		return
	}

	select {
	case <-ctx.Done():
	case <-b.stop:
	case <-unsub.doneCh:
	}
}

func (b *Bus) Send(ctx context.Context, event events.Event) {
	if event.Data == nil {
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-b.stop: // Check if bus is stopped
		return
	case b.fout <- sendEvent{event: event, ctx: ctx}:
		if b.logger != nil {
			b.logger.Debug(
				"sending event",
				zap.String("system", string(event.System)),
				zap.String("kind", string(event.Kind)),
				zap.Any("payload", event.Data),
			)
		}
	}
}

func (b *Bus) Stop() {
	close(b.stop)
	b.wg.Wait()
}

func (b *Bus) handleEvents() {
	defer b.wg.Done()

	for {
		select {
		case <-b.stop:
			for _, s := range b.subscribers {
				close(s.eventCh)
			}
			close(b.fout)
			return
		case sub := <-b.sub:
			b.subscribers[sub.subID] = sub
			sub.doneCh <- true
		case u := <-b.unsub:
			b.handleUnsubscribe(u.subID)
			u.doneCh <- true
		case send, ok := <-b.fout:
			if !ok {
				return
			}

			if send.ctx.Err() != nil {
				continue
			}

			for _, s := range b.subscribers {
				if s.system != send.event.System && s.system != "*" {
					continue
				}

				if s.kind != nil && !s.kind.Match(string(send.event.Kind)) {
					continue
				}

				select {
				case <-send.ctx.Done():
					continue
				case s.eventCh <- send.event:
					continue
				}
			}
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
	return events.SubscriberID(fmt.Sprintf("sub-%d", id))
}
