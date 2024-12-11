package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"go.uber.org/zap"
)

type sub struct {
	system  events.System
	kind    *wildcard
	eventCh chan<- events.Event
}

type Bus struct {
	mu           sync.RWMutex
	subscribers  map[events.SubscriberID]sub
	logger       *zap.Logger
	internalEvCh chan events.Event
	unsubCh      chan events.SubscriberID // Channel for unsubscription requests (simplified)
	stop         chan struct{}
	wg           sync.WaitGroup
}

func NewBus(logger *zap.Logger) *Bus {
	b := &Bus{
		subscribers:  make(map[events.SubscriberID]sub),
		logger:       logger,
		internalEvCh: make(chan events.Event, 100),
		unsubCh:      make(chan events.SubscriberID), // Use SubscriberID directly
		stop:         make(chan struct{}),
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
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if ch == nil {
		return "", errors.New("nil channel provided")
	}

	subID := generateSubscriberID()

	b.mu.Lock()
	b.subscribers[subID] = sub{
		system:  system,
		kind:    nil,
		eventCh: ch,
	}
	b.mu.Unlock()

	return subID, nil
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

	subID := generateSubscriberID()
	w := newWildcard(string(kind))

	b.mu.Lock()
	b.subscribers[subID] = sub{
		system:  system,
		kind:    w,
		eventCh: ch,
	}
	b.mu.Unlock()

	return subID, nil
}

func (b *Bus) Unsubscribe(ctx context.Context, subID events.SubscriberID) {
	select {
	case b.unsubCh <- subID: // Send subID directly
	case <-b.stop: // Handle case where bus is stopped
	}
}

func (b *Bus) Send(ctx context.Context, event events.Event) {
	if event.Payload == nil {
		return
	}

	select {
	case <-b.stop: // Check if bus is stopped
		return
	default:
		if b.logger != nil {
			b.logger.Debug(
				"sending event",
				zap.String("system", string(event.System)),
				zap.String("kind", string(event.Kind)),
				zap.Any("payload", event.Payload),
			)
		}

		select {
		case b.internalEvCh <- event:
		default:
			if b.logger != nil {
				b.logger.Error(
					"internal event channel full, dropping event",
					zap.String("system", string(event.System)),
					zap.String("kind", string(event.Kind)),
					zap.Any("payload", event.Payload),
				)
			}
		}
	}
}

func (b *Bus) Stop() {
	close(b.stop)
	b.wg.Wait()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Close all subscriber channels
	for _, s := range b.subscribers {
		close(s.eventCh)
	}
	b.subscribers = make(map[events.SubscriberID]sub)

	close(b.internalEvCh)
}

func (b *Bus) handleEvents() {
	defer b.wg.Done()

	for {
		select {
		case <-b.stop:
			return
		case event, ok := <-b.internalEvCh:
			if !ok {
				return
			}

			b.mu.RLock()
			for subID, s := range b.subscribers {
				if s.system != event.System && s.system != "*" {
					continue
				}

				if s.kind != nil && !s.kind.match(string(event.Kind)) {
					continue
				}

				select {
				case s.eventCh <- event:
				default:
					if b.logger != nil {
						b.logger.Warn(
							"subscriber channel full, dropping event",
							zap.String("sid", string(subID)),
							zap.String("system", string(event.System)),
							zap.String("kind", string(event.Kind)),
							zap.Any("payload", event.Payload),
						)
					}
				}
			}
			b.mu.RUnlock()

		case subID := <-b.unsubCh: // Receive subID directly
			b.mu.Lock()
			if s, exists := b.subscribers[subID]; exists {
				close(s.eventCh)
				delete(b.subscribers, subID)
			}
			b.mu.Unlock()
		}
	}
}

func generateSubscriberID() events.SubscriberID {
	return events.SubscriberID(fmt.Sprintf("sub-%d", time.Now().UnixNano()))
}
