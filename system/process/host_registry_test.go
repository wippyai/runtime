package process

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/process"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Mock implementations
type mockManagedHost struct{}

func (m *mockManagedHost) Send(msg *pubsub.Package) error {
	return nil
}

func (m *mockManagedHost) Terminate(ctx context.Context, pid pubsub.PID) error {
	return nil
}

func (m *mockManagedHost) Launch(ctx context.Context, launch *process.Launch) (pubsub.PID, error) {
	return pubsub.PID{}, nil
}

type mockDelegatedHost struct{}

func (m *mockDelegatedHost) Send(msg *pubsub.Package) error {
	return nil
}

func (m *mockDelegatedHost) Terminate(ctx context.Context, pid pubsub.PID) error {
	return nil
}

// Updated to match the Delegated interface with Lifecycle parameter
func (m *mockDelegatedHost) Launch(ctx context.Context, pid pubsub.PID, lf process.Lifecycle, input payload.Payloads) (pubsub.PID, error) {
	return pubsub.PID{}, nil
}

type invalidHost struct{}

func newTestHostRegistry(t *testing.T) (*HostRegistry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	registry := NewHostRegistry(bus, logger)
	return registry, bus
}

func TestHostRegistry_StartStop(t *testing.T) {
	ctx := context.Background()
	registry, _ := newTestHostRegistry(t)

	err := registry.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, registry.subscriber)

	err = registry.Stop()
	require.NoError(t, err)
}

func TestHostRegistry_RegisterHost(t *testing.T) {
	ctx := context.Background()
	hostRegistry, bus := newTestHostRegistry(t)
	require.NoError(t, hostRegistry.Start(ctx))
	defer func() { assert.NoError(t, hostRegistry.Stop()) }()

	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		process.HostSystem,
		"hosts.*",
		func(evt event.Event) {
			if evt.Kind == process.HostAccept || evt.Kind == process.HostReject {
				responses <- evt
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	t.Run("register managed host", func(t *testing.T) {
		host := &mockManagedHost{}
		bus.Send(ctx, event.Event{
			System: process.HostSystem,
			Kind:   process.HostRegister,
			Path:   "test:managed-host",
			Data:   host,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.HostAccept, resp.Kind)
			assert.Equal(t, "test:managed-host", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify host was registered
		registeredHost, exists := hostRegistry.GetHost("test:managed-host")
		assert.True(t, exists)
		assert.NotNil(t, registeredHost)
		_, ok := registeredHost.(process.Managed)
		assert.True(t, ok)
	})

	t.Run("register delegated host", func(t *testing.T) {
		host := &mockDelegatedHost{}
		bus.Send(ctx, event.Event{
			System: process.HostSystem,
			Kind:   process.HostRegister,
			Path:   "test:delegated-host",
			Data:   host,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.HostAccept, resp.Kind)
			assert.Equal(t, "test:delegated-host", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify host was registered
		registeredHost, exists := hostRegistry.GetHost("test:delegated-host")
		assert.True(t, exists)
		assert.NotNil(t, registeredHost)
		_, ok := registeredHost.(process.Delegated)
		assert.True(t, ok)
	})

	t.Run("register invalid host type", func(t *testing.T) {
		host := &invalidHost{}
		bus.Send(ctx, event.Event{
			System: process.HostSystem,
			Kind:   process.HostRegister,
			Path:   "test:invalid-host",
			Data:   host,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.HostReject, resp.Kind)
			assert.Equal(t, "test:invalid-host", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify host was not registered
		_, exists := hostRegistry.GetHost("test:invalid-host")
		assert.False(t, exists)
	})

	t.Run("register with invalid payload", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: process.HostSystem,
			Kind:   process.HostRegister,
			Path:   "test:invalid-payload",
			Data:   "invalid data",
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.HostReject, resp.Kind)
			assert.Equal(t, "test:invalid-payload", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})
}

func TestHostRegistry_DeleteHost(t *testing.T) {
	ctx := context.Background()
	hostRegistry, bus := newTestHostRegistry(t)
	require.NoError(t, hostRegistry.Start(ctx))
	defer func() { assert.NoError(t, hostRegistry.Stop()) }()

	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		process.HostSystem,
		"hosts.*",
		func(evt event.Event) {
			if evt.Kind == process.HostAccept || evt.Kind == process.HostReject {
				responses <- evt
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register a test host first
	hostID := "test:managed-host"
	host := &mockManagedHost{}
	bus.Send(ctx, event.Event{
		System: process.HostSystem,
		Kind:   process.HostRegister,
		Path:   hostID,
		Data:   host,
	})

	select {
	case resp := <-responses:
		assert.Equal(t, process.HostAccept, resp.Kind)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for registration response")
	}

	t.Run("successful deletion", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: process.HostSystem,
			Kind:   process.HostDelete,
			Path:   hostID,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.HostAccept, resp.Kind)
			assert.Equal(t, hostID, resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify host was deleted
		_, exists := hostRegistry.GetHost(hostID)
		assert.False(t, exists)
	})

	t.Run("delete non-existent host", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: process.HostSystem,
			Kind:   process.HostDelete,
			Path:   "test:nonexistent",
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.HostReject, resp.Kind)
			assert.Equal(t, "test:nonexistent", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})
}
