package relay

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// mockHost implements Host interface for testing
type mockHost struct {
	sendErr error
}

func (h *mockHost) Send(_ *relay.Package) error {
	return h.sendErr
}

func setupManagerTest() (*NodeManager, *Node, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	node := NewNode("test-node")
	manager := NewNodeManager(node, bus, logger)
	return manager, node, bus
}

func TestManager_StartStop(t *testing.T) {
	ctx := context.Background()
	manager, _, _ := setupManagerTest()

	// Test Serve
	err := manager.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, manager.subscriber)

	// Test close
	err = manager.Stop()
	require.NoError(t, err)
}

func TestManager_HandleRegisterHost(t *testing.T) {
	ctx := context.Background()
	manager, node, bus := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	// Create a channel to collect response events
	responses := make(chan event.Event, 2)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		relay.System,
		"host.(accept|reject)",
		func(e event.Event) {
			responses <- e
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	tests := []struct {
		name          string
		hostID        string
		host          any
		expectedKind  event.Kind
		expectedError string
	}{
		{
			name:         "successful registration",
			hostID:       "host1",
			host:         &mockHost{},
			expectedKind: relay.HostAccept,
		},
		{
			name:          "invalid host type",
			hostID:        "host2",
			host:          "invalid",
			expectedKind:  relay.HostReject,
			expectedError: "invalid host payload",
		},
		{
			name:          "duplicate host",
			hostID:        "host1",
			host:          &mockHost{},
			expectedKind:  relay.HostReject,
			expectedError: "already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// send register event
			bus.Send(ctx, event.Event{
				System: relay.System,
				Kind:   relay.HostRegister,
				Path:   tt.hostID,
				Data:   tt.host,
			})

			// Wait for response
			select {
			case resp := <-responses:
				assert.Equal(t, tt.expectedKind, resp.Kind)
				assert.Equal(t, tt.hostID, resp.Path)
				if tt.expectedError != "" {
					if errStr, ok := resp.Data.(string); ok {
						assert.Contains(t, errStr, tt.expectedError)
					} else {
						t.Errorf("expected error string, got %T", resp.Data)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for response")
			}

			if tt.expectedKind == relay.HostAccept {
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
	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		relay.System,
		"host.accept",
		func(e event.Event) {
			responses <- e
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// send delete event
	bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostDelete,
		Path:   "host1",
	})

	// Wait for response
	select {
	case resp := <-responses:
		assert.Equal(t, relay.HostAccept, resp.Kind)
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

	// send unknown event
	bus.Send(ctx, event.Event{
		System: relay.System,
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

	t.Run("send to unregistered host returns error", func(t *testing.T) {
		pid := pidapi.PID{
			Node:   "test-node",
			Host:   "nonexistent-host",
			UniqID: "test",
		}

		pkg := &relay.Package{
			Target: pid,
			Messages: []*relay.Message{
				{Topic: "test"},
			},
		}

		err := manager.Node().Send(pkg)
		assert.Error(t, err)
	})

	t.Run("send to registered host succeeds", func(t *testing.T) {
		host := &mockHost{}
		require.NoError(t, node.RegisterHost("test-host", host))

		pid := pidapi.PID{
			Node:   "test-node",
			Host:   "test-host",
			UniqID: "test",
		}

		pkg := &relay.Package{
			Target: pid,
			Messages: []*relay.Message{
				{Topic: "test"},
			},
		}

		err := manager.Node().Send(pkg)
		assert.NoError(t, err)
	})
}

func TestManager_Attach(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager, node, _ := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	// Register an attachable mailbox first
	attachableMailbox := NewMailbox(ctx, WithWorkerCount(1), WithBufferSize(10))
	require.NoError(t, node.RegisterHost("test-host", attachableMailbox))

	pid := pidapi.PID{
		Node:   "test-node",
		Host:   "test-host",
		UniqID: "test",
	}
	ch := make(chan *relay.Package, 1)

	detach, err := manager.Node().Attach(pid, ch)
	require.NoError(t, err)
	assert.NotNil(t, detach)

	// Test cancel function
	detach()
}

func TestManager_Node(t *testing.T) {
	ctx := context.Background()
	manager, node, _ := setupManagerTest()
	require.NoError(t, manager.Start(ctx))
	defer func() { assert.NoError(t, manager.Stop()) }()

	// Test that Node() returns the underlying node
	assert.Equal(t, node, manager.Node())
}
