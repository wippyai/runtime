// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	systempayload "github.com/wippyai/runtime/system/payload"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
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

func newWorkerTestTranscoder() payload.Transcoder {
	transcoder := systempayload.NewTranscoder()
	luapayload.RegisterAllBasicFormats(transcoder)
	msgpayload.Register(transcoder)
	return transcoder
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
	w, err := NewWorkerBuilder().
		WithLogger(logger).
		WithID(id).
		WithConfig(cfg).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()
	if err != nil {
		return nil, err
	}
	if f.workers == nil {
		f.workers = make(map[registry.ID]*Worker)
	}
	f.workers[id] = w
	return w, nil
}

func mustNewWorker(t *testing.T, id registry.ID, cfg *api.WorkerConfig) *Worker {
	t.Helper()

	worker, err := NewWorkerBuilder().
		WithLogger(zap.NewNop()).
		WithID(id).
		WithConfig(cfg).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()
	require.NoError(t, err)
	return worker
}

type failingFactory struct {
	err error
}

func (f *failingFactory) CreateWorker(_ context.Context, _ *zap.Logger, _ registry.ID, _ *api.WorkerConfig, _ resource.Registry) (*Worker, error) {
	return nil, f.err
}

type mockEnvRegistry struct{}

func (m *mockEnvRegistry) Get(_ context.Context, _ string) (string, error) { return "", nil }

func (m *mockEnvRegistry) Lookup(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil
}

func (m *mockEnvRegistry) Set(_ context.Context, _ string, _ string) error { return nil }

func (m *mockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (m *mockEnvRegistry) GetStorage(_ context.Context, _ registry.ID) (env.Storage, error) {
	return nil, nil
}

func (m *mockEnvRegistry) RegisterStorage(_ registry.ID, _ env.Storage) {}

func newTestManager(t *testing.T) (*Manager, *mockEventBus) {
	t.Helper()

	bus := &mockEventBus{}
	m, err := NewManager(
		WithLogger(zap.NewNop()),
		WithTranscoder(newWorkerTestTranscoder()),
		WithEventBus(bus),
		WithResourceRegistry(&mockResourceRegistry{}),
	)
	require.NoError(t, err)
	return m, bus
}

func TestNewWorker(t *testing.T) {
	id := registry.ID{Name: "test-worker"}
	config := &api.WorkerConfig{
		Client:    registry.ID{Name: "test-client"},
		TaskQueue: "test-queue",
	}

	w := mustNewWorker(t, id, config)

	require.NotNil(t, w)
	assert.Equal(t, id, w.id)
	assert.Equal(t, config, w.config)
	assert.NotNil(t, w.activities)
	assert.NotNil(t, w.workflows)
	assert.Empty(t, w.activities)
	assert.Empty(t, w.workflows)
}

func TestWorker_RegisterActivity(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

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
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterActivity(context.Background(), "test-activity", funcID)
	require.NoError(t, err)

	err = w.RegisterActivity(context.Background(), "test-activity", funcID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestWorker_RegisterLocalActivity(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

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
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterLocalActivity(context.Background(), "test-local", funcID)
	require.NoError(t, err)

	err = w.RegisterLocalActivity(context.Background(), "test-local", funcID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestWorker_UnregisterActivity(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	funcID := registry.ID{Name: "test-func"}
	err := w.RegisterActivity(context.Background(), "test-activity", funcID)
	require.NoError(t, err)

	err = w.UnregisterActivity("test-activity")
	require.NoError(t, err)
	assert.Empty(t, w.activities)
}

func TestWorker_UnregisterActivity_NotFound(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	err := w.UnregisterActivity("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorker_RegisterWorkflow(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

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
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	handler := func() {}
	err := w.RegisterWorkflow(context.Background(), "test-workflow", handler)
	require.NoError(t, err)

	err = w.RegisterWorkflow(context.Background(), "test-workflow", handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestWorker_UnregisterWorkflow(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	handler := func() {}
	err := w.RegisterWorkflow(context.Background(), "test-workflow", handler)
	require.NoError(t, err)

	err = w.UnregisterWorkflow("test-workflow")
	require.NoError(t, err)
	assert.Empty(t, w.workflows)
}

func TestWorker_UnregisterWorkflow_NotFound(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	err := w.UnregisterWorkflow("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorker_StartClosed(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})
	w.closed.Store(true)

	statusCh, err := w.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
	assert.Nil(t, statusCh)
}

func TestWorker_StopIdempotent(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})

	err := w.Stop(context.Background())
	require.NoError(t, err)

	err = w.Stop(context.Background())
	require.NoError(t, err)
}

func TestWorker_MultipleActivitiesAndWorkflows(t *testing.T) {
	w := mustNewWorker(t, registry.ID{Name: "test"}, &api.WorkerConfig{})
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

type testWorkerInterceptor struct {
	interceptor.WorkerInterceptorBase
	name string
}

func TestWorkerBuilder_Interceptors(t *testing.T) {
	i1 := &testWorkerInterceptor{name: "i1"}
	i2 := &testWorkerInterceptor{name: "i2"}

	w, err := NewWorkerBuilder().
		WithLogger(zap.NewNop()).
		WithID(registry.ID{Name: "test"}).
		WithConfig(&api.WorkerConfig{}).
		WithTranscoder(newWorkerTestTranscoder()).
		WithInterceptors([]interceptor.WorkerInterceptor{i1, i2}).
		Build()
	require.NoError(t, err)
	require.Len(t, w.interceptors, 2)
	assert.Equal(t, i1, w.interceptors[0])
	assert.Equal(t, i2, w.interceptors[1])
}

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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
	transcoder := newWorkerTestTranscoder()
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

func TestMapVersioningBehavior(t *testing.T) {
	assert.Equal(t, workflow.VersioningBehaviorPinned, mapVersioningBehavior(api.VersioningBehaviorPinned))
	assert.Equal(t, workflow.VersioningBehaviorAutoUpgrade, mapVersioningBehavior(api.VersioningBehaviorAutoUpgrade))
	assert.Equal(t, workflow.VersioningBehaviorUnspecified, mapVersioningBehavior(api.VersioningBehavior("unknown")))
}

func TestManager_NewManager_UsesEnvAndInterceptorsInDefaultFactory(t *testing.T) {
	envReg := &mockEnvRegistry{}
	workerInterceptors := []interceptor.WorkerInterceptor{
		&testWorkerInterceptor{name: "i1"},
		&testWorkerInterceptor{name: "i2"},
	}
	dtt := newWorkerTestTranscoder()

	m, err := NewManager(
		WithLogger(zap.NewNop()),
		WithTranscoder(dtt),
		WithEventBus(&mockEventBus{}),
		WithResourceRegistry(&mockResourceRegistry{}),
		WithEnvRegistry(envReg),
		WithInterceptors(workerInterceptors),
	)
	require.NoError(t, err)

	df, ok := m.factory.(*DefaultWorkerFactory)
	require.True(t, ok, "default factory should be used")
	assert.Same(t, envReg, df.envReg)
	assert.Same(t, dtt, df.dtt)
	assert.Equal(t, workerInterceptors, df.interceptors)
}

func TestManager_AddWorker_FactoryFailureRollsBackConfig(t *testing.T) {
	m, _ := newTestManager(t)
	m.factory = &failingFactory{err: errors.New("factory boom")}

	id := registry.ID{NS: "test", Name: "worker-fail-create"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}

	err := m.AddWorker(context.Background(), id, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create worker")
	assert.False(t, m.Has(id), "failed add must not leave config behind")

	m.mu.RLock()
	_, configExists := m.configs[id]
	_, serviceExists := m.services[id]
	m.mu.RUnlock()
	assert.False(t, configExists)
	assert.False(t, serviceExists)
}

func TestManager_AddWorker_ConfigAlreadyExists(t *testing.T) {
	m, _ := newTestManager(t)
	id := registry.ID{NS: "test", Name: "worker-existing-config"}
	m.configs[id] = &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}

	err := m.AddWorker(context.Background(), id, &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_AddAndUpdate_DecodeErrorOnMissingData(t *testing.T) {
	m, _ := newTestManager(t)
	ent := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "worker-missing-data"},
		Kind: api.Worker,
		Data: nil,
	}

	err := m.Add(context.Background(), ent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode worker config")

	err = m.Update(context.Background(), ent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode worker config")
}

func TestEnsureClientDependency(t *testing.T) {
	cfg := &api.WorkerConfig{
		Client: registry.ID{NS: "test", Name: "client"},
	}

	ensureClientDependency(cfg)
	require.Equal(t, []string{"test:client"}, cfg.Lifecycle.Requires)

	// Idempotent when called repeatedly.
	ensureClientDependency(cfg)
	require.Equal(t, []string{"test:client"}, cfg.Lifecycle.Requires)

	// Appends when there are existing dependencies.
	cfg.Lifecycle.Requires = []string{"service:a"}
	ensureClientDependency(cfg)
	require.Equal(t, []string{"service:a", "test:client"}, cfg.Lifecycle.Requires)

	// Migrates legacy dependencies into the canonical field.
	cfg.Lifecycle.Requires = nil
	cfg.Lifecycle.DependsOn = []string{"legacy:a"}
	ensureClientDependency(cfg)
	require.Equal(t, []string{"legacy:a", "test:client"}, cfg.Lifecycle.Requires)

	cfg.Lifecycle.Requires = []string{}
	cfg.Lifecycle.DependsOn = []string{"legacy:a"}
	ensureClientDependency(cfg)
	require.Equal(t, []string{"legacy:a", "test:client"}, cfg.Lifecycle.Requires)
}

func TestManager_DeleteWorker_EmitsRemoveEvents(t *testing.T) {
	m, bus := newTestManager(t)
	m.factory = &mockFactory{}

	id := registry.ID{NS: "test", Name: "worker-delete-events"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	require.NoError(t, m.AddWorker(context.Background(), id, cfg))

	before := len(bus.events)
	require.NoError(t, m.DeleteWorker(context.Background(), id))
	require.GreaterOrEqual(t, len(bus.events), before+2)

	removeEvent := bus.events[len(bus.events)-2]
	hostDeleteEvent := bus.events[len(bus.events)-1]

	assert.Equal(t, supervisor.System, removeEvent.System)
	assert.Equal(t, supervisor.ServiceRemove, removeEvent.Kind)
	assert.Equal(t, id.String(), removeEvent.Path)

	assert.Equal(t, relay.System, hostDeleteEvent.System)
	assert.Equal(t, relay.HostDelete, hostDeleteEvent.Kind)
	assert.Equal(t, id.String(), hostDeleteEvent.Path)
}
