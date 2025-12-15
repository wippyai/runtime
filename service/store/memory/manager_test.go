package memory

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	memstore "github.com/wippyai/runtime/api/service/store/memory"
	"github.com/wippyai/runtime/api/supervisor"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

type mockBus struct {
	mu     sync.Mutex
	events []event.Event
}

func (m *mockBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func (m *mockBus) Send(_ context.Context, e event.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockBus) getEvents() []event.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events
}

func (m *mockBus) clearEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
}

func newTestManager(t *testing.T) (*Manager, *mockBus, payload.Transcoder) {
	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)
	bus := &mockBus{}
	log := zap.NewNop()
	mgr := NewManager(bus, transcoder, log)
	return mgr, bus, transcoder
}

func makeStoreEntry(id registry.ID, maxSize int) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: memstore.MemoryKV,
		Data: payload.New(map[string]any{
			"max_size":         maxSize,
			"cleanup_interval": "1m",
		}),
	}
}

func TestNewManager(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	require.NotNil(t, mgr)
	assert.NotNil(t, mgr.stores)
}

func TestManager_Add(t *testing.T) {
	mgr, bus, _ := newTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "cache")
	entry := makeStoreEntry(storeID, 1000)

	err := mgr.Add(ctx, entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, exists := mgr.stores[storeID]
	mgr.mu.RUnlock()
	assert.True(t, exists)

	events := bus.getEvents()
	require.Len(t, events, 2)
	assert.Equal(t, supervisor.System, events[0].System)
	assert.Equal(t, supervisor.ServiceRegister, events[0].Kind)
	assert.Equal(t, resource.System, events[1].System)
	assert.Equal(t, resource.Register, events[1].Kind)
}

func TestManager_AddAlreadyExists(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "cache")
	entry := makeStoreEntry(storeID, 1000)

	err := mgr.Add(ctx, entry)
	require.NoError(t, err)

	err = mgr.Add(ctx, entry)
	require.Error(t, err)

	var storeErr apierror.Error
	require.ErrorAs(t, err, &storeErr)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_AddUnsupportedKind(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	entry := registry.Entry{
		ID:   registry.NewID("test", "cache"),
		Kind: "unknown.kind",
		Data: payload.New(map[string]any{}),
	}

	err := mgr.Add(ctx, entry)
	require.Error(t, err)

	var storeErr apierror.Error
	require.ErrorAs(t, err, &storeErr)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestManager_Update(t *testing.T) {
	mgr, bus, _ := newTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "cache")
	entry := makeStoreEntry(storeID, 1000)

	err := mgr.Add(ctx, entry)
	require.NoError(t, err)

	bus.clearEvents()

	updatedEntry := makeStoreEntry(storeID, 2000)
	err = mgr.Update(ctx, updatedEntry)
	require.NoError(t, err)

	events := bus.getEvents()
	require.Len(t, events, 1)
	assert.Equal(t, supervisor.System, events[0].System)
	assert.Equal(t, supervisor.ServiceUpdate, events[0].Kind)
}

func TestManager_UpdateNotFound(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "nonexistent")
	entry := makeStoreEntry(storeID, 1000)

	err := mgr.Update(ctx, entry)
	require.Error(t, err)

	var storeErr apierror.Error
	require.ErrorAs(t, err, &storeErr)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_UpdateUnsupportedKind(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	entry := registry.Entry{
		ID:   registry.NewID("test", "cache"),
		Kind: "unknown.kind",
		Data: payload.New(map[string]any{}),
	}

	err := mgr.Update(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestManager_Delete(t *testing.T) {
	mgr, bus, _ := newTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "cache")
	entry := makeStoreEntry(storeID, 1000)

	err := mgr.Add(ctx, entry)
	require.NoError(t, err)

	bus.clearEvents()

	err = mgr.Delete(ctx, entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, exists := mgr.stores[storeID]
	mgr.mu.RUnlock()
	assert.False(t, exists)

	events := bus.getEvents()
	require.Len(t, events, 2)
	assert.Equal(t, supervisor.System, events[0].System)
	assert.Equal(t, supervisor.ServiceRemove, events[0].Kind)
	assert.Equal(t, resource.System, events[1].System)
	assert.Equal(t, resource.Delete, events[1].Kind)
}

func TestManager_DeleteNotFound(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "nonexistent")
	entry := makeStoreEntry(storeID, 1000)

	err := mgr.Delete(ctx, entry)
	require.Error(t, err)

	var storeErr apierror.Error
	require.ErrorAs(t, err, &storeErr)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_DeleteUnsupportedKind(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	entry := registry.Entry{
		ID:   registry.NewID("test", "cache"),
		Kind: "unknown.kind",
		Data: payload.New(map[string]any{}),
	}

	err := mgr.Delete(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}
