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

func TestY07ResourceReplacePreservesBorrow(t *testing.T) {
	service, _ := setupTest()
	id := registry.NewID("test", "replace-borrowed")
	oldProvider := newMockResourceProvider()
	oldProvider.resources[id] = "old"
	newProvider := newMockResourceProvider()
	newProvider.resources[id] = "new"

	service.handleRegister(event.Event{Data: apiresource.Entry{ID: id, Provider: oldProvider}})
	borrowed, err := service.Acquire(context.Background(), id, apiresource.ModeNormal)
	require.NoError(t, err)

	service.handleUpdate(event.Event{Data: apiresource.Entry{ID: id, Provider: newProvider}})
	service.handleRemove(event.Event{Data: id})
	require.True(t, service.Exists(id), "replacement must retain the outstanding borrow generation")

	borrowed.Release()
	service.handleRemove(event.Event{Data: id})
	require.False(t, service.Exists(id))
}
