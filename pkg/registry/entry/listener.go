package entry

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/internal/wildcard"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Listener is a helper class for components to listen to their own registry entries.
type Listener struct {
	dtt          payload.Transcoder
	bus          events.Bus
	pattern      string
	factories    map[registry.Kind]func() any
	subscriberID events.SubscriberID
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	outputCh     chan<- registry.Operation
	lastEntry    registry.Entry
	mu           sync.Mutex
}

// NewListener creates a new Listener.
//
// Parameters:
//   - ctx: The context governing the lifecycle of the entry.
//   - b: The event bus to subscribe to.
//   - pattern: The pattern to match against entry paths (supports wildcards).
//   - factories: A map of registry kinds to the factories they should be unmarshaled into.
//   - outputCh: The channel to send unmarshalled operations into
//   - dtt: Transcoder to unmarshal entry data
func NewListener(
	ctx context.Context,
	b events.Bus,
	pattern string,
	types map[registry.Kind]func() interface{},
	outputCh chan<- registry.Operation,
	dtt payload.Transcoder,
) (*Listener, error) {
	ctx, cancel := context.WithCancel(ctx)
	l := &Listener{
		dtt:       dtt,
		bus:       b,
		pattern:   pattern,
		factories: types,
		ctx:       ctx,
		cancel:    cancel,
		outputCh:  outputCh,
	}

	ch := make(chan events.Event)
	var err error
	l.subscriberID, err = b.SubscribeP(ctx, registry.System, "entry.(create|update|delete)", ch)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to subscribe to event bus: %w", err)
	}

	l.wg.Add(1)
	go l.eventListener(ch)

	return l, nil
}

// eventListener listens for eventbus and processes them.
func (l *Listener) eventListener(ch <-chan events.Event) {
	defer l.wg.Done()
	w := wildcard.NewWildcard(l.pattern)

	for {
		select {
		case <-l.ctx.Done():
			l.bus.Unsubscribe(context.Background(), l.subscriberID)
			close(l.outputCh)
			return
		case evt, ok := <-ch:
			if !ok { // Channel closed
				return
			}

			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				l.rejectEntry(evt, fmt.Errorf("event data is not a registry.Entry"))
				continue
			}

			if !w.Match(string(entry.Kind)) {
				continue // Skip entries that don't match the type pattern.
			}

			l.mu.Lock()
			l.lastEntry = entry
			l.mu.Unlock()

			factory, ok := l.factories[entry.Kind]
			if !ok {
				l.rejectEntry(evt, fmt.Errorf("no type factory found for kind: %s", entry.Kind))
				continue
			}

			obj := factory()
			if entry.Data != nil {
				err := l.dtt.Unmarshal(entry.Data, &obj)
				if err != nil {
					l.rejectEntry(evt, fmt.Errorf("failed to unmarshal entry data: %w", err))
					continue
				}
			}

			// Send the processed event to the output channel.
			l.outputCh <- registry.Operation{
				Kind:  evt.Kind,
				Entry: entry,
				Data:  obj,
			}
		}
	}
}

// rejectEntry sends a rejection event to the registry.
func (l *Listener) rejectEntry(evt events.Event, reason error) {
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		// This should ideally never happen, as we already checked above.
		// Log an error or handle it appropriately.
		return
	}

	l.bus.Send(l.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Data:   registry.Entry{Path: entry.Path, Data: payload.NewString(reason.Error())},
	})
}

// RejectLast sends a rejection event for the last processed entry.
func (l *Listener) RejectLast(reason error) {
	if l.lastEntry.Path == "" {
		return
	}

	l.bus.Send(l.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Data:   registry.Entry{Path: l.lastEntry.Path, Data: payload.NewString(reason.Error())},
	})

	l.mu.Lock()
	l.lastEntry = registry.Entry{}
	l.mu.Unlock()
}

// AcceptLast sends an acceptance event for the last processed entry.
func (l *Listener) AcceptLast() {
	if l.lastEntry.Path == "" {
		return
	}

	l.bus.Send(l.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Data:   registry.Entry{Path: l.lastEntry.Path},
	})

	l.mu.Lock()
	l.lastEntry = registry.Entry{}
	l.mu.Unlock()
}

// Close stops the entry and unsubscribes from the event bus.
func (l *Listener) Close() {
	l.cancel()
	l.wg.Wait()
}
