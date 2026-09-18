// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/wildcard"
	"github.com/wippyai/runtime/system/eventbus"
)

// observedBus delegates to a real bus so subscription probes answer from the
// live subscription table, while recording what the runner publishes and how
// often it probes.
type observedBus struct {
	*eventbus.Bus
	sent   []event.Kind
	probes atomic.Int64
	mu     sync.Mutex
}

func newObservedBus() *observedBus {
	return &observedBus{Bus: eventbus.NewBus()}
}

func (b *observedBus) Send(ctx context.Context, evt event.Event) {
	b.mu.Lock()
	b.sent = append(b.sent, evt.Kind)
	b.mu.Unlock()
	b.Bus.Send(ctx, evt)
}

func (b *observedBus) HasSubscribers(system event.System, kind event.Kind) bool {
	b.probes.Add(1)
	return b.Bus.HasSubscribers(system, kind)
}

func (b *observedBus) kinds() []event.Kind {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]event.Kind(nil), b.sent...)
}

// listenerFunc replies to entry events for a single entry kind after an
// optional delay, standing in for a manager whose handler does real work.
type listenerFunc func(evt event.Event, entry registry.Entry)

func attachEntryListener(ctx context.Context, t *testing.T, bus event.Bus, kind registry.Kind, reply listenerFunc) func() {
	t.Helper()
	sub, err := eventbus.NewSubscriber(ctx, bus, registry.System, registry.AllEvents, func(evt event.Event) {
		entry, ok := evt.Data.(registry.Entry)
		if !ok || entry.Kind != kind {
			return
		}
		reply(evt, entry)
	})
	require.NoError(t, err)
	return sub.Close
}

func acceptAfter(bus event.Bus, delay time.Duration) listenerFunc {
	return func(_ event.Event, entry registry.Entry) {
		if delay > 0 {
			time.Sleep(delay)
		}
		bus.Send(context.Background(), event.Event{
			System: registry.System,
			Kind:   registry.EntryAccept,
			Path:   entry.ID.String(),
		})
	}
}

func listenerTestContext(t *testing.T, bus event.Bus) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	return event.WithAwaitService(ctx, awaitSvc), func() {
		_ = awaitSvc.Stop()
		cancel()
	}
}

func listenerEntry(id, kind string) registry.Entry {
	return registry.Entry{ID: registry.ParseID(id), Kind: kind, Data: payload.NewString("value")}
}

func TestBusRunnerRejectsDispatchWithoutSubscriber(t *testing.T) {
	bus := newObservedBus()
	defer bus.Stop()
	ctx, cleanup := listenerTestContext(t, bus)
	defer cleanup()

	builder := newTestBuilder(nil)
	br := NewBusRunner(bus, zap.NewNop(), builder,
		WithDispatchPolicy(internalDispatchPolicy()),
		WithEventWaitTimeout(10*time.Second),
	)
	entry := listenerEntry("component/listener/orphan", "listener")

	start := time.Now()
	state, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, time.Second, "a dispatch with no subscriber must not consume the wait budget")

	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Unavailable, apiErr.Kind())
	require.Equal(t, apierror.False, apiErr.Retryable())
	details := apiErr.Details()
	require.NotNil(t, details)
	require.Equal(t, entry.ID.String(), details.GetString("entry_id", ""))
	require.Equal(t, "listener", details.GetString("kind", ""))

	require.Empty(t, state)
	require.Equal(t, []event.Kind{registry.TxBegin, registry.TxDiscard}, bus.kinds(),
		"the operation is never published and there is nothing to roll back")
}

func TestBusRunnerWaitsForSubscriberBeyondLegacyDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("handler delay exceeds the legacy 30s default wait")
	}

	bus := newObservedBus()
	defer bus.Stop()
	ctx, cleanup := listenerTestContext(t, bus)
	defer cleanup()

	closeListener := attachEntryListener(ctx, t, bus, "listener", acceptAfter(bus, 31*time.Second))
	defer closeListener()

	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil), WithDispatchPolicy(internalDispatchPolicy()))
	entry := listenerEntry("component/listener/slow", "listener")

	state, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})

	require.NoError(t, err)
	require.Len(t, state, 1)
	require.Equal(t, entry.ID, state[0].ID)
}

func TestBusRunnerUnboundedWaitDefersToContext(t *testing.T) {
	bus := &recordingBus{}
	var timeouts []time.Duration
	await := &scriptedAwaitService{prepare: func(_ context.Context, _ event.System, kind event.Kind, _ event.Path, timeout time.Duration) (event.AwaitWaiter, error) {
		timeouts = append(timeouts, timeout)
		return &scriptedWaiter{result: event.AwaitResult{Event: event.Event{Kind: kind}, Accepted: true}}, nil
	}}
	ctx := event.WithAwaitService(ctxapi.NewRootContext(), await)

	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil), WithDispatchPolicy(internalDispatchPolicy()))
	entry := listenerEntry("component/listener/unbounded", "listener")

	_, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})

	require.NoError(t, err)
	require.NotEmpty(t, timeouts)
	for _, timeout := range timeouts {
		require.Equal(t, event.ContextBoundAwait, timeout,
			"without a configured cap the wait is bounded by the operation context")
	}
}

func TestBusRunnerCanceledContextEndsWait(t *testing.T) {
	bus := newObservedBus()
	defer bus.Stop()
	ctx, cleanup := listenerTestContext(t, bus)
	defer cleanup()

	// A subscriber exists but never replies, so only the context ends the wait.
	closeListener := attachEntryListener(ctx, t, bus, "listener", func(event.Event, registry.Entry) {})
	defer closeListener()

	opCtx, cancelOp := context.WithCancel(ctx)
	defer cancelOp()
	time.AfterFunc(100*time.Millisecond, cancelOp)

	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil), WithDispatchPolicy(internalDispatchPolicy()))
	entry := listenerEntry("component/listener/canceled", "listener")

	start := time.Now()
	_, err := br.Transition(opCtx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, elapsed, 5*time.Second)
}

func TestBusRunnerConfiguredCapEndsWait(t *testing.T) {
	bus := newObservedBus()
	defer bus.Stop()
	ctx, cleanup := listenerTestContext(t, bus)
	defer cleanup()

	closeListener := attachEntryListener(ctx, t, bus, "listener", func(event.Event, registry.Entry) {})
	defer closeListener()

	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil),
		WithDispatchPolicy(internalDispatchPolicy()),
		WithEventWaitTimeout(100*time.Millisecond),
	)
	entry := listenerEntry("component/listener/capped", "listener")

	_, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})

	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Timeout, apiErr.Kind())
	require.NotContains(t, err.Error(), "no listener is registered")
}

func TestBusRunnerInternalKindSkipsSubscriberProbe(t *testing.T) {
	bus := newObservedBus()
	defer bus.Stop()
	ctx, cleanup := listenerTestContext(t, bus)
	defer cleanup()

	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil),
		WithDispatchPolicy(internalDispatchPolicy()),
		WithEventWaitTimeout(10*time.Second),
	)
	entry := listenerEntry("component/config/internal", registry.EntryKind)

	state, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})

	require.NoError(t, err)
	require.Len(t, state, 1)
	require.Zero(t, bus.probes.Load(), "internal kinds never reach a listener, so they are not probed")
	require.Equal(t, []event.Kind{registry.TxBegin, registry.TxCommit}, bus.kinds())
}

// kindHandlerCheck stands in for the boot handler registry predicate: it
// answers for the kinds a replying handler declares.
func kindHandlerCheck(patterns ...registry.Kind) func(registry.Kind) bool {
	matchers := make([]*wildcard.Wildcard, 0, len(patterns))
	for _, pattern := range patterns {
		matchers = append(matchers, wildcard.NewWildcard(pattern))
	}
	return func(kind registry.Kind) bool {
		for _, matcher := range matchers {
			if matcher.Match(kind) {
				return true
			}
		}
		return false
	}
}

func TestBusRunnerRefusesKindNoHandlerReplies(t *testing.T) {
	fixtures := []struct {
		name     string
		kind     registry.Kind
		accepted bool
	}{
		{name: "declared kind proceeds", kind: "function.wasm", accepted: true},
		{name: "undeclared kind refused", kind: "service.http.nope"},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			bus := newObservedBus()
			defer bus.Stop()
			ctx, cleanup := listenerTestContext(t, bus)
			defer cleanup()

			// One subscriber covers every registry entry kind on the bus, so
			// only the handler predicate can tell the two kinds apart.
			closeListener := attachEntryListener(ctx, t, bus, fixture.kind, acceptAfter(bus, 0))
			defer closeListener()

			br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil),
				WithDispatchPolicy(internalDispatchPolicy()),
				WithEventWaitTimeout(10*time.Second),
				WithKindHandlerCheck(kindHandlerCheck("function.(wasm|wat)")),
			)
			entry := listenerEntry("component/listener/kinded", fixture.kind)

			start := time.Now()
			state, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})
			elapsed := time.Since(start)

			if fixture.accepted {
				require.NoError(t, err)
				require.Len(t, state, 1)
				return
			}

			require.Error(t, err)
			require.Less(t, elapsed, time.Second)
			var apiErr apierror.Error
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, apierror.Unavailable, apiErr.Kind())
			require.Equal(t, fixture.kind, apiErr.Details().GetString("kind", ""))
			require.Equal(t, []event.Kind{registry.TxBegin, registry.TxDiscard}, bus.kinds(),
				"the operation is never published and no waiter is prepared")
		})
	}
}

func TestBusRunnerRefusesWhenOnlyObserversWatchKind(t *testing.T) {
	bus := newObservedBus()
	defer bus.Stop()
	ctx, cleanup := listenerTestContext(t, bus)
	defer cleanup()

	closeListener := attachEntryListener(ctx, t, bus, "listener", func(event.Event, registry.Entry) {})
	defer closeListener()

	// An observer-only registry declares no replying kind at all.
	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil),
		WithDispatchPolicy(internalDispatchPolicy()),
		WithEventWaitTimeout(10*time.Second),
		WithKindHandlerCheck(kindHandlerCheck()),
	)
	entry := listenerEntry("component/listener/observed", "listener")

	start := time.Now()
	_, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, time.Second)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Unavailable, apiErr.Kind())
	require.Equal(t, []event.Kind{registry.TxBegin, registry.TxDiscard}, bus.kinds())
}

func TestBusRunnerAcceptsKindWhenHandlerDeclaresNoMatcher(t *testing.T) {
	bus := newObservedBus()
	defer bus.Stop()
	ctx, cleanup := listenerTestContext(t, bus)
	defer cleanup()

	closeListener := attachEntryListener(ctx, t, bus, "listener", acceptAfter(bus, 0))
	defer closeListener()

	// A handler with no declared matcher may reply for any kind, so the
	// predicate answers true and the operation proceeds.
	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil),
		WithDispatchPolicy(internalDispatchPolicy()),
		WithEventWaitTimeout(10*time.Second),
		WithKindHandlerCheck(func(registry.Kind) bool { return true }),
	)
	entry := listenerEntry("component/listener/undeclared", "listener")

	state, err := br.Transition(ctx, nil, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}})

	require.NoError(t, err)
	require.Len(t, state, 1)
}
