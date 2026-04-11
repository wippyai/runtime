// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func setupTest() (*Registry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	return NewRegistry(bus, logger), bus
}

type mockResourceProvider struct {
	resources       map[registry.ID]any
	lockedResources map[registry.ID]struct{}
	mu              sync.RWMutex
}

func newMockResourceProvider() *mockResourceProvider {
	return &mockResourceProvider{
		resources:       make(map[registry.ID]any),
		lockedResources: make(map[registry.ID]struct{}),
	}
}

type mockResource struct {
	data     any
	provider *mockResourceProvider
	id       registry.ID
	mu       sync.RWMutex
	mode     resource.AccessMode
	released bool
}

func (m *mockResource) Get() (any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.released {
		return nil, resource.ErrReleased
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
		return nil, resource.ErrNotFound
	}

	// Check for exclusive lock
	if _, locked := m.lockedResources[id]; locked {
		return nil, ErrLocked
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
	ctx := ctxapi.NewRootContext()
	service, _ := setupTest()

	err := service.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, service.subscriber)

	err = service.Stop()
	require.NoError(t, err)
}

func TestService_ResourceLifecycle(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	provider := newMockResourceProvider()
	id := registry.NewID("test", "resource1")

	// Test registration
	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test"},
	}

	// Initialize the provider with the resource
	provider.resources[id] = "test-data"

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})

	require.Eventually(t, func() bool {
		return service.Exists(id)
	}, 500*time.Millisecond, 5*time.Millisecond)
	assert.True(t, service.Exists(id))

	// Test concurrent access with normal mode
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := service.Acquire(ctx, id, resource.ModeNormal)
			if err == nil {
				defer res.Release()
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
	assert.Equal(t, ErrLocked, err)

	// Release exclusive lock
	res.Release()

	// Verify resource can be acquired again
	res, err = service.Acquire(ctx, id, resource.ModeNormal)
	require.NoError(t, err)
	res.Release()

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
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	provider := newMockResourceProvider()
	id := registry.NewID("test", "resource1")

	// Initialize the provider with the resource
	provider.resources[id] = "test-data"

	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test"},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})

	require.Eventually(t, func() bool {
		ids, listErr := service.List()
		if listErr != nil {
			return false
		}
		for _, existing := range ids {
			if existing == id {
				return true
			}
		}
		return false
	}, 500*time.Millisecond, 10*time.Millisecond)

	tests := []struct {
		errorType   error
		name        string
		mode        resource.AccessMode
		shouldError bool
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
					res.Release()

					// Verify can't Get after Release
					_, err = res.Get()
					assert.Equal(t, resource.ErrReleased, err)

					// Verify multiple Release calls are safe
					res.Release()
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
	id := registry.NewID("test", "resource1")

	// Initialize the provider with the resource
	provider.resources[id] = "test-data"

	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test"},
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestService_GetResources(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, _ := setupTest()

	// Test without registry in context
	assert.Nil(t, resource.GetRegistry(ctx))

	// Test with registry in context
	ctxWithReg := resource.WithRegistry(ctx, service)
	reg := resource.GetRegistry(ctxWithReg)
	assert.NotNil(t, reg)
	assert.Equal(t, service, reg)
}

func TestService_ResourceListing(t *testing.T) {
	ctx := ctxapi.NewRootContext()
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
			Meta:     map[string]any{"type": "test"},
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
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	provider := newMockResourceProvider()
	id := registry.NewID("test", "resource1")
	provider.resources[id] = "initial-data"

	// Register initial resource
	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"version": "1.0"},
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
			Meta:     map[string]any{"version": "2.0"},
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

	t.Run("update non-existent resource", func(_ *testing.T) {
		nonExistentID := registry.NewID("test", "nonexistent")
		updatedEntry := resource.Entry{
			ID:       nonExistentID,
			Provider: provider,
			Meta:     map[string]any{"version": "1.0"},
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
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	t.Run("unknown event kind", func(_ *testing.T) {
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

	t.Run("invalid update payload", func(_ *testing.T) {
		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Update,
			Path:   "test:resource",
			Data:   "invalid data",
		})
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("invalid remove payload", func(_ *testing.T) {
		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Delete,
			Path:   "test:resource",
			Data:   "invalid data",
		})
		time.Sleep(10 * time.Millisecond)
	})
}

func TestService_EventHandlingErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	// Test invalid event data for register
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   "test/resource1",
		Data:   "invalid-data", // Should be resource.Entry
	})

	// Test invalid event data for update
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   "test/resource1",
		Data:   "invalid-data", // Should be resource.Entry
	})

	// Test invalid event data for delete
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   "test/resource1",
		Data:   "invalid-data", // Should be registry.ID
	})

	// Test unknown event kind
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   "unknown",
		Path:   "test/resource1",
		Data:   nil,
	})

	// Give some time for event processing
	time.Sleep(100 * time.Millisecond)

	// Verify no resources were registered due to invalid events
	ids, err := service.List()
	require.NoError(t, err)
	assert.Empty(t, ids, "No resources should be registered after invalid events")
}

func TestService_ResourceUpdateScenarios(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	provider := newMockResourceProvider()
	id := registry.NewID("test", "resource1")

	// Initialize the provider with the resource
	provider.resources[id] = "initial-data"

	// Register initial resource
	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test", "version": 1},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})

	time.Sleep(10 * time.Millisecond)

	// Test updating non-existent resource
	nonExistentID := registry.NewID("test", "nonexistent")
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   nonExistentID.String(),
		Data: resource.Entry{
			ID:       nonExistentID,
			Provider: provider,
			Meta:     map[string]any{"type": "test"},
		},
	})

	// Update existing resource
	updatedEntry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test", "version": 2},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   id.String(),
		Data:   updatedEntry,
	})

	time.Sleep(10 * time.Millisecond)

	// Verify resource still exists after update
	assert.True(t, service.Exists(id))
}

func TestService_ResourceAcquisitionEdgeCases(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	provider := newMockResourceProvider()
	id := registry.NewID("test", "resource1")

	// Test acquiring non-existent resource
	_, err := service.Acquire(ctx, id, resource.ModeNormal)
	assert.Equal(t, resource.ErrNotFound, err)

	// Register resource
	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test"},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})

	time.Sleep(10 * time.Millisecond)

	// Test acquiring with canceled context
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = service.Acquire(cancelledCtx, id, resource.ModeNormal)
	assert.Equal(t, context.Canceled, err)

	// Test acquiring with deadline exceeded
	deadlineCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
	defer cancel()
	<-deadlineCtx.Done() // Wait for context to actually expire (Windows has ~15ms timer resolution)
	_, err = service.Acquire(deadlineCtx, id, resource.ModeNormal)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestService_ResourceListingEdgeCases(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() {
		require.NoError(t, service.Stop())
	}()

	// Test empty listing
	ids, err := service.List()
	require.NoError(t, err)
	assert.Empty(t, ids)

	// Register multiple resources
	provider := newMockResourceProvider()
	resources := []registry.ID{
		{NS: "test", Name: "resource1"},
		{NS: "test", Name: "resource2"},
		{NS: "other", Name: "resource1"},
	}

	for _, id := range resources {
		entry := resource.Entry{
			ID:       id,
			Provider: provider,
			Meta:     map[string]any{"type": "test"},
		}

		bus.Send(ctx, event.Event{
			System: resource.System,
			Kind:   resource.Register,
			Path:   id.String(),
			Data:   entry,
		})
	}

	time.Sleep(10 * time.Millisecond)

	// Test listing all resources
	ids, err = service.List()
	require.NoError(t, err)
	assert.Len(t, ids, len(resources))

	// Verify all registered resources are in the list
	for _, id := range resources {
		assert.Contains(t, ids, id)
	}

	// Remove one resource and verify listing
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   resources[0].String(),
		Data:   resources[0],
	})

	time.Sleep(10 * time.Millisecond)

	ids, err = service.List()
	require.NoError(t, err)
	assert.Len(t, ids, len(resources)-1)
	assert.NotContains(t, ids, resources[0])
}

func TestService_AcquireProviderError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	provider := newMockResourceProvider()
	id := registry.NewID("test", "missing")

	// Register resource but don't add to provider's internal map
	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test"},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})
	time.Sleep(10 * time.Millisecond)

	// Acquire should fail because provider doesn't have the resource
	_, err := service.Acquire(ctx, id, resource.ModeNormal)
	assert.Equal(t, resource.ErrNotFound, err)
}

func TestService_DeleteNonExistentResource(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	id := registry.NewID("test", "nonexistent")

	// Try to delete non-existent resource - should log warning but not panic
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   id.String(),
		Data:   id,
	})
	time.Sleep(10 * time.Millisecond)

	assert.False(t, service.Exists(id))
}

func TestService_DeleteBorrowedResource(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	service, bus := setupTest()
	require.NoError(t, service.Start(ctx))
	defer func() { assert.NoError(t, service.Stop()) }()

	provider := newMockResourceProvider()
	id := registry.NewID("test", "borrowed")
	provider.resources[id] = "borrowed-data"

	entry := resource.Entry{
		ID:       id,
		Provider: provider,
		Meta:     map[string]any{"type": "test"},
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data:   entry,
	})
	time.Sleep(10 * time.Millisecond)

	// Acquire resource (borrow it)
	res, err := service.Acquire(ctx, id, resource.ModeNormal)
	require.NoError(t, err)

	// Try to delete while borrowed - should fail silently
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   id.String(),
		Data:   id,
	})
	time.Sleep(10 * time.Millisecond)

	// Resource should still exist
	assert.True(t, service.Exists(id))

	// Release the resource
	res.Release()

	// Now delete should succeed
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   id.String(),
		Data:   id,
	})
	time.Sleep(10 * time.Millisecond)

	assert.False(t, service.Exists(id))
}
