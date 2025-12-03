package boot

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
)

// mockEntryListener is a test implementation of registry.EntryListener
type mockEntryListener struct {
	addCount    int32
	updateCount int32
	deleteCount int32
	entries     []registry.Entry
}

func (m *mockEntryListener) Add(_ context.Context, e registry.Entry) error {
	atomic.AddInt32(&m.addCount, 1)
	m.entries = append(m.entries, e)
	return nil
}

func (m *mockEntryListener) Update(_ context.Context, _ registry.Entry) error {
	atomic.AddInt32(&m.updateCount, 1)
	return nil
}

func (m *mockEntryListener) Delete(_ context.Context, _ registry.Entry) error {
	atomic.AddInt32(&m.deleteCount, 1)
	return nil
}

func TestHandlerRegistry(t *testing.T) {
	t.Run("NewHandlerRegistry creates empty registry", func(t *testing.T) {
		reg := NewHandlerRegistry()
		require.NotNil(t, reg)

		handlers := reg.Handlers()
		assert.Empty(t, handlers)
	})

	t.Run("Register adds handler", func(t *testing.T) {
		reg := NewHandlerRegistry()

		var called int32
		handler := eventbus.NewBaseHandler(
			eventbus.Pattern{System: "test", Kind: "event"},
			func(_ context.Context, _ event.Event) error {
				atomic.AddInt32(&called, 1)
				return nil
			},
		)

		reg.Register(handler)

		handlers := reg.Handlers()
		assert.Len(t, handlers, 1)

		pattern := handlers[0].Pattern()
		assert.Equal(t, "test", pattern.System)
		assert.Equal(t, "event", pattern.Kind)

		// Test handler works
		err := handlers[0].Handle(context.Background(), event.Event{})
		assert.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&called))
	})

	t.Run("Register multiple handlers", func(t *testing.T) {
		reg := NewHandlerRegistry()

		handler1 := eventbus.NewBaseHandler(
			eventbus.Pattern{System: "sys1", Kind: "event.one"},
			func(_ context.Context, _ event.Event) error { return nil },
		)
		handler2 := eventbus.NewBaseHandler(
			eventbus.Pattern{System: "sys2", Kind: "event.two"},
			func(_ context.Context, _ event.Event) error { return nil },
		)

		reg.Register(handler1)
		reg.Register(handler2)

		handlers := reg.Handlers()
		assert.Len(t, handlers, 2)
	})

	t.Run("RegisterListener wraps entry listener", func(t *testing.T) {
		reg := NewHandlerRegistry()
		listener := &mockEntryListener{}

		reg.RegisterListener("process.*", listener)

		handlers := reg.Handlers()
		require.Len(t, handlers, 1)

		// Verify pattern matches registry events
		pattern := handlers[0].Pattern()
		assert.Equal(t, registry.System, pattern.System)
		assert.Equal(t, registry.AllEvents, pattern.Kind)
	})

	t.Run("RegisterListener with multiple listeners", func(t *testing.T) {
		reg := NewHandlerRegistry()

		listener1 := &mockEntryListener{}
		listener2 := &mockEntryListener{}

		reg.RegisterListener("process.*", listener1)
		reg.RegisterListener("http.*", listener2)

		handlers := reg.Handlers()
		assert.Len(t, handlers, 2)
	})
}

func TestHandlerRegistryContext(t *testing.T) {
	t.Run("WithHandlerRegistry stores registry in context", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		reg := NewHandlerRegistry()
		ctx = WithHandlerRegistry(ctx, reg)

		retrieved := GetHandlerRegistry(ctx)
		assert.Equal(t, reg, retrieved)
	})

	t.Run("GetHandlerRegistry returns nil when not set", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		retrieved := GetHandlerRegistry(ctx)
		assert.Nil(t, retrieved)
	})

	t.Run("GetHandlerRegistry returns nil without AppContext", func(t *testing.T) {
		ctx := context.Background()

		retrieved := GetHandlerRegistry(ctx)
		assert.Nil(t, retrieved)
	})

	t.Run("WithHandlerRegistry updates existing registry", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		reg1 := NewHandlerRegistry()
		listener1 := &mockEntryListener{}
		reg1.RegisterListener("event.one", listener1)

		ctx = WithHandlerRegistry(ctx, reg1)

		reg2 := NewHandlerRegistry()
		listener2 := &mockEntryListener{}
		reg2.RegisterListener("event.two", listener2)

		ctx = WithHandlerRegistry(ctx, reg2)

		retrieved := GetHandlerRegistry(ctx)
		handlers := retrieved.Handlers()
		assert.Len(t, handlers, 1)
	})
}

func TestWrapListener(t *testing.T) {
	t.Run("wrapListener creates EventHandler from entry listener", func(t *testing.T) {
		listener := &mockEntryListener{}

		handler := wrapListener("process.*", listener)

		pattern := handler.Pattern()
		assert.Equal(t, registry.System, pattern.System)
		assert.Equal(t, registry.AllEvents, pattern.Kind)
	})

	t.Run("wrapped listener handles Add events", func(t *testing.T) {
		listener := &mockEntryListener{}
		handler := wrapListener("process.*", listener)

		// Create context with event bus
		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		bus := eventbus.NewBus()
		ctx = event.WithBus(ctx, bus)

		testEntry := registry.Entry{
			ID:   registry.NewID("test", "entry"),
			Kind: "process.lua",
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   testEntry,
		}

		err := handler.Handle(ctx, evt)
		assert.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&listener.addCount))
	})

	t.Run("wrapped listener handles Update events", func(t *testing.T) {
		listener := &mockEntryListener{}
		handler := wrapListener("process.*", listener)

		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		bus := eventbus.NewBus()
		ctx = event.WithBus(ctx, bus)

		testEntry := registry.Entry{
			ID:   registry.NewID("test", "entry"),
			Kind: "process.lua",
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Update,
			Data:   testEntry,
		}

		err := handler.Handle(ctx, evt)
		assert.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&listener.updateCount))
	})

	t.Run("wrapped listener handles Delete events", func(t *testing.T) {
		listener := &mockEntryListener{}
		handler := wrapListener("process.*", listener)

		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		bus := eventbus.NewBus()
		ctx = event.WithBus(ctx, bus)

		testEntry := registry.Entry{
			ID:   registry.NewID("test", "entry"),
			Kind: "process.lua",
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Data:   testEntry,
		}

		err := handler.Handle(ctx, evt)
		assert.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&listener.deleteCount))
	})

	t.Run("wrapped listener filters by kind pattern", func(t *testing.T) {
		listener := &mockEntryListener{}
		handler := wrapListener("process.*", listener)

		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		bus := eventbus.NewBus()
		ctx = event.WithBus(ctx, bus)

		// Non-matching kind
		nonMatchEntry := registry.Entry{
			ID:   registry.NewID("test", "entry"),
			Kind: "http.endpoint",
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   nonMatchEntry,
		}

		err := handler.Handle(ctx, evt)
		assert.NoError(t, err)
		assert.Equal(t, int32(0), atomic.LoadInt32(&listener.addCount), "should not call listener for non-matching kind")
	})
}

func TestHandlerRegistryIntegration(t *testing.T) {
	t.Run("registry integration with bootstrap context", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)
		bus := eventbus.NewBus()
		ctx = event.WithBus(ctx, bus)

		reg := NewHandlerRegistry()

		listener1 := &mockEntryListener{}
		listener2 := &mockEntryListener{}
		reg.RegisterListener("process.*", listener1)
		reg.RegisterListener("http.*", listener2)

		ctx = WithHandlerRegistry(ctx, reg)

		// Verify retrieval
		retrieved := GetHandlerRegistry(ctx)
		require.NotNil(t, retrieved)

		handlers := retrieved.Handlers()
		assert.Len(t, handlers, 2)

		// Execute handlers with matching entries
		processEntry := registry.Entry{
			ID:   registry.NewID("test", "proc"),
			Kind: "process.lua",
		}
		httpEntry := registry.Entry{
			ID:   registry.NewID("test", "endpoint"),
			Kind: "http.endpoint",
		}

		processEvt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   processEntry,
		}
		httpEvt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   httpEntry,
		}

		for _, h := range handlers {
			_ = h.Handle(ctx, processEvt)
			_ = h.Handle(ctx, httpEvt)
		}

		assert.Equal(t, int32(1), atomic.LoadInt32(&listener1.addCount), "listener1 should receive process entry")
		assert.Equal(t, int32(1), atomic.LoadInt32(&listener2.addCount), "listener2 should receive http entry")
	})
}
