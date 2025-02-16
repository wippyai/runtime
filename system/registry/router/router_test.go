package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
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
	// Transaction tracking
	beginCalled   bool
	commitCalled  bool
	discardCalled bool
}

func (m *mockListener) Add(context.Context, registry.Entry) error {
	m.addCalled = true
	return m.returnError
}

func (m *mockListener) Update(context.Context, registry.Entry) error {
	m.updateCalled = true
	return m.returnError
}

func (m *mockListener) Delete(context.Context, registry.Entry) error {
	m.deleteCalled = true
	return m.returnError
}

// mockTransactionalListener extends mockListener to support transactions
type mockTransactionalListener struct {
	mockListener
}

func (m *mockTransactionalListener) Begin(context.Context) {
	m.beginCalled = true
}

func (m *mockTransactionalListener) Commit(context.Context) {
	m.commitCalled = true
}

func (m *mockTransactionalListener) Discard(context.Context) {
	m.discardCalled = true
}

// setupRouterTest creates a new router with a mock bus for testing
func setupRouterTest(t *testing.T) (*Router, *mockListener) {
	bus := eventbus.NewBus()
	mockListener := &mockListener{}

	router, err := NewRouter(context.Background(), bus,
		WithLogger(zap.NewNop()),
		WithDefaultListener(mockListener),
		WithListener("test.*", mockListener),
	)

	require.NoError(t, err)
	require.NotNil(t, router)

	return router, mockListener
}

// setupTransactionalRouterTest creates a new router with both regular and transactional listeners
func setupTransactionalRouterTest(t *testing.T) (*Router, *mockListener, *mockTransactionalListener) {
	bus := eventbus.NewBus()
	regularListener := &mockListener{}
	txListener := &mockTransactionalListener{}

	router, err := NewRouter(context.Background(), bus,
		WithLogger(zap.NewNop()),
		WithDefaultListener(regularListener),
		WithListener("test.*", regularListener),
		WithListener("tx.*", txListener),
	)

	require.NoError(t, err)
	require.NotNil(t, router)

	return router, regularListener, txListener
}

func TestNewRouter(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		bus := eventbus.NewBus()
		router, err := NewRouter(context.Background(), bus)

		require.NoError(t, err)
		require.NotNil(t, router)
		assert.NotNil(t, router.log)
		assert.NotNil(t, router.bus)
	})

	t.Run("with options", func(t *testing.T) {
		bus := eventbus.NewBus()
		mockListener := &mockListener{}
		logger := zap.NewNop()

		router, err := NewRouter(context.Background(), bus,
			WithLogger(logger),
			WithDefaultListener(mockListener),
			WithListener("test.*", mockListener),
		)

		require.NoError(t, err)
		require.NotNil(t, router)
		assert.Equal(t, logger, router.log)
		assert.Equal(t, mockListener, router.defaultListener)
		assert.Len(t, router.routes, 1)
	})

	t.Run("with canceled context", func(t *testing.T) {
		bus := eventbus.NewBus()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		router, err := NewRouter(ctx, bus)

		assert.Error(t, err)
		assert.Nil(t, router)
	})
}

func TestRouter_Stop(t *testing.T) {
	router, _ := setupRouterTest(t)

	err := router.Stop()
	assert.NoError(t, err)
}

func TestRouter_HandleEvent(t *testing.T) {
	t.Run("successful create event", func(t *testing.T) {
		router, listener := setupRouterTest(t)

		entry := registry.Entry{
			ID:   registry.ID{Name: "test-1"},
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
		router, listener := setupRouterTest(t)

		entry := registry.Entry{
			ID:   registry.ID{Name: "test-1"},
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
		router, listener := setupRouterTest(t)

		entry := registry.Entry{
			ID:   registry.ID{Name: "test-1"},
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
		router, listener := setupRouterTest(t)

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
		router, listener := setupRouterTest(t)

		entry := registry.Entry{
			ID:   registry.ID{Name: "test-1"},
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
		router, listener := setupRouterTest(t)
		listener.returnError = errors.New("listener error")

		entry := registry.Entry{
			ID:   registry.ID{Name: "test-1"},
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
		router, listener := setupRouterTest(t)
		listener.returnError = ErrSkipOperation

		entry := registry.Entry{
			ID:   registry.ID{Name: "test-1"},
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
		router, listener := setupRouterTest(t)

		found := router.findListener("test.resource")
		assert.Equal(t, listener, found)
	})

	t.Run("non-matching pattern", func(t *testing.T) {
		router, listener := setupRouterTest(t)

		found := router.findListener("other.resource")
		assert.Equal(t, listener, found) // Should return default listener
	})

	t.Run("multiple patterns", func(t *testing.T) {
		bus := eventbus.NewBus()
		listener1 := &mockListener{}
		listener2 := &mockListener{}

		router, err := NewRouter(context.Background(), bus,
			WithListener("test.*", listener1),
			WithListener("other.*", listener2),
		)

		require.NoError(t, err)

		found := router.findListener("test.resource")
		assert.Equal(t, listener1, found)

		found = router.findListener("other.resource")
		assert.Equal(t, listener2, found)
	})
}

func TestRouter_TransactionEvents(t *testing.T) {
	t.Run("begin transaction", func(t *testing.T) {
		router, regularListener, txListener := setupTransactionalRouterTest(t)

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Begin,
		})

		// Regular listener should not receive transaction events
		assert.False(t, regularListener.beginCalled)
		assert.False(t, regularListener.commitCalled)
		assert.False(t, regularListener.discardCalled)

		// Transaction listener should receive begin event
		assert.True(t, txListener.beginCalled)
		assert.False(t, txListener.commitCalled)
		assert.False(t, txListener.discardCalled)
	})

	t.Run("commit transaction", func(t *testing.T) {
		router, regularListener, txListener := setupTransactionalRouterTest(t)

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Commit,
		})

		// Regular listener should not receive transaction events
		assert.False(t, regularListener.beginCalled)
		assert.False(t, regularListener.commitCalled)
		assert.False(t, regularListener.discardCalled)

		// Transaction listener should receive commit event
		assert.False(t, txListener.beginCalled)
		assert.True(t, txListener.commitCalled)
		assert.False(t, txListener.discardCalled)
	})

	t.Run("discard transaction", func(t *testing.T) {
		router, regularListener, txListener := setupTransactionalRouterTest(t)

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Discard,
		})

		// Regular listener should not receive transaction events
		assert.False(t, regularListener.beginCalled)
		assert.False(t, regularListener.commitCalled)
		assert.False(t, regularListener.discardCalled)

		// Transaction listener should receive discard event
		assert.False(t, txListener.beginCalled)
		assert.False(t, txListener.commitCalled)
		assert.True(t, txListener.discardCalled)
	})

	t.Run("transaction with regular events", func(t *testing.T) {
		router, regularListener, txListener := setupTransactionalRouterTest(t)

		// Begin transaction
		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Begin,
		})

		// Regular event during transaction
		entry := registry.Entry{
			ID:   registry.ID{Name: "test-1"},
			Kind: "test.resource",
			Data: payload.New([]byte("test-data")),
		}

		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-1",
			Data:   entry,
		})

		// Commit transaction
		router.handleEvent(events.Event{
			System: registry.System,
			Kind:   registry.Commit,
		})

		// Regular listener should receive only the regular event
		assert.True(t, regularListener.addCalled)
		assert.False(t, regularListener.beginCalled)
		assert.False(t, regularListener.commitCalled)

		// Transaction listener should receive begin and commit
		assert.True(t, txListener.beginCalled)
		assert.True(t, txListener.commitCalled)
		assert.False(t, txListener.discardCalled)
	})
}

func TestRouter_ListenerDetection(t *testing.T) {
	t.Run("detect transaction listener in options", func(t *testing.T) {
		bus := eventbus.NewBus()
		txListener := &mockTransactionalListener{}

		router, err := NewRouter(context.Background(), bus,
			WithLogger(zap.NewNop()),
			WithDefaultListener(txListener),
			WithListener("tx.*", txListener),
		)

		require.NoError(t, err)
		require.NotNil(t, router)

		// Check if the router correctly detected the transaction listener
		assert.True(t, router.defaultTransactional)
		assert.True(t, router.routes[0].transactional)
	})

	t.Run("mix of regular and transaction listeners", func(t *testing.T) {
		bus := eventbus.NewBus()
		regularListener := &mockListener{}
		txListener := &mockTransactionalListener{}

		router, err := NewRouter(context.Background(), bus,
			WithLogger(zap.NewNop()),
			WithDefaultListener(regularListener),
			WithListener("regular.*", regularListener),
			WithListener("tx.*", txListener),
		)

		require.NoError(t, err)
		require.NotNil(t, router)

		// Check detection of different listener types
		assert.False(t, router.defaultTransactional)
		assert.False(t, router.routes[0].transactional)
		assert.True(t, router.routes[1].transactional)
	})
}
