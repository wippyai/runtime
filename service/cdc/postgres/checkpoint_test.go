// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryCheckpointerMissing(t *testing.T) {
	cp := NewMemoryCheckpointer()
	lsn, ok, err := cp.Load(context.Background(), "slot")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, pglogrepl.LSN(0), lsn)
}

func TestMemoryCheckpointerRoundtrip(t *testing.T) {
	cp := NewMemoryCheckpointer()
	ctx := context.Background()

	require.NoError(t, cp.Save(ctx, "slot", pglogrepl.LSN(0x1234)))
	lsn, ok, err := cp.Load(ctx, "slot")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, pglogrepl.LSN(0x1234), lsn)

	require.NoError(t, cp.Save(ctx, "slot", pglogrepl.LSN(0x5678)))
	lsn, _, err = cp.Load(ctx, "slot")
	require.NoError(t, err)
	assert.Equal(t, pglogrepl.LSN(0x5678), lsn)

	_, ok, err = cp.Load(ctx, "other")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestMemoryCheckpointerDelete(t *testing.T) {
	cp := NewMemoryCheckpointer()
	ctx := context.Background()

	require.NoError(t, cp.Save(ctx, "slot", pglogrepl.LSN(0x1)))
	require.NoError(t, cp.Delete(ctx, "slot"))
	_, ok, err := cp.Load(ctx, "slot")
	require.NoError(t, err)
	assert.False(t, ok, "deleted checkpoint must not be loadable")

	require.NoError(t, cp.Delete(ctx, "never_existed"))
}
