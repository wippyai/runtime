package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type mockWorkerRegistry struct {
	activities      map[string]registry.ID
	localActivities map[string]registry.ID
	registerErr     error
	unregisterErr   error
}

func newMockWorkerRegistry() *mockWorkerRegistry {
	return &mockWorkerRegistry{
		activities:      make(map[string]registry.ID),
		localActivities: make(map[string]registry.ID),
	}
}

func (m *mockWorkerRegistry) RegisterActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.activities[workerID.String()+":"+activityName] = funcID
	return nil
}

func (m *mockWorkerRegistry) RegisterLocalActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.localActivities[workerID.String()+":"+activityName] = funcID
	return nil
}

func (m *mockWorkerRegistry) UnregisterActivity(ctx context.Context, workerID registry.ID, activityName string) error {
	if m.unregisterErr != nil {
		return m.unregisterErr
	}
	delete(m.activities, workerID.String()+":"+activityName)
	delete(m.localActivities, workerID.String()+":"+activityName)
	return nil
}

func TestListener_Pattern(t *testing.T) {
	logger := zap.NewNop()
	workers := newMockWorkerRegistry()
	listener := NewListener(logger, workers)

	pattern := listener.Pattern()
	assert.Equal(t, registry.System, pattern.System)
	assert.Equal(t, registry.Changes, pattern.Kind)
}

func TestListener_Handle_RegisterActivity(t *testing.T) {
	t.Run("register function as activity", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "app.workers:default",
						"name":   "MyActivity",
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.workers:default:MyActivity"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})

	t.Run("register function as local activity", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "local_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "app.workers:default",
						"name":   "LocalActivity",
						"local":  true,
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)

		funcID, exists := workers.localActivities["app.workers:default:LocalActivity"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})

	t.Run("use function ID as default activity name", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "app.workers:default",
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.workers:default:app.functions:my_func"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})

	t.Run("use entry namespace as default worker namespace", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "default",
						"name":   "MyActivity",
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.functions:default:MyActivity"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})
}

func TestListener_Handle_SkipNonActivity(t *testing.T) {
	t.Run("skip non-function entries", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.config", "settings"),
			Kind: "config.yaml",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "app.workers:default",
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})

	t.Run("skip function without temporal metadata", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"description": "A function without temporal config",
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})

	t.Run("skip function without activity metadata", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"workflow": map[string]any{},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})

	t.Run("skip function without worker specified", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"name": "MyActivity",
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})

	t.Run("skip wrong event system", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		evt := event.Event{
			System: "other.system",
			Kind:   registry.Create,
			Data:   registry.Entry{},
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})

	t.Run("skip wrong event data type", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   "not an entry",
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})
}

func TestListener_Handle_Update(t *testing.T) {
	t.Run("update activity registration", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "app.workers:default",
						"name":   "MyActivity",
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Update,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.workers:default:MyActivity"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})
}

func TestListener_Handle_Delete(t *testing.T) {
	t.Run("unregister activity on delete", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "app.workers:default",
						"name":   "MyActivity",
					},
				},
			},
		}

		// First register
		createEvt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}
		err := listener.Handle(context.Background(), createEvt)
		require.NoError(t, err)
		assert.Len(t, workers.activities, 1)

		// Then delete
		deleteEvt := event.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Data:   entry,
		}
		err = listener.Handle(context.Background(), deleteEvt)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})

	t.Run("skip delete for non-activity", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: nil,
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)
	})
}

func TestListener_Handle_ProcessKind(t *testing.T) {
	t.Run("register process as activity", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.processes", "my_process"),
			Kind: "process.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{
						"worker": "app.workers:default",
						"name":   "ProcessActivity",
					},
				},
			},
		}

		evt := event.Event{
			System: registry.System,
			Kind:   registry.Create,
			Data:   entry,
		}

		err := listener.Handle(context.Background(), evt)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.workers:default:ProcessActivity"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})
}

func TestListener_Handle_UnknownEventKind(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workers := newMockWorkerRegistry()
	listener := NewListener(logger, workers)

	entry := registry.Entry{
		ID:   registry.NewID("app.functions", "my_func"),
		Kind: "function.lua",
		Meta: attrs.Bag{
			"temporal": map[string]any{
				"activity": map[string]any{
					"worker": "app.workers:default",
				},
			},
		},
	}

	evt := event.Event{
		System: registry.System,
		Kind:   "unknown.kind",
		Data:   entry,
	}

	err := listener.Handle(context.Background(), evt)
	require.NoError(t, err)
	assert.Empty(t, workers.activities)
}
