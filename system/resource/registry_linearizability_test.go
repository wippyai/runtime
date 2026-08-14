// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	apiresource "github.com/wippyai/runtime/api/resource"
)

func TestY07ResourceReplacePublishesNewGenerationWhileOldIsBorrowed(t *testing.T) {
	service, _ := setupTest()
	id := registry.NewID("test", "replace-borrowed")
	oldProvider := newMockResourceProvider()
	oldProvider.resources[id] = "old"
	newProvider := newMockResourceProvider()
	newProvider.resources[id] = "new"

	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: oldProvider}})
	borrowed, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)

	service.handleRemove(event.Event{Data: id})
	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: newProvider}})
	require.True(t, service.Exists(id))
	replacement, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)
	value, err := replacement.Get()
	require.NoError(t, err)
	require.Equal(t, "new", value)
	replacement.Release()
	borrowedValue, err := borrowed.Get()
	require.NoError(t, err)
	require.Equal(t, "old", borrowedValue)
	borrowed.Release()
}

func TestResourceUpdatePublishesNewGenerationWhileOldIsBorrowed(t *testing.T) {
	service, _ := setupTest()
	id := registry.NewID("test", "update-borrowed")
	oldProvider := newMockResourceProvider()
	oldProvider.resources[id] = "old"
	newProvider := newMockResourceProvider()
	newProvider.resources[id] = "new"
	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: oldProvider}})
	borrowed, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)

	service.handleUpdate(event.Event{Data: apiresource.Entry{ID: id, Provider: newProvider}})
	require.True(t, service.Exists(id))
	replacement, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)
	value, err := replacement.Get()
	require.NoError(t, err)
	require.Equal(t, "new", value)
	replacement.Release()
	borrowedValue, err := borrowed.Get()
	require.NoError(t, err)
	require.Equal(t, "old", borrowedValue)
	borrowed.Release()
}

func TestMultipleBorrowedGenerationsRetireIndependently(t *testing.T) {
	service, _ := setupTest()
	id := registry.NewID("test", "successive-updates")
	providers := []*mockResourceProvider{
		newMockResourceProvider(), newMockResourceProvider(), newMockResourceProvider(),
	}
	for i, provider := range providers {
		provider.resources[id] = i + 1
	}

	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: providers[0]}})
	first, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)
	service.handleUpdate(event.Event{Data: apiresource.Entry{ID: id, Provider: providers[1]}})
	second, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)
	service.handleUpdate(event.Event{Data: apiresource.Entry{ID: id, Provider: providers[2]}})
	third, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)

	for expected, borrowed := range []apiresource.Resource[any]{first, second, third} {
		value, getErr := borrowed.Get()
		require.NoError(t, getErr)
		require.Equal(t, expected+1, value)
	}
	first.Release()
	second.Release()
	require.True(t, service.Exists(id))
	third.Release()
	require.Len(t, service.resources, 1)
	require.Empty(t, service.resources[id].retired)
}

func TestLaterDeleteCancelsPendingReplacement(t *testing.T) {
	service, _ := setupTest()
	id := registry.NewID("test", "delete-pending")
	provider := newMockResourceProvider()
	provider.resources[id] = "old"
	replacement := newMockResourceProvider()
	replacement.resources[id] = "new"
	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: provider}})
	borrowed, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)

	service.handleRemove(event.Event{Data: id})
	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: replacement}})
	service.handleRemove(event.Event{Data: id})
	borrowed.Release()
	require.False(t, service.Exists(id))
}

func TestStopDrainsResourcesAndRejectsNewAcquires(t *testing.T) {
	service, _ := setupTest()
	id := registry.NewID("test", "stop-borrowed")
	provider := newMockResourceProvider()
	provider.resources[id] = "value"
	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: provider}})
	borrowed, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)
	require.NoError(t, service.Stop())
	require.False(t, service.Exists(id))
	_, err = service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.ErrorIs(t, err, apiresource.ErrNotFound)
	borrowed.Release()
	require.Empty(t, service.resources)
}
