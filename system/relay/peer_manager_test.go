package relay

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type testEventCollector struct {
	events []event.Event
	mu     sync.Mutex
}

func (c *testEventCollector) collect(e event.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *testEventCollector) findEvent(kind event.Kind) *event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.events {
		if c.events[i].Kind == kind {
			return &c.events[i]
		}
	}
	return nil
}

func (c *testEventCollector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = c.events[:0]
}

type mockPeerReceiver struct {
	sendCalled int
	mu         sync.Mutex
}

func (r *mockPeerReceiver) Send(_ *relay.Package) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendCalled++
	return nil
}

func TestPeerManager_Register(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := NewNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, relay.System, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewPeerManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	peerReceiver := &mockPeerReceiver{}
	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerRegister,
		Path:   "peer1",
		Data: relay.PeerInfo{
			NodeID:   "peer1",
			Receiver: peerReceiver,
		},
	})

	time.Sleep(50 * time.Millisecond)

	acceptEvent := collector.findEvent(relay.PeerAccept)
	require.NotNil(t, acceptEvent, "PeerAccept event should be sent")
	assert.Equal(t, relay.System, acceptEvent.System)
	assert.Equal(t, "peer1", acceptEvent.Path)

	pkg := &relay.Package{Target: pid.PID{Node: "peer1", Host: "test", UniqID: "test"}}
	err = router.Send(pkg)
	require.NoError(t, err)
	assert.Equal(t, 1, peerReceiver.sendCalled, "Peer node should receive package")
}

func TestPeerManager_RegisterInvalidPayload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := NewNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, relay.System, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewPeerManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerRegister,
		Path:   "peer1",
		Data:   "invalid payload",
	})

	time.Sleep(50 * time.Millisecond)

	rejectEvent := collector.findEvent(relay.PeerReject)
	require.NotNil(t, rejectEvent, "PeerReject event should be sent")
	assert.Equal(t, relay.System, rejectEvent.System)
	assert.Equal(t, "peer1", rejectEvent.Path)
	assert.Contains(t, rejectEvent.Data.(string), "invalid payload")
}

func TestPeerManager_RegisterDuplicate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := NewNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, relay.System, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewPeerManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	peerReceiver1 := &mockPeerReceiver{}
	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerRegister,
		Path:   "peer1",
		Data: relay.PeerInfo{
			NodeID:   "peer1",
			Receiver: peerReceiver1,
		},
	})

	time.Sleep(50 * time.Millisecond)
	collector.reset()

	peerReceiver2 := &mockPeerReceiver{}
	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerRegister,
		Path:   "peer1",
		Data: relay.PeerInfo{
			NodeID:   "peer1",
			Receiver: peerReceiver2,
		},
	})

	time.Sleep(50 * time.Millisecond)

	rejectEvent := collector.findEvent(relay.PeerReject)
	require.NotNil(t, rejectEvent, "PeerReject event should be sent for duplicate")
	assert.Equal(t, "peer1", rejectEvent.Path)
	assert.Contains(t, rejectEvent.Data.(string), "already registered")
}

func TestPeerManager_RegisterConflictWithLocalNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := NewNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, relay.System, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewPeerManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	peerReceiver := &mockPeerReceiver{}
	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerRegister,
		Path:   "local",
		Data: relay.PeerInfo{
			NodeID:   "local",
			Receiver: peerReceiver,
		},
	})

	time.Sleep(50 * time.Millisecond)

	rejectEvent := collector.findEvent(relay.PeerReject)
	require.NotNil(t, rejectEvent, "PeerReject event should be sent for conflict")
	assert.Equal(t, "local", rejectEvent.Path)
	assert.Contains(t, rejectEvent.Data.(string), "conflicts with local node")
}

func TestPeerManager_Unregister(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := NewNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, relay.System, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewPeerManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	peerReceiver := &mockPeerReceiver{}
	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerRegister,
		Path:   "peer1",
		Data: relay.PeerInfo{
			NodeID:   "peer1",
			Receiver: peerReceiver,
		},
	})

	time.Sleep(50 * time.Millisecond)
	collector.reset()

	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerDelete,
		Path:   "peer1",
	})

	time.Sleep(50 * time.Millisecond)

	acceptEvent := collector.findEvent(relay.PeerAccept)
	require.NotNil(t, acceptEvent, "PeerAccept event should be sent")
	assert.Equal(t, "peer1", acceptEvent.Path)

	pkg := &relay.Package{Target: pid.PID{Node: "peer1", Host: "test", UniqID: "test"}}
	err = router.Send(pkg)
	require.Error(t, err, "Routing should fail after unregistration")
	assert.Contains(t, err.Error(), "not found")
}

func TestPeerManager_UnregisterNonexistent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := NewNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, relay.System, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewPeerManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.PeerDelete,
		Path:   "nonexistent",
	})

	time.Sleep(50 * time.Millisecond)

	acceptEvent := collector.findEvent(relay.PeerAccept)
	require.NotNil(t, acceptEvent, "PeerAccept event should be sent even for nonexistent node")
	assert.Equal(t, "nonexistent", acceptEvent.Path)
}
