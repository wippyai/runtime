//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func slotCount(t *testing.T, db *sql.DB, slot string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM pg_replication_slots WHERE slot_name=$1`, slot).Scan(&n))
	return n
}

func startBriefSource(t *testing.T, repl, admin string) *Source {
	t.Helper()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: newCaptureBus(), StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	_, err := src.Start(context.Background())
	require.NoError(t, err)
	return src
}

func TestStopKeepsPersistentSlot(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	src := startBriefSource(t, repl, admin)
	require.Equal(t, 1, slotCount(t, db, itSlot))

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	cancel()
	assert.Equal(t, 1, slotCount(t, db, itSlot), "plain stop (restart/update) must keep the slot")
}

func TestMarkForSlotDropRemovesSlot(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	src := startBriefSource(t, repl, admin)
	require.Equal(t, 1, slotCount(t, db, itSlot))
	require.Eventually(t, func() bool {
		var n int
		_ = db.QueryRow(`SELECT count(*) FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&n)
		return n == 1
	}, 5*time.Second, 100*time.Millisecond, "checkpoint row must exist before delete")

	src.MarkForSlotDrop()
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	cancel()
	assert.Equal(t, 0, slotCount(t, db, itSlot), "delete must drop the slot to free WAL")

	var offsets int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&offsets))
	assert.Equal(t, 0, offsets, "delete must remove the checkpoint row too")
}
