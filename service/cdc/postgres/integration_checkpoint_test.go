//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBCheckpointerRoundtrip(t *testing.T) {
	_, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	cp, err := NewDBCheckpointer(ctx, db)
	require.NoError(t, err)

	require.NoError(t, cp.Save(ctx, "it_cp_slot", pglogrepl.LSN(0x9999)))
	lsn, ok, err := cp.Load(ctx, "it_cp_slot")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, pglogrepl.LSN(0x9999), lsn)

	require.NoError(t, cp.Save(ctx, "it_cp_slot", pglogrepl.LSN(0xAAAA)))
	lsn, _, err = cp.Load(ctx, "it_cp_slot")
	require.NoError(t, err)
	assert.Equal(t, pglogrepl.LSN(0xAAAA), lsn)

	_, ok, err = cp.Load(ctx, "no_such_slot_xyz")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, cp.Delete(ctx, "it_cp_slot"))
	_, ok, err = cp.Load(ctx, "it_cp_slot")
	require.NoError(t, err)
	assert.False(t, ok, "deleted checkpoint must be gone")
}
