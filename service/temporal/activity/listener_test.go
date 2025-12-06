package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
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

func (m *mockWorkerRegistry) RegisterActivity(_ context.Context, workerID registry.ID, activityName string, funcID registry.ID) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.activities[workerID.String()+":"+activityName] = funcID
	return nil
}

func (m *mockWorkerRegistry) RegisterLocalActivity(_ context.Context, workerID registry.ID, activityName string, funcID registry.ID) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.localActivities[workerID.String()+":"+activityName] = funcID
	return nil
}

func (m *mockWorkerRegistry) UnregisterActivity(_ context.Context, workerID registry.ID, activityName string) error {
	if m.unregisterErr != nil {
		return m.unregisterErr
	}
	delete(m.activities, workerID.String()+":"+activityName)
	delete(m.localActivities, workerID.String()+":"+activityName)
	return nil
}

func TestListener_ImplementsEntryListener(_ *testing.T) {
	var _ registry.EntryListener = (*Listener)(nil)
}

func TestListener_Add_RegisterActivity(t *testing.T) {
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
					},
				},
			},
		}

		err := listener.Add(context.Background(), entry)
		require.NoError(t, err)

		// Activity name is always full ID
		funcID, exists := workers.activities["app.workers:default:app.functions:my_func"]
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
						"local":  true,
					},
				},
			},
		}

		err := listener.Add(context.Background(), entry)
		require.NoError(t, err)

		funcID, exists := workers.localActivities["app.workers:default:app.functions:local_func"]
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
					},
				},
			},
		}

		err := listener.Add(context.Background(), entry)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.functions:default:app.functions:my_func"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})
}

func TestListener_Add_SkipNonActivity(t *testing.T) {
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

		err := listener.Add(context.Background(), entry)
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

		err := listener.Add(context.Background(), entry)
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

		err := listener.Add(context.Background(), entry)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})

	t.Run("skip function without worker specified", func(t *testing.T) {
		logger := zap.NewNop()
		workers := newMockWorkerRegistry()
		listener := NewListener(logger, workers)

		entry := registry.Entry{
			ID:   registry.NewID("app.functions", "my_func"),
			Kind: "function.lua",
			Meta: attrs.Bag{
				"temporal": map[string]any{
					"activity": map[string]any{},
				},
			},
		}

		err := listener.Add(context.Background(), entry)
		require.NoError(t, err)
		assert.Empty(t, workers.activities)
	})
}

func TestListener_Update(t *testing.T) {
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
					},
				},
			},
		}

		err := listener.Update(context.Background(), entry)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.workers:default:app.functions:my_func"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})
}

func TestListener_Delete(t *testing.T) {
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
					},
				},
			},
		}

		// First register
		err := listener.Add(context.Background(), entry)
		require.NoError(t, err)
		assert.Len(t, workers.activities, 1)

		// Then delete
		err = listener.Delete(context.Background(), entry)
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

		err := listener.Delete(context.Background(), entry)
		require.NoError(t, err)
	})
}

func TestListener_ProcessKind(t *testing.T) {
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
					},
				},
			},
		}

		err := listener.Add(context.Background(), entry)
		require.NoError(t, err)

		funcID, exists := workers.activities["app.workers:default:app.processes:my_process"]
		assert.True(t, exists)
		assert.Equal(t, entry.ID, funcID)
	})
}
