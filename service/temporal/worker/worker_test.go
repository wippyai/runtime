package worker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.uber.org/zap"
)

// mockEventBus implements event.Bus for testing
type mockEventBus struct {
	events []event.Event
}

func (m *mockEventBus) Send(_ context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "sub-1", nil
}

func (m *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "sub-1", nil
}

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

// mockTranscoder implements payload.Transcoder for testing
type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	return nil
}

// mockResourceRegistry implements resource.Registry for testing
type mockResourceRegistry struct{}

func (m *mockResourceRegistry) Acquire(_ context.Context, _ registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	return nil, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) { return nil, nil }
func (m *mockResourceRegistry) Exists(_ registry.ID) bool    { return false }

// mockFactory implements Factory for testing
type mockFactory struct {
	workers map[registry.ID]*Worker
}

func (f *mockFactory) CreateWorker(_ context.Context, logger *zap.Logger, id registry.ID, cfg *api.WorkerConfig, _ resource.Registry) (*Worker, error) {
	w := NewWorker(logger, id, cfg, nil, nil, nil)
	if f.workers == nil {
		f.workers = make(map[registry.ID]*Worker)
	}
	f.workers[id] = w
	return w, nil
}

func TestNewWorker(t *testing.T) {
	logger := zap.NewNop()
	id := registry.ID{Name: "test-worker"}
	config := &api.WorkerConfig{
		Client:    registry.ID{Name: "test-client"},
		TaskQueue: "test-queue",
	}

	w := NewWorker(logger, id, config, nil, nil, nil)

	require.NotNil(t, w)
	assert.Equal(t, id, w.id)
	assert.Equal(t, config, w.config)
	assert.NotNil(t, w.activities)
	assert.NotNil(t, w.workflows)
	assert.Empty(t, w.activities)
	assert.Empty(t, w.workflows)
}

func TestWorker_RegisterActivity(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterActivity(context.Background(), "test-activity", funcID)

	require.NoError(t, err)
	assert.Len(t, w.activities, 1)

	reg, exists := w.activities["test-activity"]
	require.True(t, exists)
	assert.Equal(t, "test-activity", reg.name)
	assert.Equal(t, funcID, reg.function)
	assert.False(t, reg.local)
}

func TestWorker_RegisterActivity_Duplicate(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterActivity(context.Background(), "test-activity", funcID)
	require.NoError(t, err)

	err = w.RegisterActivity(context.Background(), "test-activity", funcID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestWorker_RegisterLocalActivity(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterLocalActivity(context.Background(), "test-local", funcID)

	require.NoError(t, err)
	assert.Len(t, w.activities, 1)

	reg, exists := w.activities["test-local"]
	require.True(t, exists)
	assert.Equal(t, "test-local", reg.name)
	assert.Equal(t, funcID, reg.function)
	assert.True(t, reg.local)
}

func TestWorker_RegisterLocalActivity_Duplicate(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterLocalActivity(context.Background(), "test-local", funcID)
	require.NoError(t, err)

	err = w.RegisterLocalActivity(context.Background(), "test-local", funcID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestWorker_UnregisterActivity(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterActivity(context.Background(), "test-activity", funcID)
	require.NoError(t, err)

	err = w.UnregisterActivity("test-activity")
	require.NoError(t, err)
	assert.Empty(t, w.activities)
}

func TestWorker_UnregisterActivity_NotFound(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	err := w.UnregisterActivity("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorker_RegisterWorkflow(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	handler := func() {}
	err := w.RegisterWorkflow(context.Background(), "test-workflow", handler)

	require.NoError(t, err)
	assert.Len(t, w.workflows, 1)

	reg, exists := w.workflows["test-workflow"]
	require.True(t, exists)
	assert.Equal(t, "test-workflow", reg.name)
	assert.NotNil(t, reg.handler)
}

func TestWorker_RegisterWorkflow_Duplicate(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	handler := func() {}
	err := w.RegisterWorkflow(context.Background(), "test-workflow", handler)
	require.NoError(t, err)

	err = w.RegisterWorkflow(context.Background(), "test-workflow", handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestWorker_UnregisterWorkflow(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	handler := func() {}
	err := w.RegisterWorkflow(context.Background(), "test-workflow", handler)
	require.NoError(t, err)

	err = w.UnregisterWorkflow("test-workflow")
	require.NoError(t, err)
	assert.Empty(t, w.workflows)
}

func TestWorker_UnregisterWorkflow_NotFound(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	err := w.UnregisterWorkflow("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorker_StartClosed(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)
	w.closed.Store(true)

	statusCh, err := w.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
	assert.Nil(t, statusCh)
}

func TestWorker_StopIdempotent(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)

	err := w.Stop(context.Background())
	require.NoError(t, err)

	err = w.Stop(context.Background())
	require.NoError(t, err)
}

func TestWorker_MultipleActivitiesAndWorkflows(t *testing.T) {
	w := NewWorker(zap.NewNop(), registry.ID{Name: "test"}, &api.WorkerConfig{}, nil, nil, nil)
	ctx := context.Background()

	// Register multiple activities
	for i := 0; i < 5; i++ {
		name := "activity-" + string(rune('a'+i))
		funcID := registry.ID{Name: name + "-func"}
		err := w.RegisterActivity(ctx, name, funcID)
		require.NoError(t, err)
	}

	// Register multiple local activities
	for i := 0; i < 3; i++ {
		name := "local-" + string(rune('a'+i))
		funcID := registry.ID{Name: name + "-func"}
		err := w.RegisterLocalActivity(ctx, name, funcID)
		require.NoError(t, err)
	}

	// Register multiple workflows
	for i := 0; i < 4; i++ {
		name := "workflow-" + string(rune('a'+i))
		err := w.RegisterWorkflow(ctx, name, func() {})
		require.NoError(t, err)
	}

	assert.Len(t, w.activities, 8)
	assert.Len(t, w.workflows, 4)

	// Count local vs regular activities
	localCount := 0
	regularCount := 0
	for _, act := range w.activities {
		if act.local {
			localCount++
		} else {
			regularCount++
		}
	}
	assert.Equal(t, 3, localCount)
	assert.Equal(t, 5, regularCount)
}

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	t.Run("creates manager with valid params", func(t *testing.T) {
		m, err := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)
		require.NoError(t, err)
		require.NotNil(t, m)
	})

	t.Run("fails without logger", func(t *testing.T) {
		_, err := NewManager(
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "logger")
	})

	t.Run("fails without transcoder", func(t *testing.T) {
		_, err := NewManager(
			WithLogger(logger),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transcoder")
	})

	t.Run("fails without bus", func(t *testing.T) {
		_, err := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithResourceRegistry(resourceReg),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "event bus")
	})

	t.Run("fails without resource registry", func(t *testing.T) {
		_, err := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource registry")
	})
}

func TestManager_AddWorker(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	t.Run("adds worker successfully", func(t *testing.T) {
		m, _ := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)
		m.factory = &mockFactory{}

		id := registry.ID{NS: "test", Name: "worker-1"}
		cfg := &api.WorkerConfig{
			Client:    registry.ID{NS: "test", Name: "client"},
			TaskQueue: "test-queue",
		}

		err := m.AddWorker(context.Background(), id, cfg)
		require.NoError(t, err)

		// Verify worker was stored
		assert.True(t, m.Has(id))
		w, err := m.GetWorker(id)
		require.NoError(t, err)
		require.NotNil(t, w)
	})

	t.Run("fails on duplicate worker", func(t *testing.T) {
		m, _ := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)
		m.factory = &mockFactory{}

		id := registry.ID{NS: "test", Name: "worker-dup"}
		cfg := &api.WorkerConfig{
			Client:    registry.ID{NS: "test", Name: "client"},
			TaskQueue: "test-queue",
		}

		err := m.AddWorker(context.Background(), id, cfg)
		require.NoError(t, err)

		err = m.AddWorker(context.Background(), id, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already")
	})

	t.Run("fails on invalid config", func(t *testing.T) {
		m, _ := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)

		id := registry.ID{NS: "test", Name: "worker-invalid"}
		cfg := &api.WorkerConfig{
			Client:    registry.ID{NS: "test", Name: "client"},
			TaskQueue: "", // Invalid - empty task queue
		}

		err := m.AddWorker(context.Background(), id, cfg)
		require.Error(t, err)
	})
}

func TestManager_UpdateWorker(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	t.Run("updates existing worker", func(t *testing.T) {
		m, _ := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)
		m.factory = &mockFactory{}

		id := registry.ID{NS: "test", Name: "worker-update"}
		cfg := &api.WorkerConfig{
			Client:    registry.ID{NS: "test", Name: "client"},
			TaskQueue: "test-queue",
		}

		err := m.AddWorker(context.Background(), id, cfg)
		require.NoError(t, err)

		// Update config
		newCfg := &api.WorkerConfig{
			Client:    registry.ID{NS: "test", Name: "client"},
			TaskQueue: "updated-queue",
		}
		err = m.UpdateWorker(context.Background(), id, newCfg)
		require.NoError(t, err)

		storedCfg, exists := m.GetConfig(id)
		require.True(t, exists)
		assert.Equal(t, "updated-queue", storedCfg.TaskQueue)
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		m, _ := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)

		id := registry.ID{NS: "test", Name: "nonexistent"}
		cfg := &api.WorkerConfig{
			Client:    registry.ID{NS: "test", Name: "client"},
			TaskQueue: "test-queue",
		}

		err := m.UpdateWorker(context.Background(), id, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestManager_DeleteWorker(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	t.Run("deletes existing worker", func(t *testing.T) {
		m, _ := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)
		m.factory = &mockFactory{}

		id := registry.ID{NS: "test", Name: "worker-delete"}
		cfg := &api.WorkerConfig{
			Client:    registry.ID{NS: "test", Name: "client"},
			TaskQueue: "test-queue",
		}

		err := m.AddWorker(context.Background(), id, cfg)
		require.NoError(t, err)
		assert.True(t, m.Has(id))

		err = m.DeleteWorker(context.Background(), id)
		require.NoError(t, err)
		assert.False(t, m.Has(id))
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		m, _ := NewManager(
			WithLogger(logger),
			WithTranscoder(transcoder),
			WithEventBus(bus),
			WithResourceRegistry(resourceReg),
		)

		id := registry.ID{NS: "test", Name: "nonexistent"}
		err := m.DeleteWorker(context.Background(), id)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestManager_GetWorker(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)
	m.factory = &mockFactory{}

	id := registry.ID{NS: "test", Name: "worker-get"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}

	_ = m.AddWorker(context.Background(), id, cfg)

	t.Run("gets existing worker", func(t *testing.T) {
		w, err := m.GetWorker(id)
		require.NoError(t, err)
		require.NotNil(t, w)
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		_, err := m.GetWorker(registry.ID{Name: "nonexistent"})
		require.Error(t, err)
	})
}

func TestManager_RegisterActivity(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)
	m.factory = &mockFactory{}

	workerID := registry.ID{NS: "test", Name: "worker-activity"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	_ = m.AddWorker(context.Background(), workerID, cfg)

	t.Run("registers activity", func(t *testing.T) {
		funcID := registry.ID{NS: "funcs", Name: "my-activity"}
		err := m.RegisterActivity(context.Background(), workerID, "test-activity", funcID)
		require.NoError(t, err)
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		funcID := registry.ID{NS: "funcs", Name: "my-activity"}
		err := m.RegisterActivity(context.Background(), registry.ID{Name: "nonexistent"}, "test", funcID)
		require.Error(t, err)
	})
}

func TestManager_RegisterLocalActivity(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)
	m.factory = &mockFactory{}

	workerID := registry.ID{NS: "test", Name: "worker-local"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	_ = m.AddWorker(context.Background(), workerID, cfg)

	t.Run("registers local activity", func(t *testing.T) {
		funcID := registry.ID{NS: "funcs", Name: "my-local"}
		err := m.RegisterLocalActivity(context.Background(), workerID, "test-local", funcID)
		require.NoError(t, err)
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		funcID := registry.ID{NS: "funcs", Name: "my-local"}
		err := m.RegisterLocalActivity(context.Background(), registry.ID{Name: "nonexistent"}, "test", funcID)
		require.Error(t, err)
	})
}

func TestManager_UnregisterActivity(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)
	m.factory = &mockFactory{}

	workerID := registry.ID{NS: "test", Name: "worker-unreg"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	_ = m.AddWorker(context.Background(), workerID, cfg)

	funcID := registry.ID{NS: "funcs", Name: "my-activity"}
	_ = m.RegisterActivity(context.Background(), workerID, "test-activity", funcID)

	t.Run("unregisters activity", func(t *testing.T) {
		err := m.UnregisterActivity(context.Background(), workerID, "test-activity")
		require.NoError(t, err)
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		err := m.UnregisterActivity(context.Background(), registry.ID{Name: "nonexistent"}, "test")
		require.Error(t, err)
	})
}

func TestManager_RegisterWorkflow(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)
	m.factory = &mockFactory{}

	workerID := registry.ID{NS: "test", Name: "worker-wf"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	_ = m.AddWorker(context.Background(), workerID, cfg)

	t.Run("registers workflow", func(t *testing.T) {
		handler := func() {}
		err := m.RegisterWorkflow(context.Background(), workerID, "test-workflow", handler)
		require.NoError(t, err)
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		handler := func() {}
		err := m.RegisterWorkflow(context.Background(), registry.ID{Name: "nonexistent"}, "test", handler)
		require.Error(t, err)
	})
}

func TestManager_UnregisterWorkflow(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)
	m.factory = &mockFactory{}

	workerID := registry.ID{NS: "test", Name: "worker-unreg-wf"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	_ = m.AddWorker(context.Background(), workerID, cfg)

	handler := func() {}
	_ = m.RegisterWorkflow(context.Background(), workerID, "test-workflow", handler)

	t.Run("unregisters workflow", func(t *testing.T) {
		err := m.UnregisterWorkflow(context.Background(), workerID, "test-workflow")
		require.NoError(t, err)
	})

	t.Run("fails for non-existent worker", func(t *testing.T) {
		err := m.UnregisterWorkflow(context.Background(), registry.ID{Name: "nonexistent"}, "test")
		require.Error(t, err)
	})
}

func TestManager_Add_WrongKind(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)

	entry := registry.Entry{
		ID:   registry.ID{Name: "test"},
		Kind: "wrong.kind",
	}

	err := m.Add(context.Background(), entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected entry kind")
}

func TestManager_Update_WrongKind(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)

	entry := registry.Entry{
		ID:   registry.ID{Name: "test"},
		Kind: "wrong.kind",
	}

	err := m.Update(context.Background(), entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected entry kind")
}

func TestManager_Delete_WrongKind(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := &mockTranscoder{}
	resourceReg := &mockResourceRegistry{}

	m, _ := NewManager(
		WithLogger(logger),
		WithTranscoder(transcoder),
		WithEventBus(bus),
		WithResourceRegistry(resourceReg),
	)

	entry := registry.Entry{
		ID:   registry.ID{Name: "test"},
		Kind: "wrong.kind",
	}

	err := m.Delete(context.Background(), entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected entry kind")
}
