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

func TestY07ResourceReplaceWaitsForBorrowedGeneration(t *testing.T) {
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
	require.False(t, service.Exists(id), "replacement must wait for the outstanding borrow generation")
	_, err = service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.ErrorIs(t, err, apiresource.ErrNotFound)

	borrowed.Release()
	require.True(t, service.Exists(id))
	replacement, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)
	value, err := replacement.Get()
	require.NoError(t, err)
	require.Equal(t, "new", value)
	replacement.Release()
}

func TestResourceUpdateWaitsForBorrowedGeneration(t *testing.T) {
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
	require.False(t, service.Exists(id))
	_, err = service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.ErrorIs(t, err, apiresource.ErrNotFound)
	borrowed.Release()

	replacement, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)
	value, err := replacement.Get()
	require.NoError(t, err)
	require.Equal(t, "new", value)
	replacement.Release()
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
