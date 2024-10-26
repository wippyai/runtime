package eventsbus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api"
)

type sub struct {
	pattern string
	w       *wildcard
	events  chan<- api.Event
}

type Bus struct {
	mu           sync.RWMutex
	subscribers  map[string][]*sub
	internalEvCh chan api.Event
	stop         chan struct{}
}

func newEventsBus() *Bus {
	return &Bus{
		subscribers:  make(map[string][]*sub, 10),
		internalEvCh: make(chan api.Event, 100),
		stop:         make(chan struct{}),
	}
}

// SubscribeAll for all Pony events
// returns subscriptionID
func (eb *Bus) SubscribeAll(ctx context.Context, subID string, ch chan<- api.Event) error {
	if ch == nil {
		return errors.New("nil channel provided")
	}

	subIDTr := strings.Trim(subID, " ")

	if subIDTr == "" {
		return errors.New("subscriberID can't be empty")
	}

	eb.subscribe(ctx, subID, "*", ch)

	return nil
}

// SubscribeP pattern like "sub.EventType"
// subID is a subscriber ID
// sub is a sub to subscribe to
// etype is an event type to subscribe to
// ch is a channel to receive events
func (eb *Bus) SubscribeP(
	ctx context.Context,
	subID string,
	subSystem api.Subsystem,
	etype api.EventType, // todo: maybe ignore that as subscribe to system only
	ch chan<- api.Event,
) error {
	if ch == nil {
		return errors.New("nil channel provided")
	}

	pattern := fmt.Sprintf("%s.%s", subSystem, etype)

	subIDTr := strings.Trim(subID, " ")
	patternTr := strings.Trim(pattern, " ")

	if subIDTr == "" || patternTr == "" {
		return errors.New("subscriberID or pattern can't be empty")
	}

	eb.subscribe(ctx, subID, pattern, ch)

	return nil
}

func (eb *Bus) Unsubscribe(_ context.Context, subID string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	delete(eb.subscribers, subID)
}

// UnsubscribeP unsubscribes from a specific event
func (eb *Bus) UnsubscribeP(
	_ context.Context,
	subID string,
	subSystem api.Subsystem,
	etype api.EventType,
) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	pattern := fmt.Sprintf("%s.%s", subSystem, etype)

	if _, ok := eb.subscribers[subID]; !ok {
		return
	}

	sbArr := eb.subscribers[subID]

	for i := 0; i < len(sbArr); i++ {
		if sbArr[i].pattern == pattern {
			sbArr[i] = sbArr[len(sbArr)-1]
			sbArr = sbArr[:len(sbArr)-1]
			// replace it with a new array
			eb.subscribers[subID] = sbArr
		}
	}
}

// Send sends event to the events bus
func (eb *Bus) Send(ctx context.Context, ev api.Event) {
	// do not accept nil events
	if ev == nil {
		return
	}

	eb.internalEvCh <- ev
}

func (eb *Bus) Len() uint {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return uint(len(eb.subscribers))
}

func (eb *Bus) subscribe(_ context.Context, subID string, pattern string, ch chan<- api.Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	w := newWildcard(pattern)

	if subArr, ok := eb.subscribers[subID]; ok {
		// at this point we are confident that sb is a '[]*sub' type
		subArr = append(subArr, &sub{
			pattern: pattern,
			w:       w,
			events:  ch,
		})

		eb.subscribers[subID] = subArr
	}

	subArr := make([]*sub, 0, 1)
	subArr = append(subArr, &sub{
		pattern: pattern,
		w:       w,
		events:  ch,
	})

	eb.subscribers[subID] = subArr
}

func (eb *Bus) handleEvents() {
	for ev := range eb.internalEvCh {
		// sub.ConfigurationUpdate for example
		eb.mu.RLock()
		wc := fmt.Sprintf("%s.%s", ev.Subsystem(), ev.Type())

		for _, vsub := range eb.subscribers {
			for i := 0; i < len(vsub); i++ {
				if vsub[i].w.match(wc) {
					select {
					case vsub[i].events <- ev:
					default:
					}
				}
			}
		}

		eb.mu.RUnlock()
	}
}
