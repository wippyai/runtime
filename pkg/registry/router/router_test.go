package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockListener is a mock implementation of registry.EntryListener
type mockListener struct {
	addCalled    bool
	updateCalled bool
	deleteCalled bool
	returnError  error
}

func (m *mockListener) Add(ctx context.Context, entry registry.Entry) error {
	m.addCalled = true
	return m.returnError
}

func (m *mockListener) Update(ctx context.Context, entry registry.Entry) error {
	m.updateCalled = true
	return m.returnError
}

func (m *mockListener) Delete(ctx context.Context, entry registry.Entry) error {
	m.deleteCalled = true
	return m.returnError
}

// setupRouterTest creates a new router with a mock bus for testing
func setupRouterTest(t *testing.T) (*Router, *mockListener, *eventbus.Bus) {
	bus := eventbus.NewBus(zap.NewNop())
	mockListener := &mockListener{}

	router, err := NewRouter(context.Background(), bus,
		WithLogger(zap.NewNop()),
		WithDefaultListener(mockListener),
		WithKindListener("test.*", mockListener),
	)

	require.NoError(t, err)
	require.NotNil(t, router)

	return router, mockListener, bus
}

func TestNewRouter(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		bus := eventbus.NewBus(zap.NewNop())
		router, err := NewRouter(context.Background(), bus)

		require.NoError(t, err)
		require.NotNil(t, router)
		assert.NotNil(t, router.log)
		assert.NotNil(t, router.bus)
	})

	t.Run("with options", func(t *testing.T) {
		bus := eventbus.NewBus(zap.NewNop())
		mockListener := &mockListener{}
		logger := zap.NewNop()

		router, err := NewRouter(context.Background(), bus,
			WithLogger(logger),
			WithDefaultListener(mockListener),
			WithKindListener("test.*", mockListener),
		)

		require.NoError(t, err)
		require.NotNil(t, router)
		assert.Equal(t, logger, router.log)
		assert.Equal(t, mockListener, router.default_)
		assert.Len(t, router.routes, 1)
	})

	t.Run("with cancelled context", func(t *testing.T) {
		bus := eventbus.NewBus(zap.NewNop())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		router, err := NewRouter(ctx, bus)

		assert.Error(t, err)
		assert.Nil(t, router)
	})
}

func TestRouter_Stop(t *testing.T) {
	router, _, _ := setupRouterTest(t)

	err := router.Stop()
	assert.NoError(t, err)
}

func TestRouter_HandleEvent(t *testing.T) {
	t.Run("successful create event", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)

		entry := registry.Entry{
			ID:   "test-1",
			Kind: "test.resource",
			Data: payload.New([]byte("test-data")),
		}

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-1",
			Data:   entry,
		})

		assert.True(t, listener.addCalled)
		assert.False(t, listener.updateCalled)
		assert.False(t, listener.deleteCalled)

		// No need to verify sent events as we're using real eventbus now
	})

	t.Run("successful update event", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)

		entry := registry.Entry{
			ID:   "test-1",
			Kind: "test.resource",
			Data: payload.New([]byte("test-data")),
		}

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-1",
			Data:   entry,
		})

		assert.False(t, listener.addCalled)
		assert.True(t, listener.updateCalled)
		assert.False(t, listener.deleteCalled)
	})

	t.Run("successful delete event", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)

		entry := registry.Entry{
			ID:   "test-1",
			Kind: "test.resource",
		}

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-1",
			Data:   entry,
		})

		assert.False(t, listener.addCalled)
		assert.False(t, listener.updateCalled)
		assert.True(t, listener.deleteCalled)
	})

	t.Run("invalid event data", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-1",
			Data:   "invalid-data", // Not a registry.Entry
		})

		assert.False(t, listener.addCalled)
		assert.False(t, listener.updateCalled)
		assert.False(t, listener.deleteCalled)
	})

	t.Run("create/update without data", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)

		entry := registry.Entry{
			ID:   "test-1",
			Kind: "test.resource",
			Data: nil, // Missing required data
		}

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-1",
			Data:   entry,
		})

		assert.False(t, listener.addCalled)
	})

	t.Run("listener error", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)
		listener.returnError = errors.New("listener error")

		entry := registry.Entry{
			ID:   "test-1",
			Kind: "test.resource",
			Data: payload.New([]byte("test-data")),
		}

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-1",
			Data:   entry,
		})
	})

	t.Run("skip operation", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)
		listener.returnError = ErrSkipOperation

		entry := registry.Entry{
			ID:   "test-1",
			Kind: "test.resource",
			Data: payload.New([]byte("test-data")),
		}

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-1",
			Data:   entry,
		})
	})
}

func TestRouter_FindListener(t *testing.T) {
	t.Run("matching pattern", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)

		found := router.findListener("test.resource")
		assert.Equal(t, listener, found)
	})

	t.Run("non-matching pattern", func(t *testing.T) {
		router, listener, _ := setupRouterTest(t)

		found := router.findListener("other.resource")
		assert.Equal(t, listener, found) // Should return default listener
	})

	t.Run("multiple patterns", func(t *testing.T) {
		bus := eventbus.NewBus(zap.NewNop())
		listener1 := &mockListener{}
		listener2 := &mockListener{}

		router, err := NewRouter(context.Background(), bus,
			WithKindListener("test.*", listener1),
			WithKindListener("other.*", listener2),
		)

		require.NoError(t, err)

		found := router.findListener("test.resource")
		assert.Equal(t, listener1, found)

		found = router.findListener("other.resource")
		assert.Equal(t, listener2, found)
	})
}
