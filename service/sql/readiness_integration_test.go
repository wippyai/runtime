// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/system/eventbus"
	systemresource "github.com/wippyai/runtime/system/resource"
)

func TestSQLAcknowledgementMakesDatabaseImmediatelyUsable(t *testing.T) {
	bus := eventbus.NewBus()
	manager, err := NewManagerWithFactory(&TestTranscoder{}, bus, nil, nil, NewTestPoolFactory())
	require.NoError(t, err)
	resources := systemresource.NewRegistry(bus, nil)
	require.NoError(t, resources.Start(context.Background()))
	defer resources.Stop()
	entry := registry.Entry{ID: registry.NewID("test", "ready"), Kind: sqlapi.SQLite, Data: payload.New(map[string]any{})}
	ctx := ctxapi.NewRootContext()
	for range 2 {
		require.NoError(t, manager.Add(ctx, entry))
		pool := manager.services[entry.ID]
		acquired, err := resources.Acquire(ctx, entry.ID, resource.ModeNormal)
		require.NoError(t, err)
		value, err := acquired.Get()
		require.NoError(t, err)
		var one int
		require.NoError(t, value.(DBResource).DB.QueryRowContext(ctx, "SELECT 1").Scan(&one))
		require.Equal(t, 1, one)
		acquired.Release()
		require.NoError(t, manager.Delete(ctx, entry))
		require.False(t, resources.Exists(entry.ID))
		require.NoError(t, pool.Stop(ctx))
	}
}
