package await

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/system/eventbus"
)

// Service provides request-response pattern over pub-sub.
// Maintains single subscription per (system, kind) pair, routes by path.
type Service struct {
	bus         event.Bus
	dispatchers map[string]*dispatcher
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
}

type dispatcher struct {
	system     event.System
	kind       event.Kind
	subscriber *eventbus.Subscriber
	pending    map[event.Path]chan event.Event
	mu         sync.Mutex
}

// NewService creates a new await service.
func NewService(bus event.Bus) *Service {
	return &Service{
		bus:         bus,
		dispatchers: make(map[string]*dispatcher),
	}
}

// Start initializes the service.
func (s *Service) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	return nil
}

// Stop shuts down the service.
func (s *Service) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range s.dispatchers {
		if d.subscriber != nil {
			d.subscriber.Close()
		}
	}
	s.dispatchers = make(map[string]*dispatcher)

	return nil
}

// Await waits for an event matching system, kind, and path.
func (s *Service) Await(ctx context.Context, system event.System, kind event.Kind, path event.Path, timeout time.Duration) event.AwaitResult {
	d, err := s.getOrCreateDispatcher(system, kind)
	if err != nil {
		return event.AwaitResult{Error: err}
	}

	ch := make(chan event.Event, 1)

	d.mu.Lock()
	d.pending[path] = ch
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.pending, path)
		d.mu.Unlock()
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case evt := <-ch:
		accepted := isAcceptKind(evt.Kind)
		var resultErr error
		if !accepted {
			if e, ok := evt.Data.(error); ok {
				resultErr = e
			}
		}
		return event.AwaitResult{Event: evt, Accepted: accepted, Error: resultErr}

	case <-timeoutCtx.Done():
		if ctx.Err() != nil {
			return event.AwaitResult{Error: ctx.Err()}
		}
		return event.AwaitResult{Error: eventbus.NewAwaitTimeoutError(path)}
	}
}

func (s *Service) getOrCreateDispatcher(system event.System, kind event.Kind) (*dispatcher, error) {
	key := system + ":" + kind

	s.mu.Lock()
	defer s.mu.Unlock()

	if d, ok := s.dispatchers[key]; ok {
		return d, nil
	}

	d := &dispatcher{
		system:  system,
		kind:    kind,
		pending: make(map[event.Path]chan event.Event),
	}

	sub, err := eventbus.NewSubscriber(s.ctx, s.bus, system, kind, d.handleEvent)
	if err != nil {
		return nil, err
	}
	d.subscriber = sub

	s.dispatchers[key] = d
	return d, nil
}

func (d *dispatcher) handleEvent(evt event.Event) {
	d.mu.Lock()
	ch, ok := d.pending[evt.Path]
	d.mu.Unlock()

	if ok {
		select {
		case ch <- evt:
		default:
		}
	}
}

func isAcceptKind(kind event.Kind) bool {
	return kind == "accept" || strings.HasSuffix(kind, ".accept") || strings.HasSuffix(kind, ".accepted")
}
