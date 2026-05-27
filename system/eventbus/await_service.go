// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/event"
)

// AwaitService provides request-response pattern over pub-sub.
// Maintains single subscription per (system, kind) pair, routes by path.
type AwaitService struct {
	bus         event.Bus
	ctx         context.Context
	dispatchers map[string]*awaitDispatcher
	cancel      context.CancelFunc
	mu          sync.Mutex
}

type awaitDispatcher struct {
	subscriber *Subscriber
	pending    map[event.Path]chan event.Event
	system     event.System
	kind       event.Kind
	mu         sync.Mutex
}

type awaitWaiter struct {
	ctx     context.Context
	d       *awaitDispatcher
	ch      chan event.Event
	path    event.Path
	timeout time.Duration
	once    sync.Once
}

// NewAwaitService creates a new await service.
func NewAwaitService(bus event.Bus) *AwaitService {
	return &AwaitService{
		bus:         bus,
		dispatchers: make(map[string]*awaitDispatcher),
	}
}

// Start initializes the service.
func (s *AwaitService) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	return nil
}

// Stop shuts down the service.
func (s *AwaitService) Stop() error {
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
	s.dispatchers = make(map[string]*awaitDispatcher)

	return nil
}

// Prepare registers a waiter before the triggering request is sent.
func (s *AwaitService) Prepare(ctx context.Context, system event.System, kind event.Kind, path event.Path, timeout time.Duration) (event.AwaitWaiter, error) {
	if timeout <= 0 {
		timeout = event.DefaultAwaitTimeout
	}

	d, err := s.getOrCreateDispatcher(system, kind)
	if err != nil {
		return nil, err
	}

	ch := make(chan event.Event, 1)
	d.mu.Lock()
	d.pending[path] = ch
	d.mu.Unlock()

	return &awaitWaiter{
		ctx:     ctx,
		timeout: timeout,
		path:    path,
		d:       d,
		ch:      ch,
	}, nil
}

// Await waits for an event matching system, kind, and path.
func (s *AwaitService) Await(ctx context.Context, system event.System, kind event.Kind, path event.Path, timeout time.Duration) event.AwaitResult {
	waiter, err := s.Prepare(ctx, system, kind, path, timeout)
	if err != nil {
		return event.AwaitResult{Error: err}
	}
	return waiter.Wait()
}

func (w *awaitWaiter) Wait() event.AwaitResult {
	defer w.Close()

	timeoutCtx, cancel := context.WithTimeout(w.ctx, w.timeout)
	defer cancel()

	select {
	case evt := <-w.ch:
		accepted := isAcceptKind(evt.Kind)
		var resultErr error
		if !accepted {
			if e, ok := evt.Data.(error); ok {
				resultErr = e
			}
		}
		return event.AwaitResult{Event: evt, Accepted: accepted, Error: resultErr}

	case <-timeoutCtx.Done():
		if w.ctx.Err() != nil {
			return event.AwaitResult{Error: w.ctx.Err()}
		}
		return event.AwaitResult{Error: NewAwaitTimeoutError(w.path)}
	}
}

func (w *awaitWaiter) Close() {
	w.once.Do(func() {
		w.d.mu.Lock()
		if current, ok := w.d.pending[w.path]; ok && current == w.ch {
			delete(w.d.pending, w.path)
		}
		w.d.mu.Unlock()
	})
}

func (s *AwaitService) getOrCreateDispatcher(system event.System, kind event.Kind) (*awaitDispatcher, error) {
	key := system + ":" + kind

	s.mu.Lock()
	defer s.mu.Unlock()

	if d, ok := s.dispatchers[key]; ok {
		return d, nil
	}

	d := &awaitDispatcher{
		system:  system,
		kind:    kind,
		pending: make(map[event.Path]chan event.Event),
	}

	sub, err := NewSubscriber(s.ctx, s.bus, system, kind, d.handleEvent)
	if err != nil {
		return nil, err
	}
	d.subscriber = sub

	s.dispatchers[key] = d
	return d, nil
}

func (d *awaitDispatcher) handleEvent(evt event.Event) {
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
