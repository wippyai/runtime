package eventbus

import (
	"context"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/event"
)

const defaultAwaitTimeout = 30 * time.Second

// Awaiter waits for a specific event to be received on the bus.
// Designed for inline use within handlers to wait for secondary async events.
type Awaiter struct {
	bus     event.Bus
	system  event.System
	kind    event.Kind
	timeout time.Duration
}

// AwaitResult contains the result of waiting for an event.
type AwaitResult struct {
	Event    event.Event
	Accepted bool
	Error    error
}

// Waiter represents a prepared wait operation.
// Subscribe first, then trigger the action, then wait for result.
type Waiter struct {
	ctx     context.Context
	bus     event.Bus
	path    event.Path
	timeout time.Duration
	ch      chan event.Event
	subID   event.SubscriberID
}

// NewAwaiter creates an awaiter for events matching system and kind.
func NewAwaiter(bus event.Bus, system event.System, kind event.Kind) *Awaiter {
	return &Awaiter{
		bus:     bus,
		system:  system,
		kind:    kind,
		timeout: defaultAwaitTimeout,
	}
}

// WithTimeout sets custom timeout for the awaiter.
func (a *Awaiter) WithTimeout(d time.Duration) *Awaiter {
	a.timeout = d
	return a
}

// Prepare subscribes to events for the given path and returns a Waiter.
// Use this to avoid race conditions: subscribe BEFORE sending the triggering event.
// Usage:
//
//	waiter, err := awaiter.Prepare(ctx, path)
//	if err != nil { return err }
//	defer waiter.Close()
//	bus.Send(ctx, triggeringEvent)
//	result := waiter.Wait()
func (a *Awaiter) Prepare(ctx context.Context, path event.Path) (*Waiter, error) {
	ch := make(chan event.Event, 1)

	subID, err := a.bus.SubscribeP(ctx, a.system, a.kind, ch)
	if err != nil {
		return nil, err
	}

	return &Waiter{
		ctx:     ctx,
		bus:     a.bus,
		path:    path,
		timeout: a.timeout,
		ch:      ch,
		subID:   subID,
	}, nil
}

// Wait blocks until the expected event arrives or timeout.
func (w *Waiter) Wait() AwaitResult {
	defer w.Close()

	timeoutCtx, cancel := context.WithTimeout(w.ctx, w.timeout)
	defer cancel()

	for {
		select {
		case evt := <-w.ch:
			if evt.Path != w.path {
				continue
			}
			accepted := isAcceptKind(evt.Kind)
			var resultErr error
			if !accepted {
				if e, ok := evt.Data.(error); ok {
					resultErr = e
				}
			}
			return AwaitResult{Event: evt, Accepted: accepted, Error: resultErr}

		case <-timeoutCtx.Done():
			if w.ctx.Err() != nil {
				return AwaitResult{Error: w.ctx.Err()}
			}
			return AwaitResult{Error: NewAwaitTimeoutError(w.path)}
		}
	}
}

// Close unsubscribes from the event bus.
func (w *Waiter) Close() {
	if w.subID != "" {
		w.bus.Unsubscribe(w.ctx, w.subID)
		w.subID = ""
	}
}

// WaitFor subscribes, waits for event, then unsubscribes.
// WARNING: Has race condition if event fires before subscription completes.
// Prefer Prepare() + Wait() pattern for critical paths.
func (a *Awaiter) WaitFor(ctx context.Context, path event.Path) AwaitResult {
	waiter, err := a.Prepare(ctx, path)
	if err != nil {
		return AwaitResult{Error: err}
	}
	return waiter.Wait()
}

// Await is a convenience function for one-shot event waiting.
// WARNING: Has race condition. Use Awaiter.Prepare() for critical paths.
func Await(ctx context.Context, bus event.Bus, system event.System, kind event.Kind, path event.Path) AwaitResult {
	return NewAwaiter(bus, system, kind).WaitFor(ctx, path)
}

// AwaitWithTimeout is Await with custom timeout.
func AwaitWithTimeout(ctx context.Context, bus event.Bus, system event.System, kind event.Kind, path event.Path, timeout time.Duration) AwaitResult {
	return NewAwaiter(bus, system, kind).WithTimeout(timeout).WaitFor(ctx, path)
}

func isAcceptKind(kind event.Kind) bool {
	return kind == "accept" || strings.HasSuffix(kind, ".accept") || strings.HasSuffix(kind, ".accepted")
}
