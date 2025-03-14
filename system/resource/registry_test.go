package resource

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTest() (*Registry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	return NewResourceRegistry(bus, logger), bus
}

type mockResourceProvider struct {
	resources       map[registry.ID]interface{}
	lockedResources map[registry.ID]struct{}
	mu              sync.RWMutex
}

func newMockResourceProvider() *mockResourceProvider {
	return &mockResourceProvider{
		resources:       make(map[registry.ID]interface{}),
		lockedResources: make(map[registry.ID]struct{}),
	}
}

type mockResource struct {
	provider *mockResourceProvider
	id       registry.ID
	data     interface{}
	mode     resource.AccessMode
	released bool
	mu       sync.RWMutex
}

func (m *mockResource) Get() (any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.released {
		return nil, resource.ErrResourceReleased
	}
	return m.data, nil
}

func (m *mockResource) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.released {
		return // Already released
	}

	if m.mode == resource.ModeExclusive {
		m.provider.mu.Lock()
		delete(m.provider.lockedResources, m.id)
		m.provider.mu.Unlock()
	}

	m.released = true
	return
}

func (m *mockResourceProvider) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	// First check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if resource exists
	data, exists := m.resources[id]
	if !exists {
		return nil, resource.ErrResourceNotFound
	}

	// Check for exclusive lock
	if _, locked := m.lockedResources[id]; locked {
		return nil, resource.ErrResourceLocked
	}

	// If requesting exclusive access, lock the resource
	if mode == resource.ModeExclusive {
		m.lockedResources[id] = struct{}{}
	}

	return &mockResource{
		provider: m,
		id:       id,
		data:     data,
		mode:     mode,
	}, nil
}

func TestService_StartStop(t *testing.T) {
	ctx := context.Background()
	service, _ := setupTest()

	err := service.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, service.subscriber)

	err = service.Stop()
	require.NoError(t, err)
}

func TestService_ResourceLifecycle(t *testing.T) {
	ctx := context.Background()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	provider := newMockResourceProvider()
	id := registry.ID{NS: "test", Name: "resource1"}

	// Test registration
	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]interface{}{"type": "test"},
	}

	// Initialize the provider with the resource
	provider.resources[id] = "test-data"

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})

	time.Sleep(10 * time.Millisecond)
	assert.True(t, service.Exists(id))

	// Test concurrent access with normal mode
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := service.Acquire(ctx, id, resource.ModeNormal)
			if err == nil {
				defer func() { assert.NoError(t, res.Release()) }()
				_, err := res.Get()
				assert.NoError(t, err)
			}
		}()
	}
	wg.Wait()

	// Test exclusive access
	res, err := service.Acquire(ctx, id, resource.ModeExclusive)
	require.NoError(t, err)

	// Verify other acquires fail while exclusive lock is held
	_, err = service.Acquire(ctx, id, resource.ModeNormal)
	assert.Equal(t, resource.ErrResourceLocked, err)

	// Release exclusive lock
	require.NoError(t, res.Release())

	// Verify resource can be acquired again
	res, err = service.Acquire(ctx, id, resource.ModeNormal)
	require.NoError(t, err)
	require.NoError(t, res.Release())

	// Test removal
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   id.String(),
		Data:   id,
	})

	time.Sleep(10 * time.Millisecond)
	assert.False(t, service.Exists(id))
}

func TestService_ResourceAccess(t *testing.T) {
	ctx := context.Background()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	provider := newMockResourceProvider()
	id := registry.ID{NS: "test", Name: "resource1"}

	// Initialize the provider with the resource
	provider.resources[id] = "test-data"

	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]interface{}{"type": "test"},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})

	time.Sleep(10 * time.Millisecond)

	tests := []struct {
		name        string
		mode        resource.AccessMode
		shouldError bool
		errorType   error
	}{
		{
			name:        "normal mode",
			mode:        resource.ModeNormal,
			shouldError: false,
		},
		{
			name:        "exclusive mode",
			mode:        resource.ModeExclusive,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := service.Acquire(ctx, id, tt.mode)
			if tt.shouldError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.Equal(t, tt.errorType, err)
				}
				assert.Nil(t, res)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, res)

				if res != nil {
					// Test Get and Release
					val, err := res.Get()
					assert.NoError(t, err)
					assert.NotNil(t, val)
					assert.NoError(t, res.Release())

					// Verify can't Get after Release
					_, err = res.Get()
					assert.Equal(t, resource.ErrResourceReleased, err)

					// Verify multiple Release calls are safe
					assert.NoError(t, res.Release())
				}
			}
		})
	}
}

func TestService_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	provider := newMockResourceProvider()
	id := registry.ID{NS: "test", Name: "resource1"}

	// Initialize the provider with the resource
	provider.resources[id] = "test-data"

	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]interface{}{"type": "test"},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})

	time.Sleep(10 * time.Millisecond)

	// Cancel context before acquire
	cancel()

	// Attempt to acquire resource with canceled context
	_, err := service.Acquire(ctx, id, resource.ModeExclusive)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestService_GetResources(t *testing.T) {
	ctx := context.Background()
	service, _ := setupTest()

	// Test without registry in context
	assert.Nil(t, resource.GetResources(ctx))

	// Test with registry in context
	ctxWithReg := resource.WithResources(ctx, service)
	reg := resource.GetResources(ctxWithReg)
	assert.NotNil(t, reg)
	assert.Equal(t, service, reg)
}

func TestService_ResourceListing(t *testing.T) {
	ctx := context.Background()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	provider := newMockResourceProvider()
	resources := []registry.ID{
		{NS: "test", Name: "resource1"},
		{NS: "test", Name: "resource2"},
		{NS: "test", Name: "resource3"},
	}

	// Register multiple resources
	for _, id := range resources {
		provider.resources[id] = fmt.Sprintf("data-%s", id.Name)
		entry := resource.Entry{
			ID:       id,
			Provider: provider,
			Meta:     map[string]interface{}{"type": "test"},
		}

		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Register,
			Path:   id.String(),
			Data:   entry,
		})
	}
	time.Sleep(10 * time.Millisecond)

	// Test List method
	listed, err := service.List()
	require.NoError(t, err)
	assert.Equal(t, len(resources), len(listed))

	// Verify all registered resources are in the list
	for _, id := range resources {
		found := false
		for _, listedID := range listed {
			if listedID == id {
				found = true
				break
			}
		}
		assert.True(t, found, "Resource %s should be in the list", id.String())
	}

	// Delete a resource and verify list is updated
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   resources[0].String(),
		Data:   resources[0],
	})
	time.Sleep(10 * time.Millisecond)

	listed, err = service.List()
	require.NoError(t, err)
	assert.Equal(t, len(resources)-1, len(listed))
}

func TestService_UpdateResource(t *testing.T) {
	ctx := context.Background()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	provider := newMockResourceProvider()
	id := registry.ID{NS: "test", Name: "resource1"}
	provider.resources[id] = "initial-data"

	// Register initial resource
	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]interface{}{"version": "1.0"},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})
	time.Sleep(10 * time.Millisecond)

	t.Run("update existing resource", func(t *testing.T) {
		updatedEntry := resource.Entry{
			ID:       id,
			Provider: provider,
			Meta:     map[string]interface{}{"version": "2.0"},
		}

		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Update,
			Path:   id.String(),
			Data:   updatedEntry,
		})
		time.Sleep(10 * time.Millisecond)

		// Verify resource was updated
		assert.True(t, service.Exists(id))
	})

	t.Run("update non-existent resource", func(t *testing.T) {
		nonExistentID := registry.ID{NS: "test", Name: "nonexistent"}
		updatedEntry := resource.Entry{
			ID:       nonExistentID,
			Provider: provider,
			Meta:     map[string]interface{}{"version": "1.0"},
		}

		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Update,
			Path:   nonExistentID.String(),
			Data:   updatedEntry,
		})
		time.Sleep(10 * time.Millisecond)
	})
}

func TestService_HandleEvent(t *testing.T) {
	ctx := context.Background()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	t.Run("unknown event kind", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   "unknown.event",
			Path:   "test:resource",
		})
		time.Sleep(10 * time.Millisecond) // Give time for event processing
	})

	t.Run("invalid register payload", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Register,
			Path:   "test:resource",
			Data:   "invalid data",
		})
		time.Sleep(10 * time.Millisecond)
		assert.False(t, service.Exists(registry.ParseID("test:resource")))
	})

	t.Run("invalid update payload", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Update,
			Path:   "test:resource",
			Data:   "invalid data",
		})
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("invalid remove payload", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Delete,
			Path:   "test:resource",
			Data:   "invalid data",
		})
		time.Sleep(10 * time.Millisecond)
	})
}
