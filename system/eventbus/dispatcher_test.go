package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// mockBus implements event.Bus for testing.
type mockBus struct {
	subscribers map[event.SubscriberID]chan<- event.Event
	nextID      int
}

func newMockBus() *mockBus {
	return &mockBus{
		subscribers: make(map[event.SubscriberID]chan<- event.Event),
	}
}

func (b *mockBus) Subscribe(_ context.Context, _ event.System, ch chan<- event.Event) (event.SubscriberID, error) {
	b.nextID++
	id := string(rune('A' + b.nextID))
	b.subscribers[id] = ch
	return id, nil
}

func (b *mockBus) SubscribeP(ctx context.Context, system event.System, _ event.Kind, ch chan<- event.Event) (event.SubscriberID, error) {
	return b.Subscribe(ctx, system, ch)
}

func (b *mockBus) Unsubscribe(_ context.Context, id event.SubscriberID) {
	delete(b.subscribers, id)
}

func (b *mockBus) Send(_ context.Context, evt event.Event) {
	for _, ch := range b.subscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// mockNode implements relay.Node for testing.
type mockNode struct {
	packages []*relay.Package
}

func (n *mockNode) ID() pid.NodeID { return "test" }

func (n *mockNode) Send(pkg *relay.Package) error {
	n.packages = append(n.packages, pkg)
	return nil
}

func (n *mockNode) RegisterHost(pid.HostID, relay.Receiver) error { return nil }
func (n *mockNode) UnregisterHost(pid.HostID)                     {}
func (n *mockNode) GetHost(pid.HostID) (relay.Receiver, bool)     { return nil, false }
func (n *mockNode) Attach(pid.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (n *mockNode) Detach(pid.PID) {}

func TestDispatcher_StartStop(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	err := d.Start(context.Background())
	require.NoError(t, err)

	err = d.Stop(context.Background())
	require.NoError(t, err)
}

func TestDispatcher_Subscribe(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	ctx := context.Background()
	err := d.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = d.Stop(ctx) }()

	// Subscribe
	p := pid.PID{UniqID: "test-1"}
	completed := make(chan struct{})
	var result any

	receiver := &mockReceiver{
		onComplete: func(_ uint64, data any, _ error) {
			result = data
			close(completed)
		},
	}

	cmd := event.SubscribeCmd{
		System: "test.system",
		Kind:   "test.kind",
		Topic:  "events@1",
		PID:    p,
	}

	err = d.handleSubscribe(ctx, cmd, 0, receiver)
	require.NoError(t, err)

	<-completed
	sub, ok := result.(event.Subscription)
	require.True(t, ok)
	assert.Equal(t, "test.system", sub.System)
	assert.Equal(t, "test.kind", sub.Kind)
	assert.Equal(t, "events@1", sub.Topic)
}

func TestDispatcher_RouteEvent(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	ctx := context.Background()
	err := d.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = d.Stop(ctx) }()

	// Add subscription
	p := pid.PID{UniqID: "test-1"}
	d.mu.Lock()
	d.subs["events@1"] = &subscription{
		pid:    p,
		system: "test.*",
		kind:   "",
		topic:  "events@1",
	}
	d.mu.Unlock()

	// Route an event
	d.routeEvent(event.Event{
		System: "test.system",
		Kind:   "test.kind",
		Path:   "/test/path",
		Data:   map[string]any{"key": "value"},
	})

	// Check that package was sent
	require.Len(t, node.packages, 1)
	assert.Equal(t, "events@1", node.packages[0].Messages[0].Topic)
}

func TestDispatcher_PatternMatch(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		match   bool
	}{
		{"*", "anything", true},
		{"", "anything", true},
		{"test.*", "test.system", true},
		{"test.*", "other.system", false},
		{"exact", "exact", true},
		{"exact", "notexact", false},
		{"prefix.*", "prefix.suffix", true},
		{"prefix.*", "prefixsuffix", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.value, func(t *testing.T) {
			assert.Equal(t, tt.match, matchPattern(tt.pattern, tt.value))
		})
	}
}

func TestDispatcher_Unsubscribe(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	d.subs["events@1"] = &subscription{topic: "events@1"}
	assert.Len(t, d.subs, 1)

	d.Unsubscribe("events@1")
	assert.Len(t, d.subs, 0)
}

func TestDispatcher_Send(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	ctx := context.Background()
	err := d.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = d.Stop(ctx) }()

	// Add a subscriber to capture the event
	received := make(chan event.Event, 1)
	_, _ = bus.Subscribe(ctx, "test.system", received)

	// Send via dispatcher
	completed := make(chan struct{})
	receiver := &mockReceiver{
		onComplete: func(_ uint64, _ any, _ error) {
			close(completed)
		},
	}

	cmd := event.SendCmd{
		System: "test.system",
		Kind:   "test.kind",
		Path:   "/test/path",
		Data:   map[string]any{"key": "value"},
	}

	err = d.handleSend(ctx, cmd, 0, receiver)
	require.NoError(t, err)

	<-completed

	select {
	case evt := <-received:
		assert.Equal(t, "test.system", evt.System)
		assert.Equal(t, "test.kind", evt.Kind)
		assert.Equal(t, "/test/path", evt.Path)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

type mockReceiver struct {
	onComplete func(tag uint64, data any, err error)
}

func (r *mockReceiver) CompleteYield(tag uint64, data any, err error) {
	if r.onComplete != nil {
		r.onComplete(tag, data, err)
	}
}

func (r *mockReceiver) FailYield(tag uint64, err error) {
	if r.onComplete != nil {
		r.onComplete(tag, nil, err)
	}
}

func TestDispatcher_RegisterAll(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	registered := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		registered[id] = h
	})

	assert.NotNil(t, registered[event.Subscribe])
	assert.NotNil(t, registered[event.Send])
	assert.Len(t, registered, 2)
}

func TestDispatcher_StartError(t *testing.T) {
	bus := &errorBus{err: assert.AnError}
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	err := d.Start(context.Background())
	require.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

func TestDispatcher_RouteEventFiltering(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	p := pid.PID{UniqID: "test-1"}

	t.Run("system mismatch", func(t *testing.T) {
		node.packages = nil
		d.mu.Lock()
		d.subs["events@1"] = &subscription{
			pid:    p,
			system: "other.*",
			kind:   "",
			topic:  "events@1",
		}
		d.mu.Unlock()

		d.routeEvent(event.Event{
			System: "test.system",
			Kind:   "test.kind",
		})

		assert.Len(t, node.packages, 0)
	})

	t.Run("kind mismatch", func(t *testing.T) {
		node.packages = nil
		d.mu.Lock()
		d.subs["events@2"] = &subscription{
			pid:    p,
			system: "test.*",
			kind:   "other.kind",
			topic:  "events@2",
		}
		d.mu.Unlock()

		d.routeEvent(event.Event{
			System: "test.system",
			Kind:   "test.kind",
		})

		assert.Len(t, node.packages, 0)
	})
}

func TestDispatcher_SubscribeUnsubscribeCallback(t *testing.T) {
	bus := newMockBus()
	node := &mockNode{}
	d := NewDispatcher(bus, node)

	ctx := context.Background()
	err := d.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = d.Stop(ctx) }()

	p := pid.PID{UniqID: "test-1"}
	completed := make(chan struct{})
	var sub event.Subscription

	receiver := &mockReceiver{
		onComplete: func(_ uint64, data any, _ error) {
			sub = data.(event.Subscription)
			close(completed)
		},
	}

	cmd := event.SubscribeCmd{
		System: "test.system",
		Kind:   "test.kind",
		Topic:  "events@unsub",
		PID:    p,
	}

	err = d.handleSubscribe(ctx, cmd, 0, receiver)
	require.NoError(t, err)
	<-completed

	d.mu.RLock()
	_, exists := d.subs["events@unsub"]
	d.mu.RUnlock()
	assert.True(t, exists)

	sub.Unsubscribe()

	d.mu.RLock()
	_, exists = d.subs["events@unsub"]
	d.mu.RUnlock()
	assert.False(t, exists)
}

type errorBus struct {
	err error
}

func (b *errorBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", b.err
}

func (b *errorBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", b.err
}

func (b *errorBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func (b *errorBus) Send(_ context.Context, _ event.Event) {}
