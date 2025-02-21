package pubsub

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

// mockNode implements Node interface for testing
type mockNode struct {
	nodeID  string
	hosts   sync.Map
	sendErr error
}

func newMockNode(id string) *mockNode {
	return &mockNode{
		nodeID: id,
	}
}

func (n *mockNode) ID() string {
	return n.nodeID
}

func (n *mockNode) RegisterHost(id string, host api.Host) error {
	_, loaded := n.hosts.LoadOrStore(id, host)
	if loaded {
		return api.ErrHostAlreadyExists
	}
	return nil
}

func (n *mockNode) UnregisterHost(id string) {
	n.hosts.Delete(id)
}

func (n *mockNode) Send(ctx context.Context, pkg *api.Package) error {
	return n.sendErr
}

func (n *mockNode) Attach(pid api.PID, ch chan *api.Package) (context.CancelFunc, error) {
	return func() {}, nil
}

func (n *mockNode) Detach(pid api.PID) {
	// No-op for testing
}

// mockHost implements Host interface for testing
type mockHost struct {
	sendErr error
}

func (h *mockHost) Send(ctx context.Context, pkg *api.Package) error {
	return h.sendErr
}

func (h *mockHost) Attach(pid api.PID, ch chan *api.Package) (context.CancelFunc, error) {
	return func() {}, nil
}

func (h *mockHost) Detach(pid api.PID) {
	// No-op for testing
}

func setupManagerTest() (*NodeManager, *mockNode, events.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	node := newMockNode("test-node")
	manager := NewNodeManager(node, bus, logger)
	return manager, node, bus
}

func TestManager_StartStop(t *testing.T) {
	ctx := context.Background()
	manager, _, _ := setupManagerTest()

	// Test Start
	err := manager.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, manager.subscriber)

	// Test Close
	err = manager.Stop()
	require.NoError(t, err)
}

func TestManager_HandleRegisterHost(t *testing.T) {
	ctx := context.Background()
	manager, node, bus := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	// Create a channel to collect response events
	responses := make(chan events.Event, 2)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		api.System,
		"node.(accept_host|reject_host)",
		func(e events.Event) {
			responses <- e
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	tests := []struct {
		name          string
		hostID        string
		host          interface{}
		expectedKind  events.Kind
		expectedError string
	}{
		{
			name:         "successful registration",
			hostID:       "host1",
			host:         &mockHost{},
			expectedKind: api.AcceptHost,
		},
		{
			name:          "invalid host type",
			hostID:        "host2",
			host:          "invalid",
			expectedKind:  api.RejectHost,
			expectedError: "invalid host payload",
		},
		{
			name:          "duplicate host",
			hostID:        "host1",
			host:          &mockHost{},
			expectedKind:  api.RejectHost,
			expectedError: "host already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Send register event
			bus.Send(ctx, events.Event{
				System: api.System,
				Kind:   api.RegisterHost,
				Path:   tt.hostID,
				Data:   tt.host,
			})

			// Wait for response
			select {
			case resp := <-responses:
				assert.Equal(t, tt.expectedKind, resp.Kind)
				assert.Equal(t, tt.hostID, resp.Path)
				if tt.expectedError != "" {
					assert.Equal(t, tt.expectedError, resp.Data)
				}
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for response")
			}

			if tt.expectedKind == api.AcceptHost {
				// Verify host was registered
				_, exists := node.hosts.Load(tt.hostID)
				assert.True(t, exists)
			}
		})
	}
}

func TestManager_HandleDeleteHost(t *testing.T) {
	ctx := context.Background()
	manager, node, bus := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	// Pre-register a host
	host := &mockHost{}
	assert.NoError(t, node.RegisterHost("host1", host))

	// Create a channel to collect response events
	responses := make(chan events.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		api.System,
		"node.accept_host",
		func(e events.Event) {
			responses <- e
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Send delete event
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.DeleteHost,
		Path:   "host1",
	})

	// Wait for response
	select {
	case resp := <-responses:
		assert.Equal(t, api.AcceptHost, resp.Kind)
		assert.Equal(t, "host1", resp.Path)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Verify host was removed
	_, exists := node.hosts.Load("host1")
	assert.False(t, exists)
}

func TestManager_HandleUnknownEvent(t *testing.T) {
	ctx := context.Background()
	manager, _, bus := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	// Send unknown event
	bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   "unknown.event",
		Path:   "test",
	})

	// No panic should occur
	time.Sleep(10 * time.Millisecond)
}

func TestManager_Send(t *testing.T) {
	ctx := context.Background()
	manager, node, _ := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	tests := []struct {
		name        string
		sendErr     error
		shouldError bool
	}{
		{
			name:        "successful send",
			sendErr:     nil,
			shouldError: false,
		},
		{
			name:        "send error",
			sendErr:     api.ErrHostNotFound,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node.sendErr = tt.sendErr

			pid := api.PID{
				Node:   "test-node",
				Host:   "test-host",
				ID:     registry.ID{NS: "test", Name: "proc"},
				UniqID: "test",
			}

			pkg := &api.Package{
				PID: pid,
				Messages: []*api.Message{
					{Topic: "test"},
				},
			}

			err := manager.Send(ctx, pkg)

			if tt.shouldError {
				assert.Error(t, err)
				assert.Equal(t, tt.sendErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestManager_Attach(t *testing.T) {
	ctx := context.Background()
	manager, _, _ := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	pid := api.PID{
		Node:   "test-node",
		Host:   "test-host",
		ID:     registry.ID{NS: "test", Name: "proc"},
		UniqID: "test",
	}
	ch := make(chan *api.Package)

	cancel, err := manager.Attach(pid, ch)
	require.NoError(t, err)
	assert.NotNil(t, cancel)

	// Test cancel function
	cancel()
}

func TestManager_Node(t *testing.T) {
	ctx := context.Background()
	manager, node, _ := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	// Test that Node() returns the underlying node
	assert.Equal(t, node, manager.Node())
}
