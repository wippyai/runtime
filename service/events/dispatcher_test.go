package events

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
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
	id := event.SubscriberID(string(rune('A' + b.nextID)))
	b.subscribers[id] = ch
	return id, nil
}

func (b *mockBus) SubscribeP(ctx context.Context, system event.System, _ event.Kind, ch chan<- event.Event) (event.SubscriberID, error) {
	return b.Subscribe(ctx, system, ch)
}

func (b *mockBus) Unsubscribe(_ context.Context, id event.SubscriberID) {
	delete(b.subscribers, id)
}

func (b *mockBus) Send(ctx context.Context, evt event.Event) {
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

func (n *mockNode) ID() relay.NodeID { return "test" }

func (n *mockNode) Send(pkg *relay.Package) error {
	n.packages = append(n.packages, pkg)
	return nil
}

func (n *mockNode) RegisterHost(relay.HostID, relay.Host) error { return nil }
func (n *mockNode) UnregisterHost(relay.HostID)                 {}
func (n *mockNode) GetHost(relay.HostID) (relay.Host, bool)     { return nil, false }
func (n *mockNode) Attach(relay.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (n *mockNode) Detach(relay.PID) {}

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
	pid := relay.PID{UniqID: "test-1"}
	completed := make(chan struct{})
	var result any

	receiver := &mockReceiver{
		onComplete: func(_ uint64, data any, _ error) {
			result = data
			close(completed)
		},
	}

	cmd := event.EventsSubscribeCmd{
		System: "test.system",
		Kind:   "test.kind",
		Topic:  "events@1",
		PID:    pid,
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
	pid := relay.PID{UniqID: "test-1"}
	d.mu.Lock()
	d.subs["events@1"] = &subscription{
		pid:    pid,
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
	assert.Equal(t, relay.Topic("events@1"), node.packages[0].Messages[0].Topic)
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

	cmd := event.EventsSendCmd{
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
		assert.Equal(t, event.System("test.system"), evt.System)
		assert.Equal(t, event.Kind("test.kind"), evt.Kind)
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
