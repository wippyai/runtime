package relay

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/relay"
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

type mockVirtualNodeReceiver struct {
	sendCalled int
	mu         sync.Mutex
}

func (r *mockVirtualNodeReceiver) Send(_ *api.Package) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendCalled++
	return nil
}

func TestVirtualNodeManager_Register(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := newMockNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, api.VirtualNodeSystem, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewVirtualNodeManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	virtualReceiver := &mockVirtualNodeReceiver{}
	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeRegister,
		Path:   "virtual1",
		Data: api.VirtualNodeInfo{
			NodeID:   "virtual1",
			Receiver: virtualReceiver,
		},
	})

	time.Sleep(50 * time.Millisecond)

	acceptEvent := collector.findEvent(api.VirtualNodeAccept)
	require.NotNil(t, acceptEvent, "VirtualNodeAccept event should be sent")
	assert.Equal(t, api.VirtualNodeSystem, acceptEvent.System)
	assert.Equal(t, "virtual1", string(acceptEvent.Path))

	pkg := &api.Package{Target: api.PID{Node: "virtual1", Host: "test", UniqID: "test"}}
	err = router.Send(pkg)
	require.NoError(t, err)
	assert.Equal(t, 1, virtualReceiver.sendCalled, "Virtual node should receive package")
}

func TestVirtualNodeManager_RegisterInvalidPayload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := newMockNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, api.VirtualNodeSystem, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewVirtualNodeManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeRegister,
		Path:   "virtual1",
		Data:   "invalid payload",
	})

	time.Sleep(50 * time.Millisecond)

	rejectEvent := collector.findEvent(api.VirtualNodeReject)
	require.NotNil(t, rejectEvent, "VirtualNodeReject event should be sent")
	assert.Equal(t, api.VirtualNodeSystem, rejectEvent.System)
	assert.Equal(t, "virtual1", string(rejectEvent.Path))
	assert.Contains(t, rejectEvent.Data.(string), "invalid payload")
}

func TestVirtualNodeManager_RegisterDuplicate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := newMockNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, api.VirtualNodeSystem, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewVirtualNodeManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	virtualReceiver1 := &mockVirtualNodeReceiver{}
	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeRegister,
		Path:   "virtual1",
		Data: api.VirtualNodeInfo{
			NodeID:   "virtual1",
			Receiver: virtualReceiver1,
		},
	})

	time.Sleep(50 * time.Millisecond)
	collector.reset()

	virtualReceiver2 := &mockVirtualNodeReceiver{}
	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeRegister,
		Path:   "virtual1",
		Data: api.VirtualNodeInfo{
			NodeID:   "virtual1",
			Receiver: virtualReceiver2,
		},
	})

	time.Sleep(50 * time.Millisecond)

	rejectEvent := collector.findEvent(api.VirtualNodeReject)
	require.NotNil(t, rejectEvent, "VirtualNodeReject event should be sent for duplicate")
	assert.Equal(t, "virtual1", string(rejectEvent.Path))
	assert.Contains(t, rejectEvent.Data.(string), "already registered")
}

func TestVirtualNodeManager_RegisterConflictWithLocalNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := newMockNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, api.VirtualNodeSystem, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewVirtualNodeManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	virtualReceiver := &mockVirtualNodeReceiver{}
	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeRegister,
		Path:   "local",
		Data: api.VirtualNodeInfo{
			NodeID:   "local",
			Receiver: virtualReceiver,
		},
	})

	time.Sleep(50 * time.Millisecond)

	rejectEvent := collector.findEvent(api.VirtualNodeReject)
	require.NotNil(t, rejectEvent, "VirtualNodeReject event should be sent for conflict")
	assert.Equal(t, "local", string(rejectEvent.Path))
	assert.Contains(t, rejectEvent.Data.(string), "conflicts with local node")
}

func TestVirtualNodeManager_Unregister(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := newMockNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, api.VirtualNodeSystem, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewVirtualNodeManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	virtualReceiver := &mockVirtualNodeReceiver{}
	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeRegister,
		Path:   "virtual1",
		Data: api.VirtualNodeInfo{
			NodeID:   "virtual1",
			Receiver: virtualReceiver,
		},
	})

	time.Sleep(50 * time.Millisecond)
	collector.reset()

	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeDelete,
		Path:   "virtual1",
	})

	time.Sleep(50 * time.Millisecond)

	acceptEvent := collector.findEvent(api.VirtualNodeAccept)
	require.NotNil(t, acceptEvent, "VirtualNodeAccept event should be sent")
	assert.Equal(t, "virtual1", string(acceptEvent.Path))

	pkg := &api.Package{Target: api.PID{Node: "virtual1", Host: "test", UniqID: "test"}}
	err = router.Send(pkg)
	require.Error(t, err, "Routing should fail after unregistration")
	assert.Contains(t, err.Error(), "not found")
}

func TestVirtualNodeManager_UnregisterNonexistent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	localNode := newMockNode("local")
	router := NewRouter(localNode, nil)
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	collector := &testEventCollector{}
	eventCh := make(chan event.Event, 10)
	_, err := bus.Subscribe(ctx, api.VirtualNodeSystem, eventCh)
	require.NoError(t, err)

	go func() {
		for e := range eventCh {
			collector.collect(e)
		}
	}()

	manager := NewVirtualNodeManager(router, bus, logger)
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	bus.Send(ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeDelete,
		Path:   "nonexistent",
	})

	time.Sleep(50 * time.Millisecond)

	acceptEvent := collector.findEvent(api.VirtualNodeAccept)
	require.NotNil(t, acceptEvent, "VirtualNodeAccept event should be sent even for nonexistent node")
	assert.Equal(t, "nonexistent", string(acceptEvent.Path))
}
