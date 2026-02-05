package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

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
	require.Equal(t, []string{"test:client"}, cfg.Lifecycle.DependsOn)

	// Idempotent when called repeatedly.
	ensureClientDependency(cfg)
	require.Equal(t, []string{"test:client"}, cfg.Lifecycle.DependsOn)

	// Appends when there are existing dependencies.
	cfg.Lifecycle.DependsOn = []string{"service:a"}
	ensureClientDependency(cfg)
	require.Equal(t, []string{"service:a", "test:client"}, cfg.Lifecycle.DependsOn)
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
