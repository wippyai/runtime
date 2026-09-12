// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
)

func TestApplicationStoreCannotMutateOrScanNamingControlRecords(t *testing.T) {
	app := newTestStore(t, "application")
	ctx := context.Background()
	control := "_sys:registry:participants"
	original := []byte(`{"node":"incarnation"}`)
	_, err := app.engine.Set(control, original)
	require.NoError(t, err)
	before, err := app.engine.Get(control)
	require.NoError(t, err)
	key := registry.ParseID(control)
	_, err = app.Get(ctx, key)
	require.ErrorIs(t, err, store.ErrKeyNotFound)
	require.NoError(t, app.Set(ctx, store.Entry{Key: key, Value: jsonVal(`"application-value"`)}))
	got, err := app.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, []byte(`"application-value"`), got.Data())
	require.NoError(t, app.Delete(ctx, key))
	var visible []store.Entry
	require.NoError(t, app.Scan(ctx, store.ScanOptions{}, func(entry store.Entry) bool {
		visible = append(visible, entry)
		return true
	}))
	require.Empty(t, visible, "a full app scan must not expose runtime naming state")
	after, err := app.engine.Get(control)
	require.NoError(t, err)
	require.Equal(t, before, after, "application operations must not touch control record content or version")
}
